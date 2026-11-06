package api

import (
	"testing"

	"github.com/danielakinremi1-dev/tubely/internal/database"
)

func TestMediaURL(t *testing.T) {
	srv := Server{mediaBaseURL: "http://localhost:9090/tubely-local"}
	for _, key := range []string{"landscape/abc.mp4", "/landscape/abc.mp4"} {
		if got, want := srv.mediaURL(key), "http://localhost:9090/tubely-local/landscape/abc.mp4"; got != want {
			t.Errorf("mediaURL(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestWithMediaURLs(t *testing.T) {
	srv := Server{mediaBaseURL: "http://localhost:9090/tubely-local"}
	key := "portrait/abc.mp4"
	legacy := "https://old.example.com/x.mp4"

	got := srv.withMediaURLs(database.Video{VideoKey: &key, VideoURL: &legacy})
	if got.VideoURL == nil || *got.VideoURL != "http://localhost:9090/tubely-local/portrait/abc.mp4" {
		t.Errorf("with key: VideoURL = %v", got.VideoURL)
	}

	got = srv.withMediaURLs(database.Video{VideoURL: &legacy})
	if got.VideoURL == nil || *got.VideoURL != legacy {
		t.Errorf("without key: VideoURL = %v, want legacy %q", got.VideoURL, legacy)
	}

	thumb := "thumbnails/t.png"
	got = srv.withMediaURLs(database.Video{ThumbnailKey: &thumb})
	if got.ThumbnailURL == nil || *got.ThumbnailURL != "http://localhost:9090/tubely-local/thumbnails/t.png" {
		t.Errorf("thumbnail: ThumbnailURL = %v", got.ThumbnailURL)
	}
}
