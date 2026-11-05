package main

import (
	"errors"
	"mime"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	reqFile, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer reqFile.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	} else if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing media type", err)
		return
	} else if mediaType != "image/png" && mediaType != "image/jpeg" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type uploaded", nil)
		return
	}
	fileExtension := getExtension(mediaType)

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

	thumbnailKey := newObjectKey("thumbnails/", fileExtension)

	// The multipart file is seekable, which S3 signing over plain http needs.
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &thumbnailKey,
		Body:        reqFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload thumbnail", err)
		return
	}
	oldKey := videoMetadata.ThumbnailKey
	videoMetadata.ThumbnailKey = &thumbnailKey

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		cfg.deleteObject(r.Context(), &thumbnailKey) // don't orphan the new upload
		respondWithError(w, http.StatusInternalServerError, "Unable to update video data", err)
		return
	}
	cfg.replaceObject(r.Context(), oldKey, thumbnailKey)

	respondWithJSON(w, http.StatusOK, cfg.withMediaURLs(videoMetadata))

}
