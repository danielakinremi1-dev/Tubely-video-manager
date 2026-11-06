package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/danielakinremi1-dev/tubely/internal/database"
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

	params := parameters{}
	if !decodeJSON(w, r, &params) {
		return
	}
	if err := validateVideoParams(&params.CreateVideoParams); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video: "+err.Error(), err)
		return
	}
	params.UserID = userIDFrom(r.Context())

	video, err := s.db.CreateVideo(params.CreateVideoParams)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create video", err)
		return
	}

	respondWithJSON(w, http.StatusCreated, s.withMediaURLs(video))
}

func (s *Server) handlerVideoMetaDelete(w http.ResponseWriter, r *http.Request) {
	video, ok := s.loadOwnedVideo(w, r)
	if !ok {
		return
	}

	err := s.db.DeleteVideo(video.ID)
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
	video, ok := s.loadOwnedVideo(w, r)
	if !ok {
		return
	}
	respondWithJSON(w, http.StatusOK, s.withMediaURLs(video))
}

func (s *Server) handlerVideosRetrieve(w http.ResponseWriter, r *http.Request) {
	videos, err := s.db.GetVideos(userIDFrom(r.Context()))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't retrieve videos", err)
		return
	}

	for i := range videos {
		videos[i] = s.withMediaURLs(videos[i])
	}
	respondWithJSON(w, http.StatusOK, videos)
}
