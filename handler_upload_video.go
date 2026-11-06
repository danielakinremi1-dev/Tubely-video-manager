package main

import (
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/danielakinremi1-dev/tubely/internal/media"
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
		respondWithError(w, http.StatusForbidden, "You can't upload to this video", nil)
		return
	}

	f, _, err := r.FormFile("video")
	if err != nil {
		status, msg := uploadFormError(err, "video", uploadLimit)
		respondWithError(w, status, msg, err)
		return
	}
	defer f.Close()

	mediaType, err := sniffMediaType(f)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to read uploaded file", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Video must be an MP4 file (got "+mediaType+")", nil)
		return
	}

	tmp, err := os.CreateTemp("", "tubely-upload-*.mp4")
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

	// Probe first: it's cheap, and it rejects files that aren't playable
	// video before the remux spends time on them.
	info, err := cfg.processor.Probe(r.Context(), tmp.Name())
	if errors.Is(err, media.ErrInvalidMedia) {
		respondWithError(w, http.StatusUnprocessableEntity, "File is not a playable video", err)
		return
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to read video metadata", err)
		return
	}

	tmpProcessed, err := cfg.processor.FastStart(r.Context(), tmp.Name())
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

	fileKey := newObjectKey(keyPrefix(info.AspectRatio()), getExtension(mediaType))

	// A cancelled request (client gone) aborts the upload, and neither a
	// failed PutObject nor an aborted multipart upload leaves an object.
	err = cfg.store.Put(r.Context(), fileKey, mediaType, tmpProcessedFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload video data to server", err)
		return
	}

	oldKey := videoMetadata.VideoKey
	videoMetadata.VideoKey = &fileKey

	updated, err := cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		cfg.deleteObject(r.Context(), &fileKey) // don't orphan the new upload
		if errors.Is(err, database.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "Video was deleted during upload", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Unable to update database video data", err)
		return
	}
	cfg.replaceObject(r.Context(), oldKey, fileKey)

	respondWithJSON(w, http.StatusOK, cfg.withMediaURLs(updated))
}

// keyPrefix files videos under their shape: landscape/, portrait/ or other/.
func keyPrefix(aspectRatio string) string {
	switch aspectRatio {
	case media.Landscape:
		return "landscape/"
	case media.Portrait:
		return "portrait/"
	default:
		return "other/"
	}
}
