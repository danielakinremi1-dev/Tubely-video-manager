package main

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewObjectKey(t *testing.T) {
	a := newObjectKey("thumbnails/", ".png")
	b := newObjectKey("thumbnails/", ".png")
	if !strings.HasPrefix(a, "thumbnails/") || !strings.HasSuffix(a, ".png") {
		t.Errorf("key %q lacks prefix/suffix", a)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(a, "thumbnails/"), ".png")
	if _, err := uuid.Parse(id); err != nil {
		t.Errorf("key %q doesn't contain a UUID: %v", a, err)
	}
	if a == b {
		t.Errorf("two keys are identical: %q", a)
	}
}

func TestSniffMediaType(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"png", "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16), "image/png"},
		{"jpeg", "\xff\xd8\xff\xe0" + strings.Repeat("\x00", 16), "image/jpeg"},
		{"mp4", "\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom", "video/mp4"},
		{"text strips charset", "hello", "text/plain"},
		{"empty", "", "text/plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.data)
			got, err := sniffMediaType(r)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if pos, _ := r.Seek(0, io.SeekCurrent); pos != 0 {
				t.Errorf("reader not rewound: at %d", pos)
			}
		})
	}
}
