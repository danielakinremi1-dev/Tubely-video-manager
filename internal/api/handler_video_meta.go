package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/google/uuid"
)

const (
	maxTitleLength       = 100
	maxDescriptionLength = 5000
)

// validateVideoParams trims the title and enforces length limits. Lengths
// count characters (runes), not bytes, so "café" is 4 long, not 5.
func validateVideoParams(p *database.CreateVideoParams) error {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return errors.New("title is required")
	}
	if n := utf8.RuneCountInString(p.Title); n > maxTitleLength {
		return fmt.Errorf("title is %d characters; the limit is %d", n, maxTitleLength)
	}
	if n := utf8.RuneCountInString(p.Description); n > maxDescriptionLength {
		return fmt.Errorf("description is %d characters; the limit is %d", n, maxDescriptionLength)
	}
	return nil
}

func (s *Server) handlerVideoMetaCreate(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		database.CreateVideoParams
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, s.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	params := parameters{}
	if !decodeJSON(w, r, &params) {
		return
	}
	if err := validateVideoParams(&params.CreateVideoParams); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video: "+err.Error(), err)
		return
	}
	params.UserID = userID

	video, err := s.db.CreateVideo(params.CreateVideoParams)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create video", err)
		return
	}

	respondWithJSON(w, http.StatusCreated, s.withMediaURLs(video))
}

func (s *Server) handlerVideoMetaDelete(w http.ResponseWriter, r *http.Request) {
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
	userID, err := auth.ValidateJWT(token, s.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	video, err := s.db.GetVideo(videoID)
	if errors.Is(err, database.ErrNotFound) {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusForbidden, "You can't delete this video", err)
		return
	}

	err = s.db.DeleteVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete video", err)
		return
	}

	// Remove the row first, then its objects: if storage cleanup fails we
	// leave a logged orphan object, never a row pointing at a missing file.
	s.deleteObject(r.Context(), video.VideoKey)
	s.deleteObject(r.Context(), video.ThumbnailKey)

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlerVideoGet(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
		return
	}

	videoMetadata, err := s.db.GetVideo(videoID)
	if errors.Is(err, database.ErrNotFound) {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, s.withMediaURLs(videoMetadata))
}

func (s *Server) handlerVideosRetrieve(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, s.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	videos, err := s.db.GetVideos(userID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't retrieve videos", err)
		return
	}

	for i := range videos {
		videos[i] = s.withMediaURLs(videos[i])
	}
	respondWithJSON(w, http.StatusOK, videos)
}
