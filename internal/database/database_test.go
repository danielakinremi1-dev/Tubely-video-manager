package database

import (
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
	t.Cleanup(func() { c.db.Close() })
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

func TestMigrationIsIdempotentAndBackfillsKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	c, err := NewClient(path)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	u := createTestUser(t, c)

	legacy, err := c.CreateVideo(CreateVideoParams{Title: "legacy", UserID: u.ID})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	broken, err := c.CreateVideo(CreateVideoParams{Title: "broken", UserID: u.ID})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	// Simulate rows written before video_key existed.
	for id, url := range map[string]string{
		legacy.ID.String(): "https://dxxxx.cloudfront.net/portrait/abc.mp4",
		broken.ID.String(): "not a url",
	} {
		if _, err := c.db.Exec("UPDATE videos SET video_url = ?, video_key = NULL WHERE id = ?", url, id); err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
	}
	c.db.Close()

	// Re-opening runs the migration again: it must not fail, and it must
	// backfill keys from legacy URLs.
	c, err = NewClient(path)
	if err != nil {
		t.Fatalf("second NewClient: %v", err)
	}
	t.Cleanup(func() { c.db.Close() })

	got, err := c.GetVideo(legacy.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.VideoKey == nil || *got.VideoKey != "portrait/abc.mp4" {
		t.Errorf("legacy VideoKey = %v, want portrait/abc.mp4", got.VideoKey)
	}
	got, err = c.GetVideo(broken.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.VideoKey != nil {
		t.Errorf("unparseable URL VideoKey = %q, want nil", *got.VideoKey)
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
	v.VideoKey = &key
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
}
