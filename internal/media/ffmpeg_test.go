package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFFmpegProbe(t *testing.T) {
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
			info, err := FFmpeg{}.Probe(context.Background(), tt.file(t))
			got := ""
			if err == nil {
				got = info.AspectRatio()
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidMedia) {
				t.Errorf("err = %v, want it to wrap ErrInvalidMedia", err)
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

func TestFFmpegFastStart(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	in := filepath.Join(t.TempDir(), "in.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc=duration=0.2:size=320x180:rate=10", "-pix_fmt", "yuv420p", in)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, b)
	}
	out, err := FFmpeg{}.FastStart(context.Background(), in)
	if err != nil {
		t.Fatalf("FastStart: %v", err)
	}
	defer os.Remove(out)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	moov, mdat := strings.Index(string(data), "moov"), strings.Index(string(data), "mdat")
	if moov < 0 || mdat < 0 || moov > mdat {
		t.Errorf("moov at %d, mdat at %d: want moov first", moov, mdat)
	}

	bad := filepath.Join(t.TempDir(), "bad.mp4")
	os.WriteFile(bad, []byte("not a video"), 0o644)
	if _, err := (FFmpeg{}).FastStart(context.Background(), bad); err == nil {
		t.Error("FastStart of a non-video succeeded")
	}
	if _, err := os.Stat(bad + ".processing"); !os.IsNotExist(err) {
		t.Error("failed FastStart left its output file behind")
	}
}
