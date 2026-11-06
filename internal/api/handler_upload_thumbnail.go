package api

import (
	"errors"
	"net/http"

	"github.com/danielakinremi1-dev/tubely/internal/database"
)

func (s *Server) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	const maxThumbnailBytes = 10 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxThumbnailBytes)

	videoMetadata, ok := s.loadOwnedVideo(w, r)
	if !ok {
		return
	}
	extendDeadlines(w, thumbnailUploadTimeout)

	// The whole (capped) body fits in memory, so nothing spills to disk.
	if err := r.ParseMultipartForm(maxThumbnailBytes); err != nil {
		status, msg := uploadFormError(err, "thumbnail", maxThumbnailBytes)
		respondWithError(w, status, msg, err)
		return
	}

	reqFile, _, err := r.FormFile("thumbnail")
	if err != nil {
		status, msg := uploadFormError(err, "thumbnail", maxThumbnailBytes)
		respondWithError(w, status, msg, err)
		return
	}
	defer reqFile.Close()

	mediaType, err := sniffMediaType(reqFile)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to read uploaded file", err)
		return
	}
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Thumbnail must be a PNG or JPEG image (got "+mediaType+")", nil)
		return
	}
	fileExtension := getExtension(mediaType)

	thumbnailKey := newObjectKey("thumbnails/", fileExtension)

	err = s.store.Put(r.Context(), thumbnailKey, mediaType, reqFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload thumbnail", err)
		return
	}
	oldKey := videoMetadata.ThumbnailKey
	videoMetadata.ThumbnailKey = &thumbnailKey

	updated, err := s.db.UpdateVideo(videoMetadata)
	if err != nil {
		s.deleteObject(r.Context(), &thumbnailKey) // don't orphan the new upload
		if errors.Is(err, database.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "Video was deleted during upload", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Unable to update video data", err)
		return
	}
	s.replaceObject(r.Context(), oldKey, thumbnailKey)

	respondWithJSON(w, http.StatusOK, s.withMediaURLs(updated))

}
