package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Upper bounds for the external media tools. ffprobe only reads headers;
// ffmpeg remuxes (-c copy, no re-encode), which is I/O-bound even for a 1 GB
// upload. A hung process is killed instead of pinning the request forever.
const (
	ffprobeTimeout = 30 * time.Second
	ffmpegTimeout  = 5 * time.Minute
)

// FFmpeg is a Processor that runs ffprobe and ffmpeg from PATH.
type FFmpeg struct{}

var _ Processor = FFmpeg{}

func (FFmpeg) Probe(ctx context.Context, path string) (Info, error) {
	out, err := runTool(ctx, ffprobeTimeout, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-print_format", "json", "-show_streams", path)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Info{}, fmt.Errorf("%w: %w", ErrInvalidMedia, err)
		}
		return Info{}, err
	}

	var probe struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return Info{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(probe.Streams) == 0 {
		return Info{}, fmt.Errorf("%w: no video stream found", ErrInvalidMedia)
	}
	s := probe.Streams[0]
	if s.Width <= 0 || s.Height <= 0 {
		return Info{}, fmt.Errorf("%w: invalid video dimensions %dx%d", ErrInvalidMedia, s.Width, s.Height)
	}
	return Info{Width: s.Width, Height: s.Height}, nil
}

func (FFmpeg) FastStart(ctx context.Context, path string) (string, error) {
	newPath := path + ".processing"
	_, err := runTool(ctx, ffmpegTimeout, "ffmpeg", "-v", "error", "-i", path,
		"-c", "copy", "-movflags", "faststart", "-f", "mp4", newPath)
	if err != nil {
		os.Remove(newPath) // ffmpeg may leave a partial output file
		return "", err
	}
	return newPath, nil
}

// runTool runs an external command under a timeout derived from ctx, so a
// cancelled request or a stuck process kills it. It returns stdout, and on
// failure an error that includes the tool's stderr.
func runTool(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	// After a kill, stop waiting on output pipes that a leftover child
	// process may still hold open.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = fmt.Errorf("%w (%v)", ctxErr, err)
		}
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
