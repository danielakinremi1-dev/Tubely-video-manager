package main

import (
	"context"
	"log"
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
	godotenv.Load(".env")

	pathToDB := os.Getenv("DB_PATH")
	if pathToDB == "" {
		log.Fatal("DB_PATH must be set")
	}

	db, err := database.NewClient(pathToDB)
	if err != nil {
		log.Fatalf("Couldn't connect to database: %v", err)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is not set")
	}

	platform := os.Getenv("PLATFORM")
	if platform == "" {
		log.Fatal("PLATFORM environment variable is not set")
	}

	filepathRoot := os.Getenv("FILEPATH_ROOT")
	if filepathRoot == "" {
		log.Fatal("FILEPATH_ROOT environment variable is not set")
	}

	s3Bucket := os.Getenv("S3_BUCKET")
	if s3Bucket == "" {
		log.Fatal("S3_BUCKET environment variable is not set")
	}

	s3Region := os.Getenv("S3_REGION")
	if s3Region == "" {
		log.Fatal("S3_REGION environment variable is not set")
	}

	// Public base URL that stored object keys are served from.
	mediaBaseURL, err := resolveMediaBaseURL(os.Getenv("MEDIA_BASE_URL"), os.Getenv("S3_CF_DISTRO"))
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT environment variable is not set")
	}

	// Optional: point at an S3-compatible endpoint (e.g. a local emulator).
	// Leave unset to use real AWS S3.
	s3Endpoint := os.Getenv("S3_ENDPOINT")

	s3ForcePathStyle := false
	if v := os.Getenv("S3_FORCE_PATH_STYLE"); v != "" {
		s3ForcePathStyle, err = strconv.ParseBool(v)
		if err != nil {
			log.Fatalf("S3_FORCE_PATH_STYLE must be a boolean: %v", err)
		}
	}

	store, err := storage.NewS3(context.Background(), storage.S3Config{
		Bucket:         s3Bucket,
		Region:         s3Region,
		Endpoint:       s3Endpoint,
		ForcePathStyle: s3ForcePathStyle,
	})
	if err != nil {
		log.Fatalf("Couldn't setup AWS configuration: %v", err)
	}

	srv := api.NewServer(api.Config{
		DB:           db,
		Store:        store,
		Processor:    media.FFmpeg{},
		JWTSecret:    jwtSecret,
		Platform:     platform,
		FilepathRoot: filepathRoot,
		MediaBaseURL: mediaBaseURL,
	}).HTTPServer(":" + port)

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("Couldn't listen on %s: %v", srv.Addr, err)
	}

	// The first SIGINT (Ctrl-C) or SIGTERM (docker stop) starts a graceful
	// shutdown; stop() restores the default, so a second Ctrl-C exits now.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	log.Printf("Serving on: http://localhost:%s/app/\n", port)
	if err := serve(ctx, srv, ln, shutdownTimeout, abortGrace); err != nil {
		log.Printf("Server stopped with error: %v", err)
	}
	if err := db.Close(); err != nil {
		log.Printf("Couldn't close database: %v", err)
	}
	log.Print("Stopped")
}
