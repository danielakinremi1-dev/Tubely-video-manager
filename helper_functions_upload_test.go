package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestGetVideoAspectRatio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	dir := t.TempDir()

	gen := func(t *testing.T, name string, args ...string) string {
		t.Helper()
		out := filepath.Join(dir, name)
		cmd := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y"}, args...), out)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %s: %v: %s", name, err, b)
		}
		return out
	}
	video := func(t *testing.T, name, size string) string {
		return gen(t, name, "-f", "lavfi", "-i", "testsrc=duration=0.2:size="+size+":rate=10", "-pix_fmt", "yuv420p")
	}

	tests := []struct {
		name    string
		file    func(t *testing.T) string
		want    string
		wantErr bool
	}{
		{"landscape", func(t *testing.T) string { return video(t, "l.mp4", "1280x720") }, "16:9", false},
		{"portrait", func(t *testing.T) string { return video(t, "p.mp4", "720x1280") }, "9:16", false},
		{"other", func(t *testing.T) string { return video(t, "o.mp4", "640x480") }, "other", false},
		{"audio only", func(t *testing.T) string {
			return gen(t, "a.m4a", "-f", "lavfi", "-i", "sine=duration=0.2")
		}, "", true},
		{"not media", func(t *testing.T) string {
			p := filepath.Join(dir, "x.mp4")
			if err := os.WriteFile(p, []byte("definitely not a video"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getVideoAspectRatio(context.Background(), tt.file(t))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, errInvalidMedia) {
				t.Errorf("err = %v, want it to wrap errInvalidMedia", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunToolTimeoutAndStderr(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	_, err := runTool(context.Background(), 50*time.Millisecond, "sh", "-c", "sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("slow command: err = %v, want DeadlineExceeded", err)
	}

	_, err = runTool(context.Background(), time.Second, "sh", "-c", "echo boom >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failing command: err = %v, want stderr in message", err)
	}

	out, err := runTool(context.Background(), time.Second, "sh", "-c", "echo ok")
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("ok command: out = %q, err = %v", out, err)
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
