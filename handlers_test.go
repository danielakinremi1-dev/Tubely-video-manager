package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/google/uuid"
)

// newTestConfig returns an apiConfig backed by a throwaway SQLite file. It has
// no S3 client, so it only suits tests that fail before storage is touched.
func newTestConfig(t *testing.T) *apiConfig {
	t.Helper()
	db, err := database.NewClient(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &apiConfig{db: db, jwtSecret: "test-secret", mediaBaseURL: "http://media.test"}
}

// newTestUser creates a user and returns its ID and a valid access token.
func newTestUser(t *testing.T, cfg *apiConfig, email string) (uuid.UUID, string) {
	t.Helper()
	u, err := cfg.db.CreateUser(database.CreateUserParams{Email: email, Password: "hash"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := auth.MakeJWT(u.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	return u.ID, token
}

func newTestVideo(t *testing.T, cfg *apiConfig, owner uuid.UUID) database.Video {
	t.Helper()
	v, err := cfg.db.CreateVideo(database.CreateVideoParams{Title: "t", UserID: owner})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	return v
}

// multipartBody builds a one-file multipart form.
func multipartBody(t *testing.T, field, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="f"`)
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

// serve routes one request through the real mux pattern so PathValue works.
func serve(pattern string, h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// assertJSONError checks the status code and that the body is {"error": ...}.
func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Errorf("body %q is not a JSON error", rec.Body)
	}
}

func TestUploadOwnershipIsForbidden(t *testing.T) {
	cfg := newTestConfig(t)
	owner, _ := newTestUser(t, cfg, "owner@example.com")
	_, otherToken := newTestUser(t, cfg, "other@example.com")
	video := newTestVideo(t, cfg, owner)

	cases := []struct {
		name, pattern, path, field, ctype string
		h                                 http.HandlerFunc
	}{
		{"video", "POST /api/video_upload/{videoID}", "/api/video_upload/", "video", "video/mp4", cfg.handlerUploadVideo},
		{"thumbnail", "POST /api/thumbnail_upload/{videoID}", "/api/thumbnail_upload/", "thumbnail", "image/png", cfg.handlerUploadThumbnail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, ctype := multipartBody(t, tc.field, tc.ctype, []byte("data"))
			req := httptest.NewRequest("POST", tc.path+video.ID.String(), body)
			req.Header.Set("Content-Type", ctype)
			req.Header.Set("Authorization", "Bearer "+otherToken)
			assertJSONError(t, serve(tc.pattern, tc.h, req), http.StatusForbidden)
		})
	}
}

func TestUploadRejectsSpoofedContentType(t *testing.T) {
	cfg := newTestConfig(t)
	owner, token := newTestUser(t, cfg, "owner@example.com")
	video := newTestVideo(t, cfg, owner)

	cases := []struct {
		name, pattern, path, field, claimed string
		data                                []byte
		h                                   http.HandlerFunc
	}{
		{"pdf claiming mp4", "POST /api/video_upload/{videoID}", "/api/video_upload/", "video", "video/mp4",
			[]byte("%PDF-1.4\n%fake"), cfg.handlerUploadVideo},
		{"gif claiming png", "POST /api/thumbnail_upload/{videoID}", "/api/thumbnail_upload/", "thumbnail", "image/png",
			[]byte("GIF89a\x01\x00\x01\x00"), cfg.handlerUploadThumbnail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, ctype := multipartBody(t, tc.field, tc.claimed, tc.data)
			req := httptest.NewRequest("POST", tc.path+video.ID.String(), body)
			req.Header.Set("Content-Type", ctype)
			req.Header.Set("Authorization", "Bearer "+token)
			assertJSONError(t, serve(tc.pattern, tc.h, req), http.StatusUnsupportedMediaType)
		})
	}
}

func TestThumbnailUploadFormErrors(t *testing.T) {
	cfg := newTestConfig(t)
	owner, token := newTestUser(t, cfg, "owner@example.com")
	video := newTestVideo(t, cfg, owner)
	const pattern = "POST /api/thumbnail_upload/{videoID}"

	tooBig, tooBigType := multipartBody(t, "thumbnail", "image/png",
		append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 11<<20)...))
	wrongField, wrongFieldType := multipartBody(t, "image", "image/png", []byte("\x89PNG\r\n\x1a\n"))

	cases := []struct {
		name        string
		body        io.Reader
		contentType string
		want        int
	}{
		{"over 10 MB", tooBig, tooBigType, http.StatusRequestEntityTooLarge},
		{"missing file field", wrongField, wrongFieldType, http.StatusBadRequest},
		{"not multipart", strings.NewReader(`{"a":1}`), "application/json", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/thumbnail_upload/"+video.ID.String(), tc.body)
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Authorization", "Bearer "+token)
			assertJSONError(t, serve(pattern, cfg.handlerUploadThumbnail, req), tc.want)
		})
	}
}

func TestJSONBodyErrors(t *testing.T) {
	cfg := newTestConfig(t)
	_, token := newTestUser(t, cfg, "owner@example.com")

	endpoints := []struct {
		name, pattern, path string
		h                   http.HandlerFunc
	}{
		{"create user", "POST /api/users", "/api/users", cfg.handlerUsersCreate},
		{"login", "POST /api/login", "/api/login", cfg.handlerLogin},
		{"create video", "POST /api/videos", "/api/videos", cfg.handlerVideoMetaCreate},
	}
	bodies := []struct {
		name string
		body string
		want int
	}{
		{"malformed", `{"email": `, http.StatusBadRequest},
		{"wrong type", `{"email": 42, "title": 42}`, http.StatusBadRequest},
		{"empty", ``, http.StatusBadRequest},
		{"over 1 MB", `{"email": "` + strings.Repeat("a", 1<<20) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, ep := range endpoints {
		for _, b := range bodies {
			t.Run(ep.name+"/"+b.name, func(t *testing.T) {
				req := httptest.NewRequest("POST", ep.path, strings.NewReader(b.body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				assertJSONError(t, serve(ep.pattern, ep.h, req), b.want)
			})
		}
	}
}

func TestCreateVideoValidation(t *testing.T) {
	cfg := newTestConfig(t)
	_, token := newTestUser(t, cfg, "owner@example.com")

	cases := []struct {
		name, title, description string
		want                     int
		wantTitle                string
	}{
		{"ok", "My video", "", http.StatusCreated, "My video"},
		{"trimmed", "  padded  ", "", http.StatusCreated, "padded"},
		{"100 multibyte chars", strings.Repeat("é", 100), "", http.StatusCreated, strings.Repeat("é", 100)},
		{"missing", "", "", http.StatusBadRequest, ""},
		{"only spaces", "   ", "", http.StatusBadRequest, ""},
		{"101 chars", strings.Repeat("a", 101), "", http.StatusBadRequest, ""},
		{"long description", "ok", strings.Repeat("d", 5001), http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"title": tc.title, "description": tc.description})
			req := httptest.NewRequest("POST", "/api/videos", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := serve("POST /api/videos", cfg.handlerVideoMetaCreate, req)
			if tc.want != http.StatusCreated {
				assertJSONError(t, rec, tc.want)
				return
			}
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			var v database.Video
			if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
			if v.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", v.Title, tc.wantTitle)
			}
		})
	}
}

// A file that sniffs as MP4 but isn't playable is the client's problem.
func TestVideoUploadRejectsCorruptMP4(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH")
	}
	cfg := newTestConfig(t)
	owner, token := newTestUser(t, cfg, "owner@example.com")
	video := newTestVideo(t, cfg, owner)

	corrupt := append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), bytes.Repeat([]byte{0xAB}, 4096)...)
	body, ctype := multipartBody(t, "video", "video/mp4", corrupt)
	req := httptest.NewRequest("POST", "/api/video_upload/"+video.ID.String(), body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token)
	assertJSONError(t, serve("POST /api/video_upload/{videoID}", cfg.handlerUploadVideo, req), http.StatusUnprocessableEntity)
}
