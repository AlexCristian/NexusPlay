// Package store is NexusPlay's data layer: a SQLite database with embedded
// migrations and typed query methods. It is the only package that writes SQL.
//
// Rules that must hold across several rows (publishing and notifying
// subscribers, applying a subtitle correction once enough people vote,
// replacing a converted file) are single transactions here, not sequences
// of calls by the caller.
//
// Errors: a lookup of one row returns ErrNotFound when there is none; a
// write that breaks a uniqueness rule returns ErrDuplicate. Callers test
// them with errors.Is. A Store is safe for concurrent use.
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

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// Store wraps the database handle.
type Store struct {
	database *sql.DB
	now      func() time.Time
}

// Open opens (creating if needed) the SQLite database at path and applies
// any pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	store, err := sql.Open("sqlite", dataSourceName(path))
	if err != nil {
		return nil, err
	}
	// A bounded pool. WAL lets readers run alongside the single writer, so
	// one connection would make every page load wait behind every write (and
	// a query issued while a result set is still open would deadlock). Eight
	// is plenty for a household server.
	store.SetMaxOpenConns(8)
	database := &Store{database: store, now: time.Now}
	if err := database.migrate(ctx); err != nil {
		store.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return database, nil
}

// Close closes the database.
func (store *Store) Close() error { return store.database.Close() }

// Ping checks that the database answers.
func (store *Store) Ping(ctx context.Context) error { return store.database.PingContext(ctx) }

// dataSourceName builds the driver's connection string. The path is
// URL-encoded, so a data folder with "?", "#" or "%" in its name opens the
// right file instead of silently creating a database elsewhere and
// dropping every setting after the stray "?".
//
// Every pooled connection gets these settings. _txlock=immediate makes
// transactions take the write lock when they begin. Without it, a
// transaction that reads and then writes (creating the first user, voting)
// can fail with SQLITE_BUSY when another connection wrote in between; with
// it, a second writer waits up to busy_timeout instead. Every transaction in
// this package writes, so nothing is lost.
func dataSourceName(path string) string {
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

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

func (store *Store) unix() int64 { return store.now().Unix() }

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
