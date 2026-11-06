package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// newObjectKey returns a unique, URL-safe object key such as
// "thumbnails/0b7c3c1e-5f0a-4d3e-9c47-2a8f1e6d9b10.png". UUIDv4 carries 122
// random bits from crypto/rand, so keys are unguessable and never collide.
func newObjectKey(prefix, ext string) string {
	return prefix + uuid.NewString() + ext
}

// removeFormFiles deletes the temp files a parsed multipart form spilled to
// disk. net/http does this only for the *http.Request it created, and our
// middleware hands handlers a copy (r.WithContext), so any handler that
// parses a multipart form must defer this itself.
func removeFormFiles(r *http.Request) {
	if r.MultipartForm != nil {
		r.MultipartForm.RemoveAll()
	}
}

// extendDeadlines lets one request run past the server-wide read and write
// timeouts, for uploads that legitimately take minutes. Call it after the
// caller is authenticated, so strangers don't get the longer limit.
func extendDeadlines(w http.ResponseWriter, d time.Duration) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(d)
	// These fail only for writers that aren't a real connection (such as
	// httptest.ResponseRecorder), where there's no deadline to extend.
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
}

// uploadFormError maps an error from parsing a multipart upload to a status
// and message: 413 when the body hit its http.MaxBytesReader cap, 400 for a
// missing file field or a malformed form.
func uploadFormError(err error, field string, limit int64) (int, string) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return http.StatusRequestEntityTooLarge, fmt.Sprintf("Upload is larger than %d MB", limit>>20)
	}
	if errors.Is(err, http.ErrMissingFile) {
		return http.StatusBadRequest, fmt.Sprintf("Upload form has no %q file", field)
	}
	return http.StatusBadRequest, "Unable to parse upload form"
}

// sniffMediaType returns a file's type as detected from its first 512 bytes
// (the WHATWG MIME-sniffing algorithm behind http.DetectContentType) and
// rewinds the file. Validation then depends on the bytes, not on the
// client-supplied Content-Type, which anyone can set to anything.
func sniffMediaType(f io.ReadSeeker) (string, error) {
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read file header: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind file: %w", err)
	}
	// Drop parameters such as "; charset=utf-8".
	mediaType, _, err := mime.ParseMediaType(http.DetectContentType(buf[:n]))
	if err != nil {
		return "", fmt.Errorf("parse detected type: %w", err)
	}
	return mediaType, nil
}

func getExtension(mediaType string) string {
	extensions := strings.Split(mediaType, "/")
	if len(extensions) != 2 {
		return ".bin"
	}
	return "." + extensions[1]
}
