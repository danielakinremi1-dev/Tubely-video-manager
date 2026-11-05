package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/danielakinremi1-dev/tubely/internal/database"

	"github.com/joho/godotenv"
)

type apiConfig struct {
	db           database.Client
	jwtSecret    string
	platform     string
	filepathRoot string
	s3Bucket     string
	s3Region     string
	mediaBaseURL string
	port         string
	s3Client     *s3.Client
}

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

	s3Client, err := newS3Client(context.Background(), s3Region, s3Endpoint, s3ForcePathStyle)
	if err != nil {
		log.Fatalf("Couldn't setup AWS configuration: %v", err)
	}

	cfg := apiConfig{
		db:           db,
		jwtSecret:    jwtSecret,
		platform:     platform,
		filepathRoot: filepathRoot,
		s3Bucket:     s3Bucket,
		s3Region:     s3Region,
		mediaBaseURL: mediaBaseURL,
		s3Client:     s3Client,
		port:         port,
	}

	mux := http.NewServeMux()
	appHandler := http.StripPrefix("/app", http.FileServer(http.Dir(filepathRoot)))
	mux.Handle("/app/", appHandler)

	mux.HandleFunc("POST /api/login", cfg.handlerLogin)
	mux.HandleFunc("POST /api/refresh", cfg.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", cfg.handlerRevoke)

	mux.HandleFunc("POST /api/users", cfg.handlerUsersCreate)

	mux.HandleFunc("POST /api/videos", cfg.handlerVideoMetaCreate)
	mux.HandleFunc("POST /api/thumbnail_upload/{videoID}", cfg.handlerUploadThumbnail)
	mux.HandleFunc("POST /api/video_upload/{videoID}", cfg.handlerUploadVideo)
	mux.HandleFunc("GET /api/videos", cfg.handlerVideosRetrieve)
	mux.HandleFunc("GET /api/videos/{videoID}", cfg.handlerVideoGet)
	mux.HandleFunc("DELETE /api/videos/{videoID}", cfg.handlerVideoMetaDelete)

	mux.HandleFunc("POST /admin/reset", cfg.handlerReset)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("Serving on: http://localhost:%s/app/\n", port)
	log.Fatal(srv.ListenAndServe())
}
