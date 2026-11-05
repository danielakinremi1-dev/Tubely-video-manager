package database

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

type Client struct {
	db *sql.DB
}

func NewClient(pathToDB string) (Client, error) {
	db, err := sql.Open("sqlite3", pathToDB)
	if err != nil {
		return Client{}, err
	}
	c := Client{db}
	err = c.autoMigrate()
	if err != nil {
		return Client{}, err
	}
	return c, nil

}

func (c *Client) autoMigrate() error {
	userTable := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		password TEXT NOT NULL,
		email TEXT UNIQUE NOT NULL
	);
	`
	_, err := c.db.Exec(userTable)
	if err != nil {
		return err
	}
	refreshTokenTable := `
	CREATE TABLE IF NOT EXISTS refresh_tokens (
		token TEXT PRIMARY KEY,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		revoked_at TIMESTAMP,
		user_id TEXT NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	`
	_, err = c.db.Exec(refreshTokenTable)
	if err != nil {
		return err
	}

	videoTable := `
	CREATE TABLE IF NOT EXISTS videos (
		id TEXT PRIMARY KEY,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		title TEXT NOT NULL,
		description TEXT,
		thumbnail_url TEXT,
		video_url TEXT TEXT,
		user_id INTEGER,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	`
	_, err = c.db.Exec(videoTable)
	if err != nil {
		return err
	}

	// Columns added after the original schema. CREATE TABLE IF NOT EXISTS
	// never alters an existing table, so add them explicitly.
	if err := c.addColumnIfMissing("videos", "video_key", "TEXT"); err != nil {
		return err
	}
	if err := c.addColumnIfMissing("videos", "thumbnail_key", "TEXT"); err != nil {
		return err
	}
	return c.backfillVideoKeys()
}

// addColumnIfMissing adds a column unless the table already has it. Table,
// column and declaration are code constants, never user input.
func (c *Client) addColumnIfMissing(table, column, decl string) error {
	rows, err := c.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid, notNull, pk int
			name, colType    string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	_, err = c.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl))
	return err
}

// backfillVideoKeys derives object keys for rows created before video_key
// existed, when only a full CDN URL (https://<domain>/<key>) was stored.
func (c *Client) backfillVideoKeys() error {
	rows, err := c.db.Query(`
		SELECT id, video_url FROM videos
		WHERE video_key IS NULL AND video_url IS NOT NULL AND video_url != ''
	`)
	if err != nil {
		return err
	}
	type legacyRow struct{ id, url string }
	var legacy []legacyRow
	for rows.Next() {
		var r legacyRow
		if err := rows.Scan(&r.id, &r.url); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Update after the read cursor is closed; SQLite locks otherwise.
	for _, r := range legacy {
		key := keyFromURL(r.url)
		if key == "" {
			continue
		}
		if _, err := c.db.Exec("UPDATE videos SET video_key = ? WHERE id = ?", key, r.id); err != nil {
			return err
		}
	}
	return nil
}

// keyFromURL returns the object key from a CDN URL such as
// https://dxxxx.cloudfront.net/landscape/abc.mp4, or "" if it isn't one.
func keyFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

func (c Client) Reset() error {
	if _, err := c.db.Exec("DELETE FROM refresh_tokens"); err != nil {
		return fmt.Errorf("failed to reset table refresh_tokens: %w", err)
	}
	if _, err := c.db.Exec("DELETE FROM users"); err != nil {
		return fmt.Errorf("failed to reset table users: %w", err)
	}
	if _, err := c.db.Exec("DELETE FROM videos"); err != nil {
		return fmt.Errorf("failed to reset table videos: %w", err)
	}
	return nil
}
