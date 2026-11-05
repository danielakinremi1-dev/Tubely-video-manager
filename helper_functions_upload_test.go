package main

import (
	"strings"
	"testing"
)

func TestNewObjectKey(t *testing.T) {
	a, err := newObjectKey("thumbnails/", ".png")
	if err != nil {
		t.Fatalf("newObjectKey: %v", err)
	}
	b, err := newObjectKey("thumbnails/", ".png")
	if err != nil {
		t.Fatalf("newObjectKey: %v", err)
	}
	if !strings.HasPrefix(a, "thumbnails/") || !strings.HasSuffix(a, ".png") {
		t.Errorf("key %q lacks prefix/suffix", a)
	}
	if strings.ContainsAny(strings.TrimPrefix(a, "thumbnails/"), "/+=") {
		t.Errorf("key %q is not URL-safe", a)
	}
	if a == b {
		t.Errorf("two keys are identical: %q", a)
	}
}
