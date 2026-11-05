package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

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

func getVideoAspectRatio(filePath string) (string, error) {
	type VideoData struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	videoInfo := VideoData{}

	// Only look at the first video stream; an audio stream can come first.
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-print_format", "json", "-show_streams", filePath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffprobe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	if err := json.Unmarshal(stdout.Bytes(), &videoInfo); err != nil {
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

func processVideoForFastStart(filePath string) (string, error) {
	newPath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newPath)
	err := cmd.Run()
	if err != nil {
		os.Remove(newPath) // ffmpeg may leave a partial output file
		return "", err
	}

	return newPath, nil
}
