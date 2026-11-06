package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMemory(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	if err := m.Put(ctx, "b/2.png", "image/png", strings.NewReader("png")); err != nil {
		t.Fatal(err)
	}
	if err := m.Put(ctx, "a/1.mp4", "video/mp4", strings.NewReader("mp4")); err != nil {
		t.Fatal(err)
	}
	if got := m.Keys(); len(got) != 2 || got[0] != "a/1.mp4" || got[1] != "b/2.png" {
		t.Errorf("Keys = %v", got)
	}
	if o, ok := m.Get("a/1.mp4"); !ok || o.ContentType != "video/mp4" || string(o.Data) != "mp4" {
		t.Errorf("Get = %+v, %v", o, ok)
	}

	if err := m.Delete(ctx, "a/1.mp4"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "missing"); err != nil {
		t.Errorf("deleting a missing key: %v", err)
	}
	if _, ok := m.Get("a/1.mp4"); ok {
		t.Error("object still there after Delete")
	}

	boom := errors.New("boom")
	m.PutErr = boom
	if err := m.Put(ctx, "c", "x", strings.NewReader("x")); !errors.Is(err, boom) {
		t.Errorf("Put with PutErr: %v", err)
	}
	if _, ok := m.Get("c"); ok {
		t.Error("failed Put stored an object")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	m.PutErr = nil
	if err := m.Put(cancelled, "d", "x", strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("Put with cancelled ctx: %v", err)
	}
}
