// Package media inspects and prepares uploaded videos. Handlers depend on
// the Processor interface; FFmpeg is the real implementation and Fake stands
// in for it in tests, so they don't need ffmpeg installed.
package media

import (
	"context"
	"errors"
)

// ErrInvalidMedia marks a file the media tools rejected: the client's
// problem (422), unlike a missing binary or a timeout (500).
var ErrInvalidMedia = errors.New("not a playable video")

// Processor probes and prepares uploaded videos.
type Processor interface {
	// Probe reads the dimensions of the file's first video stream. The error
	// wraps ErrInvalidMedia when the file isn't a playable video.
	Probe(ctx context.Context, path string) (Info, error)
	// FastStart writes a copy of the MP4 at path with its index (the moov
	// atom) moved to the front, so playback can start before the whole file
	// downloads. It returns the new file's path; the caller removes it.
	FastStart(ctx context.Context, path string) (string, error)
}

// Info describes a video's first stream.
type Info struct {
	Width, Height int
}

// Aspect ratio classes, used as object key prefixes.
const (
	Landscape = "16:9"
	Portrait  = "9:16"
	Other     = "other"
)

// AspectRatio classifies the stream's shape.
func (i Info) AspectRatio() string {
	return ClassifyAspectRatio(i.Width, i.Height)
}

// ClassifyAspectRatio reports whether width x height is close to 16:9
// (1.78, within 1.72-1.84), close to 9:16 (0.5625, within 0.50-0.62), or
// neither. The tolerance absorbs encoder rounding such as 1920x1088.
func ClassifyAspectRatio(width, height int) string {
	if width <= 0 || height <= 0 {
		return Other
	}
	ratio := float64(width) / float64(height)
	switch {
	case ratio >= 1.72 && ratio <= 1.84:
		return Landscape
	case ratio >= 0.5 && ratio <= 0.62:
		return Portrait
	default:
		return Other
	}
}
