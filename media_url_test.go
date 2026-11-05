package main

import (
	"testing"

	"github.com/danielakinremi1-dev/tubely/internal/database"
)

func TestResolveMediaBaseURL(t *testing.T) {
	tests := []struct {
		name, media, cf, want string
		wantErr               bool
	}{
		{"media base wins", "http://localhost:9090/tubely-local", "dxxxx.cloudfront.net", "http://localhost:9090/tubely-local", false},
		{"trailing slash trimmed", "https://cdn.example.com/", "", "https://cdn.example.com", false},
		{"legacy cloudfront fallback", "", "dxxxx.cloudfront.net", "https://dxxxx.cloudfront.net", false},
		{"neither set", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMediaBaseURL(tt.media, tt.cf)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMediaURL(t *testing.T) {
	cfg := apiConfig{mediaBaseURL: "http://localhost:9090/tubely-local"}
	for _, key := range []string{"landscape/abc.mp4", "/landscape/abc.mp4"} {
		if got, want := cfg.mediaURL(key), "http://localhost:9090/tubely-local/landscape/abc.mp4"; got != want {
			t.Errorf("mediaURL(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestWithMediaURLs(t *testing.T) {
	cfg := apiConfig{mediaBaseURL: "http://localhost:9090/tubely-local"}
	key := "portrait/abc.mp4"
	legacy := "https://old.example.com/x.mp4"

	got := cfg.withMediaURLs(database.Video{VideoKey: &key, VideoURL: &legacy})
	if got.VideoURL == nil || *got.VideoURL != "http://localhost:9090/tubely-local/portrait/abc.mp4" {
		t.Errorf("with key: VideoURL = %v", got.VideoURL)
	}

	got = cfg.withMediaURLs(database.Video{VideoURL: &legacy})
	if got.VideoURL == nil || *got.VideoURL != legacy {
		t.Errorf("without key: VideoURL = %v, want legacy %q", got.VideoURL, legacy)
	}

	thumb := "thumbnails/t.png"
	got = cfg.withMediaURLs(database.Video{ThumbnailKey: &thumb})
	if got.ThumbnailURL == nil || *got.ThumbnailURL != "http://localhost:9090/tubely-local/thumbnails/t.png" {
		t.Errorf("thumbnail: ThumbnailURL = %v", got.ThumbnailURL)
	}
}
