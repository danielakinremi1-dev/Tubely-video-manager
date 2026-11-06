// Package api is Tubely's HTTP layer: routes, handlers and request/response
// helpers. It depends on interfaces for media storage and processing, so it
// never talks to AWS or runs ffmpeg itself.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/danielakinremi1-dev/tubely/internal/media"
	"github.com/danielakinremi1-dev/tubely/internal/storage"
)

// Config is everything a Server needs. cmd/tubely builds it from the
// environment; tests build it directly.
type Config struct {
	DB        database.Client
	Store     storage.Store
	Processor media.Processor
	JWTSecret string
	// Platform "dev" enables POST /admin/reset.
	Platform string
	// FilepathRoot is the directory the web UI is served from.
	FilepathRoot string
	// MediaBaseURL is the public URL that object keys are served under.
	MediaBaseURL string
	// Logger receives request logs. It defaults to slog.Default().
	Logger *slog.Logger
}

// Server-wide timeouts bound how long a slow or stalled client can hold a
// connection open (a "slowloris" attack sends a request a byte at a time).
// The upload routes extend their own deadlines once the caller is known.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = time.Minute
	writeTimeout      = 2 * time.Minute
	idleTimeout       = 2 * time.Minute

	// A 1 GB video at 5 Mbit/s takes about 27 minutes to arrive, then needs
	// probing, remuxing and the S3 upload.
	videoUploadTimeout     = 45 * time.Minute
	thumbnailUploadTimeout = 5 * time.Minute
)

// Server holds the dependencies the handlers use.
type Server struct {
	db           database.Client
	store        storage.Store
	processor    media.Processor
	jwtSecret    string
	platform     string
	filepathRoot string
	mediaBaseURL string
	logger       *slog.Logger
}

func NewServer(c Config) *Server {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		db:           c.DB,
		store:        c.Store,
		processor:    c.Processor,
		jwtSecret:    c.JWTSecret,
		platform:     c.Platform,
		filepathRoot: c.FilepathRoot,
		mediaBaseURL: c.MediaBaseURL,
		logger:       logger,
	}
}

// HTTPServer returns an http.Server for addr with the server-wide timeouts.
func (s *Server) HTTPServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Handler returns the router with every route registered, wrapped in the
// request ID, access log and panic recovery middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/app/", http.StripPrefix("/app", http.FileServer(http.Dir(s.filepathRoot))))

	mux.HandleFunc("GET /api/healthz", s.handlerHealthz)

	mux.HandleFunc("POST /api/login", s.handlerLogin)
	mux.HandleFunc("POST /api/refresh", s.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", s.handlerRevoke)

	mux.HandleFunc("POST /api/users", s.handlerUsersCreate)

	mux.HandleFunc("POST /api/videos", s.requireAuth(s.handlerVideoMetaCreate))
	mux.HandleFunc("POST /api/thumbnail_upload/{videoID}", s.requireAuth(s.handlerUploadThumbnail))
	mux.HandleFunc("POST /api/video_upload/{videoID}", s.requireAuth(s.handlerUploadVideo))
	mux.HandleFunc("GET /api/videos", s.requireAuth(s.handlerVideosRetrieve))
	mux.HandleFunc("GET /api/videos/{videoID}", s.requireAuth(s.handlerVideoGet))
	mux.HandleFunc("DELETE /api/videos/{videoID}", s.requireAuth(s.handlerVideoMetaDelete))

	mux.HandleFunc("POST /admin/reset", s.handlerReset)
	return s.withMiddleware(mux)
}
