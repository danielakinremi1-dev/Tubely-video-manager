package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/danielakinremi1-dev/tubely/internal/media"
	"github.com/danielakinremi1-dev/tubely/internal/storage"
	"github.com/google/uuid"
)

// newTestServer returns a Server backed by a throwaway SQLite file and an
// in-memory media store.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := database.NewClient(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewServer(Config{
		DB:           db,
		Store:        storage.NewMemory(),
		Processor:    &media.Fake{Info: media.Info{Width: 1280, Height: 720}},
		JWTSecret:    "test-secret",
		MediaBaseURL: "http://media.test",
		// Keep test output quiet; tests that check logs swap in a buffer.
		Logger: slog.New(slog.DiscardHandler),
	})
}

// newTestUser creates a user and returns its ID and a valid access token.
func newTestUser(t *testing.T, srv *Server, email string) (uuid.UUID, string) {
	t.Helper()
	u, err := srv.db.CreateUser(database.CreateUserParams{Email: email, Password: "hash"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := auth.MakeJWT(u.ID, srv.jwtSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	return u.ID, token
}

func newTestVideo(t *testing.T, srv *Server, owner uuid.UUID) database.Video {
	t.Helper()
	v, err := srv.db.CreateVideo(database.CreateVideoParams{Title: "t", UserID: owner})
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

// serve sends one request through the server's real router, including its
// middleware (requireAuth), and records the response.
func serve(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
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
	srv := newTestServer(t)
	owner, _ := newTestUser(t, srv, "owner@example.com")
	_, otherToken := newTestUser(t, srv, "other@example.com")
	video := newTestVideo(t, srv, owner)

	cases := []struct {
		name, path, field, ctype string
	}{
		{"video", "/api/video_upload/", "video", "video/mp4"},
		{"thumbnail", "/api/thumbnail_upload/", "thumbnail", "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, ctype := multipartBody(t, tc.field, tc.ctype, []byte("data"))
			req := httptest.NewRequest("POST", tc.path+video.ID.String(), body)
			req.Header.Set("Content-Type", ctype)
			req.Header.Set("Authorization", "Bearer "+otherToken)
			assertJSONError(t, serve(srv, req), http.StatusForbidden)
		})
	}
}

func TestUploadRejectsSpoofedContentType(t *testing.T) {
	srv := newTestServer(t)
	owner, token := newTestUser(t, srv, "owner@example.com")
	video := newTestVideo(t, srv, owner)

	cases := []struct {
		name, path, field, claimed string
		data                       []byte
	}{
		{"pdf claiming mp4", "/api/video_upload/", "video", "video/mp4",
			[]byte("%PDF-1.4\n%fake")},
		{"gif claiming png", "/api/thumbnail_upload/", "thumbnail", "image/png",
			[]byte("GIF89a\x01\x00\x01\x00")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, ctype := multipartBody(t, tc.field, tc.claimed, tc.data)
			req := httptest.NewRequest("POST", tc.path+video.ID.String(), body)
			req.Header.Set("Content-Type", ctype)
			req.Header.Set("Authorization", "Bearer "+token)
			assertJSONError(t, serve(srv, req), http.StatusUnsupportedMediaType)
		})
	}
}

func TestThumbnailUploadFormErrors(t *testing.T) {
	srv := newTestServer(t)
	owner, token := newTestUser(t, srv, "owner@example.com")
	video := newTestVideo(t, srv, owner)

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
			assertJSONError(t, serve(srv, req), tc.want)
		})
	}
}

func TestJSONBodyErrors(t *testing.T) {
	srv := newTestServer(t)
	_, token := newTestUser(t, srv, "owner@example.com")

	endpoints := []struct {
		name, path string
	}{
		{"create user", "/api/users"},
		{"login", "/api/login"},
		{"create video", "/api/videos"},
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
				assertJSONError(t, serve(srv, req), b.want)
			})
		}
	}
}

func TestCreateVideoValidation(t *testing.T) {
	srv := newTestServer(t)
	_, token := newTestUser(t, srv, "owner@example.com")

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
			rec := serve(srv, req)
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
func TestVideoUploadRejectsUnplayableVideo(t *testing.T) {
	srv := newTestServer(t)
	srv.processor = &media.Fake{ProbeErr: fmt.Errorf("%w: moov atom not found", media.ErrInvalidMedia)}
	owner, token := newTestUser(t, srv, "owner@example.com")
	video := newTestVideo(t, srv, owner)

	mp4 := []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
	body, ctype := multipartBody(t, "video", "video/mp4", mp4)
	req := httptest.NewRequest("POST", "/api/video_upload/"+video.ID.String(), body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+token)
	assertJSONError(t, serve(srv, req), http.StatusUnprocessableEntity)
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	srv := newTestServer(t)
	id := uuid.NewString()
	routes := []struct{ method, path string }{
		{"POST", "/api/videos"},
		{"GET", "/api/videos"},
		{"GET", "/api/videos/" + id},
		{"DELETE", "/api/videos/" + id},
		{"POST", "/api/video_upload/" + id},
		{"POST", "/api/thumbnail_upload/" + id},
	}
	for _, rt := range routes {
		for _, header := range []string{"", "Bearer not-a-jwt"} {
			t.Run(rt.method+" "+rt.path+" "+header, func(t *testing.T) {
				req := httptest.NewRequest(rt.method, rt.path, nil)
				if header != "" {
					req.Header.Set("Authorization", header)
				}
				assertJSONError(t, serve(srv, req), http.StatusUnauthorized)
			})
		}
	}
}

// Videos are private: only the owner can read or delete one.
func TestVideoAccessControl(t *testing.T) {
	srv := newTestServer(t)
	owner, ownerToken := newTestUser(t, srv, "owner@example.com")
	_, otherToken := newTestUser(t, srv, "other@example.com")

	cases := []struct {
		name, token, id string
		want            int
	}{
		{"owner", ownerToken, "", http.StatusOK},
		{"other user", otherToken, "", http.StatusForbidden},
		{"unknown video", ownerToken, uuid.NewString(), http.StatusNotFound},
		{"malformed id", ownerToken, "not-a-uuid", http.StatusBadRequest},
	}
	for _, method := range []string{"GET", "DELETE"} {
		for _, tc := range cases {
			t.Run(method+" "+tc.name, func(t *testing.T) {
				id := tc.id
				if id == "" {
					id = newTestVideo(t, srv, owner).ID.String()
				}
				req := httptest.NewRequest(method, "/api/videos/"+id, nil)
				req.Header.Set("Authorization", "Bearer "+tc.token)
				rec := serve(srv, req)

				want := tc.want
				if method == "DELETE" && want == http.StatusOK {
					want = http.StatusNoContent
				}
				if want >= 400 {
					assertJSONError(t, rec, want)
				} else if rec.Code != want {
					t.Fatalf("status = %d, want %d (body %s)", rec.Code, want, rec.Body)
				}
			})
		}
	}
}
