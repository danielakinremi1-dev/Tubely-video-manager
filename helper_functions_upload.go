package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Run()

	err := json.Unmarshal(b.Bytes(), &videoInfo)
	if err != nil {
		return "", err
	}

	aspectRatio := float64(videoInfo.Streams[0].Width) / float64(videoInfo.Streams[0].Height)

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
		return "", err
	}

	return newPath, nil
}
