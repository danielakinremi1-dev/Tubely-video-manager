package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
			got, err := getVideoAspectRatio(tt.file(t))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
