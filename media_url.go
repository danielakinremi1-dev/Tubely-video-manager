package main

import (
	"errors"
	"strings"

	"github.com/danielakinremi1-dev/tubely/internal/database"
)

// resolveMediaBaseURL picks the public base URL that object keys are served
// from. MEDIA_BASE_URL wins (e.g. http://localhost:9090/tubely-local or
// https://dxxxx.cloudfront.net); otherwise fall back to the legacy
// S3_CF_DISTRO domain over https.
func resolveMediaBaseURL(mediaBaseURL, cfDistribution string) (string, error) {
	if mediaBaseURL != "" {
		return strings.TrimRight(mediaBaseURL, "/"), nil
	}
	if cfDistribution != "" {
		return "https://" + strings.TrimRight(cfDistribution, "/"), nil
	}
	return "", errors.New("MEDIA_BASE_URL (or legacy S3_CF_DISTRO) must be set")
}

// mediaURL returns the public URL for an object key.
func (cfg *apiConfig) mediaURL(key string) string {
	return cfg.mediaBaseURL + "/" + strings.TrimLeft(key, "/")
}

// withMediaURLs fills in the public URLs for a video from its stored object
// keys. Rows without a key keep their legacy stored URL.
func (cfg *apiConfig) withMediaURLs(v database.Video) database.Video {
	if v.VideoKey != nil && *v.VideoKey != "" {
		u := cfg.mediaURL(*v.VideoKey)
		v.VideoURL = &u
	}
	return v
}
