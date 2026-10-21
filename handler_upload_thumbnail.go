package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

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
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to retrieve video metadata", err)
		return
	} else if videoMetadata.UserID != userID {
		respondWithJSON(w, http.StatusUnauthorized, struct{}{})
		return
	}

	key := make([]byte, 32)
	rand.Read(key)
	randomBase := base64.RawURLEncoding.EncodeToString(key)

	assetPath := randomBase + fileExtension
	baseURL := filepath.Join(cfg.assetsRoot, assetPath)
	newFile, err := os.Create(baseURL)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create video file", err)
		return
	}

	_, err = io.Copy(newFile, reqFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy request data to video file", err)
		return
	}

	thumbnailURL := fmt.Sprintf("http://localhost:%v/assets/%v", cfg.port, assetPath)
	videoMetadata.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video data", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)

}
