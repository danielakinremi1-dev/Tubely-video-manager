package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/danielakinremi1-dev/tubely/internal/api"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/danielakinremi1-dev/tubely/internal/media"
	"github.com/danielakinremi1-dev/tubely/internal/storage"

	"github.com/joho/godotenv"
)

func main() {
	// .env is optional (containers set real env vars), but a malformed one
	// is an error worth stopping for. Variables already set win.
	envErr := godotenv.Load(".env")

	// Human-readable logs in development; JSON (one object per line, easy to
	// search and ship to a log service) everywhere else.
	var logHandler slog.Handler = slog.NewJSONHandler(os.Stderr, nil)
	if os.Getenv("PLATFORM") == "dev" {
		logHandler = slog.NewTextHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(logHandler))
	if envErr != nil && !errors.Is(envErr, fs.ErrNotExist) {
		fatal("couldn't read .env", "error", envErr)
	}

	processor := media.FFmpeg{}
	if err := processor.CheckInstalled(); err != nil {
		fatal(err.Error())
	}

	pathToDB := os.Getenv("DB_PATH")
	if pathToDB == "" {
		fatal("DB_PATH must be set")
	}

	db, err := database.NewClient(pathToDB)
	if err != nil {
		fatal("couldn't open database", "error", err)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		fatal("JWT_SECRET environment variable is not set")
	}

	platform := os.Getenv("PLATFORM")
	if platform == "" {
		fatal("PLATFORM environment variable is not set")
	}

	filepathRoot := os.Getenv("FILEPATH_ROOT")
	if filepathRoot == "" {
		fatal("FILEPATH_ROOT environment variable is not set")
	}

	s3Bucket := os.Getenv("S3_BUCKET")
	if s3Bucket == "" {
		fatal("S3_BUCKET environment variable is not set")
	}

	s3Region := os.Getenv("S3_REGION")
	if s3Region == "" {
		fatal("S3_REGION environment variable is not set")
	}

	// Public base URL that stored object keys are served from.
	mediaBaseURL, err := resolveMediaBaseURL(os.Getenv("MEDIA_BASE_URL"), os.Getenv("S3_CF_DISTRO"))
	if err != nil {
		fatal(err.Error())
	}

	port := os.Getenv("PORT")
	if port == "" {
		fatal("PORT environment variable is not set")
	}

	// Optional: point at an S3-compatible endpoint (e.g. a local emulator).
	// Leave unset to use real AWS S3.
	s3Endpoint := os.Getenv("S3_ENDPOINT")

	s3ForcePathStyle := false
	if v := os.Getenv("S3_FORCE_PATH_STYLE"); v != "" {
		s3ForcePathStyle, err = strconv.ParseBool(v)
		if err != nil {
			fatal("S3_FORCE_PATH_STYLE must be a boolean", "error", err)
		}
	}

	store, err := storage.NewS3(context.Background(), storage.S3Config{
		Bucket:         s3Bucket,
		Region:         s3Region,
		Endpoint:       s3Endpoint,
		ForcePathStyle: s3ForcePathStyle,
	})
	if err != nil {
		fatal("couldn't set up S3 storage", "error", err)
	}

	srv := api.NewServer(api.Config{
		DB:           db,
		Store:        store,
		Processor:    processor,
		JWTSecret:    jwtSecret,
		Platform:     platform,
		FilepathRoot: filepathRoot,
		MediaBaseURL: mediaBaseURL,
	}).HTTPServer(":" + port)

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		fatal("couldn't listen", "addr", srv.Addr, "error", err)
	}

	// The first SIGINT (Ctrl-C) or SIGTERM (docker stop) starts a graceful
	// shutdown; stop() restores the default, so a second Ctrl-C exits now.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	slog.Info("serving", "url", "http://localhost:"+port+"/app/")
	if err := serve(ctx, srv, ln, shutdownTimeout, abortGrace); err != nil {
		slog.Error("server stopped with an error", "error", err)
	}
	if err := db.Close(); err != nil {
		slog.Error("couldn't close database", "error", err)
	}
	slog.Info("stopped")
}

// fatal logs a startup error and exits.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
