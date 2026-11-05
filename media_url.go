package main

import (
	"errors"
	"strings"
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
