-- The schema as the pre-goose auto-migration left it (Phase 0.5), quirks
-- included; 00003 fixes them. IF NOT EXISTS lets goose adopt a database the
-- auto-migration already created: this is a no-op there, and goose just
-- records version 1.

-- +goose Up
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	password TEXT NOT NULL,
	email TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
	token TEXT PRIMARY KEY,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	revoked_at TIMESTAMP,
	user_id TEXT NOT NULL,
	expires_at TIMESTAMP NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS videos (
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

-- +goose Down
DROP TABLE videos;
DROP TABLE refresh_tokens;
DROP TABLE users;
