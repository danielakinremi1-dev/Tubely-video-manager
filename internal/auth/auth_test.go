package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestJWTRoundTrip(t *testing.T) {
	userID := uuid.New()
	token, err := MakeJWT(userID, "secret", time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	got, err := ValidateJWT(token, "secret")
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}
	if got != userID {
		t.Fatalf("user ID = %v, want %v", got, userID)
	}
}

func TestValidateJWTRejects(t *testing.T) {
	userID := uuid.New()

	valid, err := MakeJWT(userID, "secret", time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	expired, err := MakeJWT(userID, "secret", -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	wrongIssuer, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "someone-else",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Subject:   userID.String(),
	}).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	tests := []struct {
		name, token, secret string
	}{
		{"wrong secret", valid, "other-secret"},
		{"expired", expired, "secret"},
		{"wrong issuer", wrongIssuer, "secret"},
		{"garbage", "not.a.jwt", "secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ValidateJWT(tt.token, tt.secret); err == nil {
				t.Fatal("ValidateJWT succeeded, want error")
			}
		})
	}
}
