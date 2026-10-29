package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// [migrations] holds the schema changes in migrations/, applied in the order
// of their number prefix ("0001_init.sql" first).
//
//go:embed migrations/*.sql
var migrations embed.FS

// [ErrNotFound] is returned when a lookup of one row matches none.
var ErrNotFound = errors.New("not found")

// [Store] is the NexusPlay database. Get one from [Open]. It is safe for
// concurrent use.
type Store struct {
	// [database] is the pool of SQLite connections, at most eight.
	database *sql.DB
	// [now] tells the time for the timestamps the store writes. Tests replace
	// it.
	now func() time.Time
}

// [Open] opens the SQLite database at a path, creating it if needed, and
// applies any migrations it does not have yet, each in its own transaction.
//
// Parameters:
//   - [ctx]: cancels the migrations.
//   - [path]: the database file, such as the one [config.Config.DatabasePath]
//     names. Any characters are allowed (see [dataSourceName]).
//
// Returns:
//   - [*Store]: the open database, or [nil] if [error] is not [nil]. Call
//     [Store.Close] when done.
//   - [error]: [nil] on success, the driver's error if the file cannot be
//     opened, or an error starting with "migrate" if a migration fails.
func Open(ctx context.Context, path string) (*Store, error) {
	pool, err := sql.Open("sqlite", dataSourceName(path))
	if err != nil {
		return nil, err
	}
	// A bounded pool. WAL lets readers run alongside the single writer, so
	// one connection would make every page load wait behind every write (and
	// a query issued while a result set is still open would deadlock). Eight
	// is plenty for a household server.
	pool.SetMaxOpenConns(8)
	store := &Store{database: pool, now: time.Now}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

// [Close] closes every connection. The [Store] must not be used afterwards.
//
// Returns:
//   - [error]: [nil] on success, or the driver's error.
func (store *Store) Close() error { return store.database.Close() }

// [Ping] checks that the database answers, for the health check.
//
// Parameters:
//   - [ctx]: cancels the check.
//
// Returns:
//   - [error]: [nil] if the database answered, or why it did not.
func (store *Store) Ping(ctx context.Context) error { return store.database.PingContext(ctx) }

// [dataSourceName] builds the driver's connection string. The path is
// URL-encoded, so a data folder with "?", "#" or "%" in its name opens the
// right file instead of silently creating a database elsewhere and dropping
// every setting after the stray "?".
//
// Every pooled connection gets these settings: foreign keys on, WAL
// journaling, a 5-second busy timeout and normal syncing. "_txlock=immediate"
// makes transactions take the write lock when they begin. Without it, a
// transaction that reads and then writes (creating the first user, voting)
// can fail with SQLITE_BUSY when another connection wrote in between; with
// it, a second writer waits up to the busy timeout instead. Every
// transaction in this package writes, so nothing is lost.
//
// Parameters:
//   - [path]: the database file.
//
// Returns:
//   - [string]: a "file:" URL with the path and settings.
func dataSourceName(path string) string {
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

// [migrate] applies the embedded migrations newer than the database's
// version, recorded in the schema_migrations table, in order, each in its
// own transaction.
//
// Parameters:
//   - [ctx]: cancels the migrations.
//
// Returns:
//   - [error]: [nil] on success; an error naming the file if a migration
//     has no number prefix or fails; otherwise the database error.
func (store *Store) migrate(ctx context.Context) error {
	if _, err := store.database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var current int
	if err := store.database.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad version prefix", base)
		}
		if version <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = store.withTransaction(ctx, func(transaction *sql.Tx) error {
			if _, err := transaction.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", base, err)
			}
			_, err := transaction.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// [withTransaction] runs an operation in one transaction, committing it if
// the operation succeeds and rolling it back if it fails.
//
// Parameters:
//   - [ctx]: cancels the transaction.
//   - [operation]: the work to do with the transaction.
//
// Returns:
//   - [error]: [nil] if the operation succeeded and the commit worked;
//     otherwise the operation's error, or the error from starting or
//     committing the transaction.
func (store *Store) withTransaction(ctx context.Context, operation func(*sql.Tx) error) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := operation(transaction); err != nil {
		transaction.Rollback()
		return err
	}
	return transaction.Commit()
}

// [unix] gives the current time as stored in the database.
//
// Returns:
//   - [int64]: seconds since 1 January 1970 UTC.
func (store *Store) unix() int64 { return store.now().Unix() }

// [notFound] translates the database package's "no rows" error into this
// package's.
//
// Parameters:
//   - [err]: an error from a single-row query, or [nil].
//
// Returns:
//   - [error]: [ErrNotFound] for [sql.ErrNoRows], and [err] unchanged
//     otherwise.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
