package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestClient(t *testing.T) Client {
	t.Helper()
	c, err := NewClient(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestGetUserByRefreshTokenUnknown(t *testing.T) {
	c := newTestClient(t)

	user, err := c.GetUserByRefreshToken("does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if user != nil {
		t.Fatalf("user = %+v, want nil", user)
	}
}

func createTestUser(t *testing.T, c Client) *User {
	t.Helper()
	u, err := c.CreateUser(CreateUserParams{Email: "a@example.com", Password: "hash"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func TestGetUserByRefreshTokenValidity(t *testing.T) {
	c := newTestClient(t)
	u := createTestUser(t, c)

	mustCreate := func(token string, expiresAt time.Time) {
		t.Helper()
		_, err := c.CreateRefreshToken(CreateRefreshTokenParams{Token: token, UserID: u.ID, ExpiresAt: expiresAt})
		if err != nil {
			t.Fatalf("CreateRefreshToken(%s): %v", token, err)
		}
	}
	now := time.Now().UTC()
	mustCreate("valid", now.Add(time.Hour))
	mustCreate("expired", now.Add(-time.Minute))
	mustCreate("revoked", now.Add(time.Hour))
	if err := c.RevokeRefreshToken("revoked"); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}

	got, err := c.GetUserByRefreshToken("valid")
	if err != nil {
		t.Fatalf("valid token: err = %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("valid token: user ID = %v, want %v", got.ID, u.ID)
	}

	for _, token := range []string{"expired", "revoked"} {
		if _, err := c.GetUserByRefreshToken(token); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s token: err = %v, want ErrNotFound", token, err)
		}
	}
}

func TestGetVideoUnknown(t *testing.T) {
	c := newTestClient(t)

	if _, err := c.GetVideo(uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Schemas that existed before goose: the original starter, and what the
// Phase 0.5 auto-migration left (key columns added).
const (
	starterSchema = `
	CREATE TABLE users (id TEXT PRIMARY KEY, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, password TEXT NOT NULL, email TEXT UNIQUE NOT NULL);
	CREATE TABLE refresh_tokens (token TEXT PRIMARY KEY, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, revoked_at TIMESTAMP, user_id TEXT NOT NULL,
		expires_at TIMESTAMP NOT NULL, FOREIGN KEY(user_id) REFERENCES users(id));
	CREATE TABLE videos (id TEXT PRIMARY KEY, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, title TEXT NOT NULL, description TEXT,
		thumbnail_url TEXT, video_url TEXT TEXT, user_id INTEGER, FOREIGN KEY(user_id) REFERENCES users(id));`
	autoMigratedSchema = starterSchema + `
	ALTER TABLE videos ADD COLUMN video_key TEXT;
	ALTER TABLE videos ADD COLUMN thumbnail_key TEXT;`
)

// Goose must adopt databases created before it existed, without losing rows,
// and derive object keys from legacy CDN URLs.
func TestMigrateAdoptsPreGooseDatabases(t *testing.T) {
	for name, schema := range map[string]string{"starter": starterSchema, "auto-migrated": autoMigratedSchema} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			raw, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			userID, legacyID, brokenID, orphanID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			stmts := []string{schema,
				`INSERT INTO users (id, password, email) VALUES ('` + userID.String() + `', 'hash', 'a@example.com')`,
				`INSERT INTO videos (id, title, description, video_url, user_id) VALUES ('` + legacyID.String() +
					`', 'legacy', '', 'https://dxxxx.cloudfront.net/portrait/abc.mp4', '` + userID.String() + `')`,
				// NULL description: allowed by the old schema, unscannable in Go.
				`INSERT INTO videos (id, title, video_url, user_id) VALUES ('` + brokenID.String() +
					`', 'broken', 'not a url', '` + userID.String() + `')`,
				// Owner no longer exists.
				`INSERT INTO videos (id, title, description, user_id) VALUES ('` + orphanID.String() +
					`', 'orphan', '', '` + uuid.NewString() + `')`,
			}
			for _, stmt := range stmts {
				if _, err := raw.Exec(stmt); err != nil {
					t.Fatalf("seed: %v\n%s", err, stmt)
				}
			}
			raw.Close()

			c, err := NewClient(path)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			t.Cleanup(func() { c.Close() })

			got, err := c.GetVideo(legacyID)
			if err != nil {
				t.Fatalf("GetVideo: %v", err)
			}
			if got.VideoKey == nil || *got.VideoKey != "portrait/abc.mp4" {
				t.Errorf("legacy VideoKey = %v, want portrait/abc.mp4", got.VideoKey)
			}
			if got.UserID != userID {
				t.Errorf("UserID = %v, want %v", got.UserID, userID)
			}
			got, err = c.GetVideo(brokenID)
			if err != nil {
				t.Fatalf("GetVideo: %v", err)
			}
			if got.VideoKey != nil {
				t.Errorf("unparseable URL VideoKey = %q, want nil", *got.VideoKey)
			}
			if got.Description != "" {
				t.Errorf("NULL description became %q, want empty", got.Description)
			}
			if _, err := c.GetVideo(orphanID); !errors.Is(err, ErrNotFound) {
				t.Errorf("orphaned video: err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestSchemaFixes(t *testing.T) {
	c := newTestClient(t)

	colType := func(table, column string) (string, bool) {
		t.Helper()
		var typ string
		var notNull bool
		err := c.db.QueryRow("SELECT type, \"notnull\" FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&typ, &notNull)
		if err != nil {
			t.Fatalf("%s.%s: %v", table, column, err)
		}
		return typ, notNull
	}
	for _, tc := range []struct {
		table, column, typ string
		notNull            bool
	}{
		{"videos", "user_id", "TEXT", true},
		{"videos", "video_url", "TEXT", false},
		{"videos", "description", "TEXT", true},
	} {
		if typ, notNull := colType(tc.table, tc.column); typ != tc.typ || notNull != tc.notNull {
			t.Errorf("%s.%s = %s notnull=%v, want %s notnull=%v", tc.table, tc.column, typ, notNull, tc.typ, tc.notNull)
		}
	}
	for _, table := range []string{"videos", "refresh_tokens"} {
		var onDelete string
		err := c.db.QueryRow("SELECT on_delete FROM pragma_foreign_key_list(?) WHERE \"table\" = 'users'", table).Scan(&onDelete)
		if err != nil || onDelete != "CASCADE" {
			t.Errorf("%s -> users: on_delete = %q (%v), want CASCADE", table, onDelete, err)
		}
	}
}

// Every down migration must undo its up migration cleanly.
func TestMigrationsRollBackAndReapply(t *testing.T) {
	c := newTestClient(t)
	u := createTestUser(t, c)
	v, err := c.CreateVideo(CreateVideoParams{Title: "keep me", UserID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	p, err := newMigrationProvider(c.db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := p.DownTo(ctx, 1); err != nil {
		t.Fatalf("DownTo(1): %v", err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if got, err := c.GetVideo(v.ID); err != nil || got.Title != "keep me" {
		t.Errorf("after down/up: %+v, %v", got, err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	c := newTestClient(t)

	// Every pooled connection must have enforcement on, not just the first.
	c.db.SetMaxIdleConns(4)
	conns := make([]*sql.Conn, 3)
	for i := range conns {
		conn, err := c.db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns[i] = conn
		var on int
		if err := conn.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&on); err != nil || on != 1 {
			t.Errorf("connection %d: foreign_keys = %d (%v), want 1", i, on, err)
		}
	}
	for _, conn := range conns {
		conn.Close()
	}

	if _, err := c.CreateVideo(CreateVideoParams{Title: "nobody's", UserID: uuid.New()}); err == nil {
		t.Error("CreateVideo for a nonexistent user succeeded")
	}

	u := createTestUser(t, c)
	v, err := c.CreateVideo(CreateVideoParams{Title: "mine", UserID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRefreshToken(CreateRefreshTokenParams{Token: "tok", UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := c.GetVideo(v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("video after owner deleted: err = %v, want ErrNotFound", err)
	}
	var tokens int
	c.db.QueryRow("SELECT count(*) FROM refresh_tokens WHERE user_id = ?", u.ID).Scan(&tokens)
	if tokens != 0 {
		t.Errorf("%d refresh tokens left after owner deleted", tokens)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := range 2 {
		c, err := NewClient(path)
		if err != nil {
			t.Fatalf("NewClient #%d: %v", i+1, err)
		}
		c.Close()
	}
}

func TestUpdateVideoPersistsKey(t *testing.T) {
	c := newTestClient(t)
	u := createTestUser(t, c)
	v, err := c.CreateVideo(CreateVideoParams{Title: "t", UserID: u.ID})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}

	key := "landscape/xyz.mp4"
	thumb := "thumbnails/abc.png"
	v.VideoKey = &key
	v.ThumbnailKey = &thumb
	if err := c.UpdateVideo(v); err != nil {
		t.Fatalf("UpdateVideo: %v", err)
	}

	videos, err := c.GetVideos(u.ID)
	if err != nil {
		t.Fatalf("GetVideos: %v", err)
	}
	if len(videos) != 1 || videos[0].VideoKey == nil || *videos[0].VideoKey != key {
		t.Fatalf("GetVideos = %+v, want one video with key %q", videos, key)
	}
	if videos[0].ThumbnailKey == nil || *videos[0].ThumbnailKey != thumb {
		t.Fatalf("ThumbnailKey = %v, want %q", videos[0].ThumbnailKey, thumb)
	}
}
