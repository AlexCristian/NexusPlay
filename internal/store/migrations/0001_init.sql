CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    nickname      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT    NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE collections (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    category   TEXT    NOT NULL DEFAULT 'TV Shows',
    year       TEXT    NOT NULL DEFAULT '',
    overview   TEXT    NOT NULL DEFAULT '',
    poster     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE media (
    id            INTEGER PRIMARY KEY,
    path          TEXT    NOT NULL UNIQUE,
    title         TEXT    NOT NULL,
    year          TEXT    NOT NULL DEFAULT '',
    overview      TEXT    NOT NULL DEFAULT '',
    category      TEXT    NOT NULL DEFAULT '',
    collection_id INTEGER REFERENCES collections(id) ON DELETE SET NULL,
    season        INTEGER NOT NULL DEFAULT 0,
    episode       INTEGER NOT NULL DEFAULT 0,
    poster        TEXT    NOT NULL DEFAULT '',
    duration      REAL    NOT NULL DEFAULT 0,
    container     TEXT    NOT NULL DEFAULT '',
    video_codec   TEXT    NOT NULL DEFAULT '',
    audio_codec   TEXT    NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    published     INTEGER NOT NULL DEFAULT 0,
    views         INTEGER NOT NULL DEFAULT 0,
    added_at      INTEGER NOT NULL,
    published_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX media_collection ON media(collection_id, season, episode);
CREATE INDEX media_published ON media(published, published_at);

CREATE TABLE watch_progress (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id   INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    position   REAL    NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE watched (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id   INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    watched_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE subscriptions (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, collection_id)
);

CREATE TABLE notifications (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id   INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE subtitles (
    id         INTEGER PRIMARY KEY,
    media_id   INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    language   TEXT    NOT NULL,
    label      TEXT    NOT NULL,
    source     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX subtitles_media ON subtitles(media_id);
CREATE UNIQUE INDEX subtitles_source ON subtitles(media_id, source) WHERE source <> '';

CREATE TABLE subtitle_cues (
    id          INTEGER PRIMARY KEY,
    subtitle_id INTEGER NOT NULL REFERENCES subtitles(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    start_ms    INTEGER NOT NULL,
    end_ms      INTEGER NOT NULL,
    text        TEXT    NOT NULL
);
CREATE INDEX subtitle_cues_sub ON subtitle_cues(subtitle_id, idx);

CREATE TABLE subtitle_changes (
    id            INTEGER PRIMARY KEY,
    cue_id        INTEGER NOT NULL REFERENCES subtitle_cues(id) ON DELETE CASCADE,
    proposed_text TEXT    NOT NULL,
    created_by    INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at    INTEGER NOT NULL,
    UNIQUE (cue_id, proposed_text)
);

CREATE TABLE subtitle_votes (
    change_id INTEGER NOT NULL REFERENCES subtitle_changes(id) ON DELETE CASCADE,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (change_id, user_id)
);
