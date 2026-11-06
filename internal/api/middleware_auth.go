package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielakinremi1-dev/tubely/internal/auth"
	"github.com/danielakinremi1-dev/tubely/internal/database"
	"github.com/google/uuid"
)

type contextKey int

const userIDKey contextKey = iota

// requireAuth rejects requests without a valid access token (401) and hands
// the token's user ID to next through the request context.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		next(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
	}
}

// userIDFrom returns the authenticated user's ID. It panics if the route
// isn't wrapped in requireAuth, which is a programming error.
func userIDFrom(ctx context.Context) uuid.UUID {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	if !ok {
		panic("userIDFrom: route is not wrapped in requireAuth")
	}
	return id
}

// loadOwnedVideo loads the {videoID} video and checks the authenticated user
// owns it. On failure it writes the response (400, 404, 403 or 500) and
// returns false; the handler just returns.
func (s *Server) loadOwnedVideo(w http.ResponseWriter, r *http.Request) (database.Video, bool) {
	videoID, err := uuid.Parse(r.PathValue("videoID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
		return database.Video{}, false
	}
	video, err := s.db.GetVideo(videoID)
	if errors.Is(err, database.ErrNotFound) {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return database.Video{}, false
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return database.Video{}, false
	}
	if video.UserID != userIDFrom(r.Context()) {
		respondWithError(w, http.StatusForbidden, "You don't have access to this video", nil)
		return database.Video{}, false
	}
	return video, true
}
