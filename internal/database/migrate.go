package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/pressly/goose/v3"
)

// Versioned schema migrations, applied in order by goose and recorded in the
// goose_db_version table. SQL migrations live in migrations/NNNNN_name.sql;
// Go migrations (for logic SQL can't express) are registered below. Never
// edit a migration that has shipped. Add a new one.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

func newMigrationProvider(db *sql.DB) (*goose.Provider, error) {
	fsys, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys,
		goose.WithGoMigrations(
			goose.NewGoMigration(2, &goose.GoFunc{RunTx: adoptPreKeyDatabase}, nil),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("init migrations: %w", err)
	}
	return provider, nil
}

// migrate brings the database up to the latest schema version.
func migrate(ctx context.Context, db *sql.DB) error {
	provider, err := newMigrationProvider(db)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// adoptPreKeyDatabase (version 2) upgrades databases created before object
// keys existed: the baseline's CREATE TABLE IF NOT EXISTS leaves their videos
// table without the key columns. It adds them and derives keys from the
// stored CDN URLs. On any newer database it's a no-op.
func adoptPreKeyDatabase(ctx context.Context, tx *sql.Tx) error {
	for _, col := range []string{"video_key", "thumbnail_key"} {
		if err := addColumnIfMissing(ctx, tx, "videos", col, "TEXT"); err != nil {
			return err
		}
	}
	return backfillVideoKeys(ctx, tx)
}

// addColumnIfMissing adds a column unless the table already has it. Table,
// column and declaration are code constants, never user input.
func addColumnIfMissing(ctx context.Context, tx *sql.Tx, table, column, decl string) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
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

	_, err = tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl))
	return err
}

// backfillVideoKeys derives object keys for rows that only have a full CDN
// URL (https://<domain>/<key>) stored.
func backfillVideoKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
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
		if _, err := tx.ExecContext(ctx, "UPDATE videos SET video_key = ? WHERE id = ?", key, r.id); err != nil {
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
