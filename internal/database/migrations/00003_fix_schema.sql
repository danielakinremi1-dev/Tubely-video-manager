-- Fix the baseline's quirks:
--   * videos.user_id was INTEGER but holds UUID text; now TEXT NOT NULL.
--   * videos.video_url was declared "TEXT TEXT".
--   * videos.description could be NULL, which Go can't scan into a string.
--   * Deleting a user now cascades to their videos and refresh tokens.
--   * Index the foreign keys (cascades and per-user listings use them).
--
-- SQLite can't change a column's type or constraints with ALTER TABLE, so
-- each table is rebuilt: create, copy, drop, rename. Rows whose user no
-- longer exists can't satisfy the foreign key and are dropped; nothing could
-- reach them anyway. (Their media objects, if any, stay in the bucket.)

-- +goose Up
CREATE TABLE videos_new (
	id TEXT PRIMARY KEY,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	thumbnail_url TEXT, -- legacy full URL, read-only; see thumbnail_key
	video_url TEXT,     -- legacy full URL, read-only; see video_key
	video_key TEXT,
	thumbnail_key TEXT,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
INSERT INTO videos_new (id, created_at, updated_at, title, description,
	thumbnail_url, video_url, video_key, thumbnail_key, user_id)
SELECT id, COALESCE(created_at, CURRENT_TIMESTAMP), COALESCE(updated_at, CURRENT_TIMESTAMP),
	title, COALESCE(description, ''), thumbnail_url, video_url, video_key,
	thumbnail_key, CAST(user_id AS TEXT)
FROM videos
WHERE user_id IN (SELECT id FROM users);
DROP TABLE videos;
ALTER TABLE videos_new RENAME TO videos;
CREATE INDEX videos_user_id_created_at ON videos (user_id, created_at);

CREATE TABLE refresh_tokens_new (
	token TEXT PRIMARY KEY,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	revoked_at TIMESTAMP,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TIMESTAMP NOT NULL
);
INSERT INTO refresh_tokens_new (token, created_at, updated_at, revoked_at, user_id, expires_at)
SELECT token, created_at, updated_at, revoked_at, user_id, expires_at
FROM refresh_tokens
WHERE user_id IN (SELECT id FROM users);
DROP TABLE refresh_tokens;
ALTER TABLE refresh_tokens_new RENAME TO refresh_tokens;
CREATE INDEX refresh_tokens_user_id ON refresh_tokens (user_id);

-- +goose Down
CREATE TABLE videos_old (
	id TEXT PRIMARY KEY,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	title TEXT NOT NULL,
	description TEXT,
	thumbnail_url TEXT,
	video_url TEXT TEXT,
	user_id INTEGER,
	video_key TEXT,
	thumbnail_key TEXT,
	FOREIGN KEY(user_id) REFERENCES users(id)
);
INSERT INTO videos_old (id, created_at, updated_at, title, description,
	thumbnail_url, video_url, user_id, video_key, thumbnail_key)
SELECT id, created_at, updated_at, title, description,
	thumbnail_url, video_url, user_id, video_key, thumbnail_key
FROM videos;
DROP TABLE videos;
ALTER TABLE videos_old RENAME TO videos;

CREATE TABLE refresh_tokens_old (
	token TEXT PRIMARY KEY,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	revoked_at TIMESTAMP,
	user_id TEXT NOT NULL,
	expires_at TIMESTAMP NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id)
);
INSERT INTO refresh_tokens_old SELECT token, created_at, updated_at, revoked_at, user_id, expires_at
FROM refresh_tokens;
DROP TABLE refresh_tokens;
ALTER TABLE refresh_tokens_old RENAME TO refresh_tokens;
