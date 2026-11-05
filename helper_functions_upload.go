package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
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

// newObjectKey returns a unique, URL-safe object key such as
// "thumbnails/0b7c3c1e-5f0a-4d3e-9c47-2a8f1e6d9b10.png". UUIDv4 carries 122
// random bits from crypto/rand, so keys are unguessable and never collide.
func newObjectKey(prefix, ext string) string {
	return prefix + uuid.NewString() + ext
}

// uploadFormError maps an error from parsing a multipart upload to a status
// and message: 413 when the body hit its http.MaxBytesReader cap, 400 for a
// missing file field or a malformed form.
func uploadFormError(err error, field string, limit int64) (int, string) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return http.StatusRequestEntityTooLarge, fmt.Sprintf("Upload is larger than %d MB", limit>>20)
	}
	if errors.Is(err, http.ErrMissingFile) {
		return http.StatusBadRequest, fmt.Sprintf("Upload form has no %q file", field)
	}
	return http.StatusBadRequest, "Unable to parse upload form"
}

// sniffMediaType returns a file's type as detected from its first 512 bytes
// (the WHATWG MIME-sniffing algorithm behind http.DetectContentType) and
// rewinds the file. Validation then depends on the bytes, not on the
// client-supplied Content-Type, which anyone can set to anything.
func sniffMediaType(f io.ReadSeeker) (string, error) {
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read file header: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind file: %w", err)
	}
	// Drop parameters such as "; charset=utf-8".
	mediaType, _, err := mime.ParseMediaType(http.DetectContentType(buf[:n]))
	if err != nil {
		return "", fmt.Errorf("parse detected type: %w", err)
	}
	return mediaType, nil
}

func getExtension(mediaType string) string {
	extensions := strings.Split(mediaType, "/")
	if len(extensions) != 2 {
		return ".bin"
	}
	return "." + extensions[1]
}

// errInvalidMedia marks a file the media tools rejected: the client's
// problem (422), unlike a missing binary or a timeout (500).
var errInvalidMedia = errors.New("not a playable video")

// getVideoAspectRatio probes the first video stream. Errors wrap
// errInvalidMedia when ffprobe rejected the file itself.
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
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%w: %w", errInvalidMedia, err)
		}
		return "", err
	}

	if err := json.Unmarshal(out, &videoInfo); err != nil {
		return "", fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(videoInfo.Streams) == 0 {
		return "", fmt.Errorf("%w: no video stream found", errInvalidMedia)
	}
	stream := videoInfo.Streams[0]
	if stream.Width <= 0 || stream.Height <= 0 {
		return "", fmt.Errorf("%w: invalid video dimensions %dx%d", errInvalidMedia, stream.Width, stream.Height)
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
