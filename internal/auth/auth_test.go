package auth

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
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

	claims := jwt.RegisteredClaims{
		Issuer:    string(TokenTypeAccess),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Subject:   userID.String(),
	}
	// Correctly signed with the right secret, but not with HS256.
	hs512, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// alg "none": no signature at all.
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	tests := []struct {
		name, token, secret string
	}{
		{"wrong secret", valid, "other-secret"},
		{"HS512 instead of HS256", hs512, "secret"},
		{"alg none", none, "secret"},
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

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name, header, want string
		wantErr            bool
	}{
		{"valid", "Bearer abc.def.ghi", "abc.def.ghi", false},
		{"scheme is case-insensitive (RFC 7235)", "bearer abc", "abc", false},
		{"extra spaces", "Bearer   abc  ", "abc", false},
		{"missing", "", "", true},
		{"other scheme", "Basic dXNlcjpwYXNz", "", true},
		{"scheme only", "Bearer", "", true},
		{"empty token", "Bearer ", "", true},
		{"token with a space", "Bearer abc def", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.header != "" {
				h.Set("Authorization", tt.header)
			}
			got, err := GetBearerToken(h)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v (token %q)", err, tt.wantErr, got)
			}
			if got != tt.want {
				t.Errorf("token = %q, want %q", got, tt.want)
			}
		})
	}
	if _, err := GetBearerToken(http.Header{}); !errors.Is(err, ErrNoAuthHeaderIncluded) {
		t.Errorf("missing header: err = %v, want ErrNoAuthHeaderIncluded", err)
	}
}

func TestMakeRefreshToken(t *testing.T) {
	a, err := MakeRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := MakeRefreshToken()
	if len(a) != 64 {
		t.Errorf("len = %d, want 64 hex chars (256 bits)", len(a))
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("%q isn't hex: %v", a, err)
	}
	if a == b {
		t.Error("two refresh tokens are identical")
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "correct horse") {
		t.Errorf("hash %q isn't an argon2id hash of the password", hash)
	}
	if again, _ := HashPassword("correct horse"); again == hash {
		t.Error("same password hashed twice gave the same hash: no random salt")
	}
	if ok, err := CheckPasswordHash("correct horse", hash); err != nil || !ok {
		t.Errorf("right password: ok=%v err=%v", ok, err)
	}
	if ok, err := CheckPasswordHash("wrong horse", hash); err != nil || ok {
		t.Errorf("wrong password: ok=%v err=%v", ok, err)
	}
	if _, err := CheckPasswordHash("x", "not-a-hash"); err == nil {
		t.Error("malformed hash: want an error")
	}
}
