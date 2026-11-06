package api

import (
	"context"
)

// deleteObject removes an object from media storage. It's used for cleanup
// after the database has already changed, so failures are logged (leaving an
// orphaned object) rather than returned. A nil or empty key is a no-op.
//
// The context is detached from the request's cancellation so cleanup still
// runs if the client disconnects right after the response.
func (s *Server) deleteObject(ctx context.Context, key *string) {
	if key == nil || *key == "" {
		return
	}
	if err := s.store.Delete(context.WithoutCancel(ctx), *key); err != nil {
		logFrom(ctx).Warn("couldn't delete object; it is now orphaned", "key", *key, "error", err)
	}
}

// replaceObject deletes the previous object after a successful re-upload.
func (s *Server) replaceObject(ctx context.Context, oldKey *string, newKey string) {
	if oldKey != nil && *oldKey != newKey {
		s.deleteObject(ctx, oldKey)
	}
}
