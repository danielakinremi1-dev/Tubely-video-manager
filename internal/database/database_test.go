package database

import (
	"errors"
	"path/filepath"
	"testing"
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
