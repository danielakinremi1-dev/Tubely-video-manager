package main

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if errors.Is(err, database.ErrNotFound) {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to retrieve video metadata", err)
		return
	} else if videoMetadata.UserID != userID {
		respondWithJSON(w, http.StatusUnauthorized, struct{}{})
		return
	}

	f, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer f.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	} else if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing media type", err)
		return
	} else if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type uploaded", nil)
		return
	}

	tmp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp video upload file", err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	_, err = io.Copy(tmp, f)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy request data to temp file", err)
		return
	}
	tmp.Seek(0, io.SeekStart)

	tmpProcessed, err := processVideoForFastStart(r.Context(), tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process video for fast start encoding", err)
		return
	}
	defer os.Remove(tmpProcessed)

	tmpProcessedFile, err := os.Open(tmpProcessed)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open processed video", err)
		return
	}
	defer tmpProcessedFile.Close()

	prefix := "other/"
	aspectRatio, err := getVideoAspectRatio(r.Context(), tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse video data: Aspect ratio", err)
		return
	}

	if aspectRatio == "16:9" {
		prefix = "landscape/"
	} else if aspectRatio == "9:16" {
		prefix = "portrait/"
	}

	fileKey := newObjectKey(prefix, getExtension(mediaType))

	// A cancelled request (client gone) aborts the upload; S3 never stores a
	// partial PutObject, so nothing is orphaned.
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        tmpProcessedFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload video data to server", err)
		return
	}

	oldKey := videoMetadata.VideoKey
	videoMetadata.VideoKey = &fileKey

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		cfg.deleteObject(r.Context(), &fileKey) // don't orphan the new upload
		respondWithError(w, http.StatusInternalServerError, "Unable to update database video data", err)
		return
	}
	cfg.replaceObject(r.Context(), oldKey, fileKey)

	respondWithJSON(w, http.StatusOK, cfg.withMediaURLs(videoMetadata))
}
