package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/danielakinremi1-dev/tubely/internal/media"
	"github.com/danielakinremi1-dev/tubely/internal/storage"
	"github.com/google/uuid"
)

// The upload path end to end, with storage.Memory and media.Fake standing
// in for S3 and ffmpeg: no network, no external binaries.

// mp4Bytes sniffs as video/mp4; the fake processor doesn't parse it.
var mp4Bytes = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom" + strings.Repeat("v", 256))
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

type uploadEnv struct {
	srv     *Server
	store   *storage.Memory
	fake    *media.Fake
	token   string
	videoID uuid.UUID
	tmpDir  string
}

// newUploadEnv returns a server with one user, one video, and TMPDIR pointed
// at an empty directory so tests can check that no temp files are left.
func newUploadEnv(t *testing.T) *uploadEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	srv := newTestServer(t)
	owner, token := newTestUser(t, srv, "owner@example.com")
	return &uploadEnv{
		srv:     srv,
		store:   srv.store.(*storage.Memory),
		fake:    srv.processor.(*media.Fake),
		token:   token,
		videoID: newTestVideo(t, srv, owner).ID,
		tmpDir:  tmp,
	}
}

func (e *uploadEnv) upload(t *testing.T, kind string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	field, path, ctype := "video", "/api/video_upload/", "video/mp4"
	if kind == "thumbnail" {
		field, path, ctype = "thumbnail", "/api/thumbnail_upload/", "image/png"
	}
	body, formType := multipartBody(t, field, ctype, data)
	req := httptest.NewRequest("POST", path+e.videoID.String(), body)
	req.Header.Set("Content-Type", formType)
	req.Header.Set("Authorization", "Bearer "+e.token)
	return serve(e.srv, req)
}

func (e *uploadEnv) stored(t *testing.T) database.Video {
	t.Helper()
	v, err := e.srv.db.GetVideo(e.videoID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	return v
}

func (e *uploadEnv) assertNoTempFiles(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(e.tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		t.Errorf("temp file left behind: %s", en.Name())
	}
}

func decodeVideo(t *testing.T, rec *httptest.ResponseRecorder) database.Video {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var v database.Video
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVideoUploadSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		info   media.Info
		prefix string
	}{
		{"landscape", media.Info{Width: 1920, Height: 1080}, "landscape/"},
		{"portrait", media.Info{Width: 1080, Height: 1920}, "portrait/"},
		{"other", media.Info{Width: 640, Height: 480}, "other/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newUploadEnv(t)
			e.fake.Info = tc.info

			got := decodeVideo(t, e.upload(t, "video", mp4Bytes))

			keys := e.store.Keys()
			if len(keys) != 1 || !strings.HasPrefix(keys[0], tc.prefix) || !strings.HasSuffix(keys[0], ".mp4") {
				t.Fatalf("stored keys = %v, want one %s*.mp4", keys, tc.prefix)
			}
			obj, _ := e.store.Get(keys[0])
			if obj.ContentType != "video/mp4" || string(obj.Data) != string(mp4Bytes) {
				t.Errorf("stored object: type %q, %d bytes", obj.ContentType, len(obj.Data))
			}
			if got.VideoURL == nil || *got.VideoURL != "http://media.test/"+keys[0] {
				t.Errorf("video_url = %v, want http://media.test/%s", got.VideoURL, keys[0])
			}
			if v := e.stored(t); v.VideoKey == nil || *v.VideoKey != keys[0] {
				t.Errorf("DB video_key = %v, want %s", v.VideoKey, keys[0])
			}
			e.assertNoTempFiles(t)
		})
	}
}

func TestReuploadDeletesPreviousObject(t *testing.T) {
	for _, kind := range []string{"video", "thumbnail"} {
		t.Run(kind, func(t *testing.T) {
			e := newUploadEnv(t)
			data := mp4Bytes
			if kind == "thumbnail" {
				data = pngBytes
			}
			decodeVideo(t, e.upload(t, kind, data))
			first := e.store.Keys()
			decodeVideo(t, e.upload(t, kind, data))
			second := e.store.Keys()

			if len(first) != 1 || len(second) != 1 || first[0] == second[0] {
				t.Errorf("keys after first upload %v, after second %v: want one, replaced", first, second)
			}
		})
	}
}

func TestThumbnailUploadSuccess(t *testing.T) {
	e := newUploadEnv(t)
	got := decodeVideo(t, e.upload(t, "thumbnail", pngBytes))

	keys := e.store.Keys()
	if len(keys) != 1 || !strings.HasPrefix(keys[0], "thumbnails/") || !strings.HasSuffix(keys[0], ".png") {
		t.Fatalf("stored keys = %v, want one thumbnails/*.png", keys)
	}
	if obj, _ := e.store.Get(keys[0]); obj.ContentType != "image/png" {
		t.Errorf("content type = %q", obj.ContentType)
	}
	if got.ThumbnailURL == nil || *got.ThumbnailURL != "http://media.test/"+keys[0] {
		t.Errorf("thumbnail_url = %v", got.ThumbnailURL)
	}
	e.assertNoTempFiles(t)
}

// Every failure after validation must leave the bucket, the DB row and the
// temp directory as they were (or the row gone, when that's the failure).
func TestVideoUploadFailuresLeaveNothingBehind(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		setup func(e *uploadEnv)
		want  int
	}{
		{"storage put fails", func(e *uploadEnv) { e.store.PutErr = boom }, http.StatusInternalServerError},
		{"probe rejects file", func(e *uploadEnv) {
			e.fake.ProbeErr = errors.Join(media.ErrInvalidMedia, boom)
		}, http.StatusUnprocessableEntity},
		{"probe binary fails", func(e *uploadEnv) { e.fake.ProbeErr = boom }, http.StatusInternalServerError},
		{"fast start fails", func(e *uploadEnv) { e.fake.FastStartErr = boom }, http.StatusInternalServerError},
		// The object is uploaded, then saving the key fails: the new object
		// must be deleted again.
		{"database fails after upload", func(e *uploadEnv) {
			e.fake.OnFastStart = func() { e.srv.db.Close() }
		}, http.StatusInternalServerError},
		{"video deleted mid-upload", func(e *uploadEnv) {
			e.fake.OnFastStart = func() { e.srv.db.DeleteVideo(e.videoID) }
		}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newUploadEnv(t)
			tc.setup(e)

			assertJSONError(t, e.upload(t, "video", mp4Bytes), tc.want)

			if keys := e.store.Keys(); len(keys) != 0 {
				t.Errorf("objects left in storage: %v", keys)
			}
			if tc.name != "database fails after upload" && tc.name != "video deleted mid-upload" {
				if v := e.stored(t); v.VideoKey != nil {
					t.Errorf("DB video_key changed to %q", *v.VideoKey)
				}
			}
			e.assertNoTempFiles(t)
		})
	}
}

func TestDeleteRemovesStoredObjects(t *testing.T) {
	e := newUploadEnv(t)
	decodeVideo(t, e.upload(t, "video", mp4Bytes))
	decodeVideo(t, e.upload(t, "thumbnail", pngBytes))
	if n := len(e.store.Keys()); n != 2 {
		t.Fatalf("%d objects stored, want 2", n)
	}

	req := httptest.NewRequest("DELETE", "/api/videos/"+e.videoID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	if rec := serve(e.srv, req); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d (body %s)", rec.Code, rec.Body)
	}
	if keys := e.store.Keys(); len(keys) != 0 {
		t.Errorf("objects left after delete: %v", keys)
	}
	if _, err := e.srv.db.GetVideo(e.videoID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("row after delete: err = %v", err)
	}
}

// Storage cleanup failures are logged, not returned: the delete still
// succeeds and the row is gone.
func TestDeleteSurvivesStorageCleanupFailure(t *testing.T) {
	e := newUploadEnv(t)
	decodeVideo(t, e.upload(t, "video", mp4Bytes))
	e.store.DeleteErr = errors.New("s3 down")

	req := httptest.NewRequest("DELETE", "/api/videos/"+e.videoID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	if rec := serve(e.srv, req); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", rec.Code)
	}
	if _, err := e.srv.db.GetVideo(e.videoID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("row should be gone even though storage cleanup failed: %v", err)
	}
}
