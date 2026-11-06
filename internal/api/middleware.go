package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

type loggerKey struct{}

// logFrom returns the request-scoped logger (tagged with the request ID), or
// the default logger outside a request.
func logFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// withMiddleware wraps h in the cross-cutting layers, outermost first:
// request ID, access log, panic recovery.
func (s *Server) withMiddleware(h http.Handler) http.Handler {
	return s.requestID(accessLog(recoverPanics(h)))
}

// A client-supplied request ID is kept (so a proxy's ID ties logs together)
// only if it's short and plain; anything else could be used to forge log
// lines.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestID gives every request an ID, returns it in the X-Request-ID
// response header, and puts a logger tagged with it in the context.
func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), loggerKey{}, s.logger.With("request_id", id))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// responseRecorder captures the status, size and any error a handler
// reported, for the access log. Unwrap keeps http.ResponseController (used
// to extend upload deadlines) working through it.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
	err    error
}

func (rr *responseRecorder) WriteHeader(code int) {
	if rr.status == 0 {
		rr.status = code
	}
	rr.ResponseWriter.WriteHeader(code)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	if rr.status == 0 {
		rr.status = http.StatusOK
	}
	n, err := rr.ResponseWriter.Write(b)
	rr.bytes += n
	return n, err
}

func (rr *responseRecorder) Unwrap() http.ResponseWriter { return rr.ResponseWriter }

// noteError records the error behind an error response, so the access log
// line carries it. respondWithError calls it.
func (rr *responseRecorder) noteError(err error) { rr.err = err }

// accessLog writes one line per request: method, path, status, size,
// duration and, for failures, the underlying error. 5xx responses are
// logged at ERROR, everything else at INFO.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rr := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rr, r)

		status := rr.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", rr.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if rr.err != nil {
			attrs = append(attrs, "error", rr.err.Error())
		}
		logFrom(r.Context()).Log(r.Context(), level, "request", attrs...)
	})
}

// recoverPanics turns a panic in a handler into a 500 response and an
// ERROR log with the stack trace, instead of a dropped connection.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(v) // net/http's deliberate "abort this response"
			}
			logFrom(r.Context()).Error("panic", "panic", v, "stack", string(debug.Stack()))
			if rr, ok := w.(*responseRecorder); !ok || rr.status == 0 {
				respondWithError(w, http.StatusInternalServerError, "Internal server error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
