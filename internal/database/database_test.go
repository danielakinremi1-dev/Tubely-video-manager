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
