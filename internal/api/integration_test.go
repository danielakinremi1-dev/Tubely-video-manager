package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/danielakinremi1-dev/tubely/internal/media"
	"github.com/danielakinremi1-dev/tubely/internal/storage"
)

// End-to-end through the real router, the real S3 client against the
// emulator, and real ffmpeg. Skipped unless TUBELY_TEST_S3_ENDPOINT is set:
//
//	make test-integration
//
// CI runs it with the emulator as a service container.
func newIntegrationServer(t *testing.T) *Server {
	t.Helper()
	endpoint := os.Getenv("TUBELY_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TUBELY_TEST_S3_ENDPOINT not set")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	bucket := os.Getenv("TUBELY_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "tubely-local"
	}
	// Emulators ignore credentials, but the SDK still signs requests.
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		t.Setenv("AWS_ACCESS_KEY_ID", "test")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	}
	store, err := storage.NewS3(context.Background(), storage.S3Config{
		Bucket: bucket, Region: "us-east-1", Endpoint: endpoint, ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	db, err := database.NewClient(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewServer(Config{
		DB:        db,
		Store:     store,
		Processor: media.FFmpeg{},
		JWTSecret: "test-secret",
		// The emulator serves objects path-style, like MEDIA_BASE_URL in
		// .env.example.
		MediaBaseURL: strings.TrimRight(endpoint, "/") + "/" + bucket,
		Logger:       slog.New(slog.DiscardHandler),
	})
}

func TestIntegrationUploadPlayDelete(t *testing.T) {
	srv := newIntegrationServer(t)
	owner, token := newTestUser(t, srv, "owner@example.com")
	video := newTestVideo(t, srv, owner)
	mp4, err := os.ReadFile("../media/testdata/landscape.mp4") // 320x180, not fast-start
	if err != nil {
		t.Fatal(err)
	}

	post := func(path, field, ctype string, data []byte) database.Video {
		t.Helper()
		body, formType := multipartBody(t, field, ctype, data)
		req := httptest.NewRequest(http.MethodPost, path+video.ID.String(), body)
		req.Header.Set("Content-Type", formType)
		req.Header.Set("Authorization", "Bearer "+token)
		return decodeVideo(t, serve(srv, req))
	}
	post("/api/thumbnail_upload/", "thumbnail", "image/png", pngBytes)
	got := post("/api/video_upload/", "video", "video/mp4", mp4)

	// The key is stored, and the URL is built from it at response time.
	row, err := srv.db.GetVideo(video.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if row.VideoKey == nil || !strings.HasPrefix(*row.VideoKey, "landscape/") || row.ThumbnailKey == nil {
		t.Fatalf("stored keys: video %v, thumbnail %v", row.VideoKey, row.ThumbnailKey)
	}
	wantURL := srv.mediaBaseURL + "/" + *row.VideoKey
	if got.VideoURL == nil || *got.VideoURL != wantURL || got.ThumbnailURL == nil {
		t.Fatalf("video_url = %v, want %s (thumbnail_url %v)", got.VideoURL, wantURL, got.ThumbnailURL)
	}

	// The URL really plays: the emulator serves the remuxed, fast-start file.
	status, served := fetch(t, *got.VideoURL)
	if status != http.StatusOK {
		t.Fatalf("GET video_url = %d, want 200", status)
	}
	moov, mdat := bytes.Index(served, []byte("moov")), bytes.Index(served, []byte("mdat"))
	if moov < 0 || mdat < 0 || moov > mdat {
		t.Errorf("served video isn't fast-start (moov at %d, mdat at %d)", moov, mdat)
	}
	if status, _ := fetch(t, *got.ThumbnailURL); status != http.StatusOK {
		t.Errorf("GET thumbnail_url = %d, want 200", status)
	}

	// Deleting the video removes its objects from the bucket.
	req := httptest.NewRequest(http.MethodDelete, "/api/videos/"+video.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if rec := serve(srv, req); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204 (body %s)", rec.Code, rec.Body)
	}
	for _, u := range []string{*got.VideoURL, *got.ThumbnailURL} {
		if status, _ := fetch(t, u); status != http.StatusNotFound {
			t.Errorf("GET %s after delete = %d, want 404", u, status)
		}
	}
}

// fetch GETs url and returns the status and body.
func fetch(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, body
}
