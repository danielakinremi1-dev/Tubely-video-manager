package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newLoggedServer returns a test server whose logs go to the returned
// buffer as JSON lines.
func newLoggedServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	srv := newTestServer(t)
	var buf bytes.Buffer
	srv.logger = slog.New(slog.NewJSONHandler(&buf, nil))
	return srv, &buf
}

// logLines decodes the JSON log lines in buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q isn't JSON: %v", line, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func TestRequestID(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct {
		name, incoming string
		keep           bool
	}{
		{"generated", "", false},
		{"proxy ID kept", "edge-1234.abc_DEF", true},
		{"unsafe ID replaced", "bad id\nlevel=ERROR", false},
		{"too long replaced", strings.Repeat("a", 65), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/videos", nil)
			if tc.incoming != "" {
				req.Header.Set(requestIDHeader, tc.incoming)
			}
			got := serve(srv, req).Header().Get(requestIDHeader)
			if tc.keep && got != tc.incoming {
				t.Errorf("request ID = %q, want %q kept", got, tc.incoming)
			}
			if !tc.keep && (got == "" || got == tc.incoming) {
				t.Errorf("request ID = %q, want a fresh one", got)
			}
		})
	}
}

func TestAccessLog(t *testing.T) {
	srv, buf := newLoggedServer(t)
	req := httptest.NewRequest("GET", "/api/videos", nil) // no token: 401
	rec := serve(srv, req)

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %s", len(lines), buf)
	}
	l := lines[0]
	checks := map[string]any{
		"msg":        "request",
		"level":      "INFO",
		"method":     "GET",
		"path":       "/api/videos",
		"status":     float64(401),
		"request_id": rec.Header().Get(requestIDHeader),
	}
	for k, want := range checks {
		if l[k] != want {
			t.Errorf("log %s = %v, want %v", k, l[k], want)
		}
	}
	if e, _ := l["error"].(string); !strings.Contains(e, "no auth header") {
		t.Errorf("log error = %q, want the underlying cause", l["error"])
	}
}

func TestRecoverPanics(t *testing.T) {
	srv, buf := newLoggedServer(t)
	h := srv.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]int
		m["boom"]++ // nil map write: panics
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	assertJSONError(t, rec, http.StatusInternalServerError)

	var sawPanic, sawRequest bool
	for _, l := range logLines(t, buf) {
		switch l["msg"] {
		case "panic":
			sawPanic = strings.Contains(l["stack"].(string), "middleware_test.go")
		case "request":
			sawRequest = l["status"] == float64(500) && l["level"] == "ERROR"
		}
	}
	if !sawPanic {
		t.Errorf("no panic log with a stack trace: %s", buf)
	}
	if !sawRequest {
		t.Errorf("no ERROR access log with status 500: %s", buf)
	}

	// The server keeps serving after a panic.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("second request: status %d", rec.Code)
	}
}
