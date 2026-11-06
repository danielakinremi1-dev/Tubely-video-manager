package api

import (
	"strings"

	"github.com/danielakinremi1-dev/tubely/internal/database"
)

// mediaURL returns the public URL for an object key.
func (s *Server) mediaURL(key string) string {
	return s.mediaBaseURL + "/" + strings.TrimLeft(key, "/")
}

// withMediaURLs fills in the public URLs for a video from its stored object
// keys. Rows without a key keep their legacy stored URL.
func (s *Server) withMediaURLs(v database.Video) database.Video {
	if v.VideoKey != nil && *v.VideoKey != "" {
		u := s.mediaURL(*v.VideoKey)
		v.VideoURL = &u
	}
	if v.ThumbnailKey != nil && *v.ThumbnailKey != "" {
		u := s.mediaURL(*v.ThumbnailKey)
		v.ThumbnailURL = &u
	}
	return v
}
