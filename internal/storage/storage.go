// Package storage holds media objects (videos and thumbnails) by key, such
// as "portrait/<uuid>.mp4". Handlers depend on the Store interface, so the
// backend is a configuration choice (S3, an S3-compatible emulator, or the
// in-memory store in tests) rather than something baked into the code.
package storage

import (
	"context"
	"io"
)

// Store is an object store for media.
type Store interface {
	// Put uploads body under key. A failed or cancelled Put leaves no
	// object behind.
	Put(ctx context.Context, key, contentType string, body io.Reader) error
	// Delete removes the object at key. Deleting a missing key succeeds.
	Delete(ctx context.Context, key string) error
}
