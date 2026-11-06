package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

type Client struct {
	db *sql.DB
}

func NewClient(pathToDB string) (Client, error) {
	// SQLite ignores REFERENCES clauses unless foreign_keys is on, and the
	// setting is per connection. Putting it in the DSN applies it to every
	// connection database/sql opens, which a one-off PRAGMA wouldn't.
	db, err := sql.Open("sqlite3", pathToDB+"?_foreign_keys=on")
	if err != nil {
		return Client{}, err
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return Client{}, err
	}
	return Client{db}, nil
}

// Ping checks that the database is reachable.
func (c Client) Ping(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// Close releases the underlying database handle.
func (c Client) Close() error {
	return c.db.Close()
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
