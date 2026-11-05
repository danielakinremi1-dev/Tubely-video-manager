package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

// maxJSONBodyBytes caps JSON request bodies; every JSON payload here is tiny.
const maxJSONBodyBytes = 1 << 20

// decodeJSON decodes a JSON request body into dst. A malformed or oversized
// body is the client's fault, so it responds 400 (or 413) and returns false;
// the caller just returns.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondWithError(w, http.StatusRequestEntityTooLarge, "Request body is too large", err)
			return false
		}
		respondWithError(w, http.StatusBadRequest, "Request body must be valid JSON", err)
		return false
	}
	return true
}

func respondWithError(w http.ResponseWriter, code int, msg string, err error) {
	if err != nil {
		log.Println(err)
	}
	if code > 499 {
		log.Printf("Responding with 5XX error: %s", msg)
	}
	type errorResponse struct {
		Error string `json:"error"`
	}
	respondWithJSON(w, code, errorResponse{
		Error: msg,
	})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(code)
	w.Write(dat)
}
