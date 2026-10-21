package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
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
