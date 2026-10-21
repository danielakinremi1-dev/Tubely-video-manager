package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
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
	if err != nil {
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

	tmpProcessed, err := processVideoForFastStart(tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process video for fast start encoding", err)
		return
	}

	tmpProcessedFile, err := os.Open(tmpProcessed)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open processed video", err)
		return
	}
	tmpProcessedFile.Seek(0, io.SeekStart)

	ext := getExtension(mediaType)
	key := make([]byte, 32)
	rand.Read(key)
	randomBase := base64.RawURLEncoding.EncodeToString(key)

	prefix := "other"
	aspectRatio, err := getVideoAspectRatio(tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse video data: Aspect ratio", err)
		return
	}

	if aspectRatio == "16:9" {
		prefix = "landscape/"
	} else if aspectRatio == "9:16" {
		prefix = "portrait/"
	}

	fileKey := fmt.Sprintf("%v%v%v", prefix, randomBase, ext)

	_, err = cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        tmpProcessedFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload video data to server", err)
		return
	}

	newURL := fmt.Sprintf("%v,%v", cfg.s3Bucket, fileKey)
	videoMetadata.VideoURL = &newURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update database video data", err)
		return
	}

	newVideo, err := cfg.dbVideoToSignedVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate presigned Video URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, newVideo)
}
