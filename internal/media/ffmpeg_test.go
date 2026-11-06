package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Sample media in testdata/ (a few KB each, generated once with ffmpeg's
// testsrc and sine sources). The MP4s are not fast-start: moov comes after
// mdat, as most cameras and encoders write it.
func sample(name string) string { return filepath.Join("testdata", name) }

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}

func TestFFmpegProbe(t *testing.T) {
	requireTool(t, "ffprobe")
	notMedia := filepath.Join(t.TempDir(), "x.mp4")
	if err := os.WriteFile(notMedia, []byte("definitely not a video"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, path string
		want       Info
		wantErr    bool
	}{
		{"landscape", sample("landscape.mp4"), Info{320, 180}, false},
		{"portrait", sample("portrait.mp4"), Info{180, 320}, false},
		{"other", sample("other.mp4"), Info{240, 180}, false},
		{"audio only", sample("audio-only.m4a"), Info{}, true},
		{"not media", notMedia, Info{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FFmpeg{}.Probe(context.Background(), tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidMedia) {
				t.Errorf("err = %v, want it to wrap ErrInvalidMedia", err)
			}
			if got != tt.want {
				t.Errorf("Info = %+v, want %+v", got, tt.want)
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
	requireTool(t, "ffmpeg")
	moovBeforeMdat := func(data []byte) bool {
		moov, mdat := bytes.Index(data, []byte("moov")), bytes.Index(data, []byte("mdat"))
		return moov >= 0 && mdat >= 0 && moov < mdat
	}

	// Work on a copy: FastStart writes next to its input.
	orig, err := os.ReadFile(sample("landscape.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if moovBeforeMdat(orig) {
		t.Fatal("testdata/landscape.mp4 is already fast-start; the test would prove nothing")
	}
	in := filepath.Join(t.TempDir(), "in.mp4")
	if err := os.WriteFile(in, orig, 0o644); err != nil {
		t.Fatal(err)
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
	if !moovBeforeMdat(data) {
		t.Error("output isn't fast-start: moov should come before mdat")
	}
	if info, err := (FFmpeg{}).Probe(context.Background(), out); err != nil || info != (Info{320, 180}) {
		t.Errorf("remuxed file probes as %+v, %v", info, err)
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

func TestFFmpegCheckInstalled(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err == nil {
		if err := (FFmpeg{}).CheckInstalled(); err != nil {
			t.Errorf("CheckInstalled with tools on PATH: %v", err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	err := FFmpeg{}.CheckInstalled()
	if err == nil || !strings.Contains(err.Error(), "ffmpeg must be installed") {
		t.Errorf("CheckInstalled with empty PATH: err = %v", err)
	}
}
