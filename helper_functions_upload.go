package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
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

// newObjectKey returns a random, URL-safe object key such as
// "thumbnails/<43 random chars>.png".
func newObjectKey(prefix, ext string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate object key: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b) + ext, nil
}

func getExtension(mediaType string) string {
	extensions := strings.Split(mediaType, "/")
	if len(extensions) != 2 {
		return ".bin"
	}
	return "." + extensions[1]
}

func getVideoAspectRatio(ctx context.Context, filePath string) (string, error) {
	type VideoData struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	videoInfo := VideoData{}

	// Only look at the first video stream; an audio stream can come first.
	out, err := runTool(ctx, ffprobeTimeout, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-print_format", "json", "-show_streams", filePath)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(out, &videoInfo); err != nil {
		return "", fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(videoInfo.Streams) == 0 {
		return "", errors.New("no video stream found")
	}
	stream := videoInfo.Streams[0]
	if stream.Width <= 0 || stream.Height <= 0 {
		return "", fmt.Errorf("invalid video dimensions %dx%d", stream.Width, stream.Height)
	}

	aspectRatio := float64(stream.Width) / float64(stream.Height)

	if aspectRatio >= 1.72 && aspectRatio <= 1.84 {
		return "16:9", nil
	}

	if aspectRatio >= 0.5 && aspectRatio <= 0.62 {
		return "9:16", nil
	}

	return "other", nil
}

func processVideoForFastStart(ctx context.Context, filePath string) (string, error) {
	newPath := filePath + ".processing"

	_, err := runTool(ctx, ffmpegTimeout, "ffmpeg", "-v", "error", "-i", filePath,
		"-c", "copy", "-movflags", "faststart", "-f", "mp4", newPath)
	if err != nil {
		os.Remove(newPath) // ffmpeg may leave a partial output file
		return "", err
	}

	return newPath, nil
}
