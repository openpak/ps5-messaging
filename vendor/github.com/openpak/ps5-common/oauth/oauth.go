// Package oauth is the bearer token of the OpenPak PS5 auth service: an
// HMAC-SHA256 signed claim set, plus middleware that verifies it.
//
// Wire form: base64url(json claims) "." base64url(hmac). It is an OpenPak
// token, not a JWT; the console never parses it.
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/openpak/ps5-common/httpx"
)

// Claims is what a token asserts.
type Claims struct {
	AccountID string   `json:"sub"`
	OnlineID  string   `json:"oid"`
	Scopes    []string `json:"scp"`
	Expires   int64    `json:"exp"` // unix seconds
}

// HasScope reports whether the claims include scope.
func (c Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Errors returned by Verify.
var (
	ErrMalformed = errors.New("malformed token")
	ErrSignature = errors.New("bad token signature")
	ErrExpired   = errors.New("token expired")
)

var enc = base64.RawURLEncoding

// Signer signs and verifies tokens with one key.
type Signer struct {
	key []byte
	now func() time.Time
}

// NewSigner requires a key of at least 32 bytes.
func NewSigner(key []byte) (*Signer, error) {
	if len(key) < 32 {
		return nil, errors.New("oauth key must be at least 32 bytes")
	}
	return &Signer{key: append([]byte(nil), key...), now: time.Now}, nil
}

func (s *Signer) mac(payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// Sign returns the token for c.
func (s *Signer) Sign(c Claims) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	p := enc.EncodeToString(b)
	return p + "." + enc.EncodeToString(s.mac(p)), nil
}

// Verify checks signature and expiry and returns the claims.
func (s *Signer) Verify(token string) (Claims, error) {
	p, sig, ok := strings.Cut(token, ".")
	if !ok || p == "" || strings.Contains(sig, ".") {
		return Claims{}, ErrMalformed
	}
	got, err := enc.DecodeString(sig)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	if !hmac.Equal(got, s.mac(p)) {
		return Claims{}, ErrSignature
	}
	raw, err := enc.DecodeString(p)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, ErrMalformed
	}
	if c.Expires <= s.now().Unix() {
		return Claims{}, ErrExpired
	}
	return c, nil
}

// Verifier is what Require needs; *Signer implements it.
type Verifier interface {
	Verify(token string) (Claims, error)
}

type ctxKey struct{}

// FromContext returns the claims Require stored.
func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

// BearerToken extracts the token of an "Authorization: Bearer ..." header.
func BearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return "", false
	}
	return strings.TrimSpace(h[7:]), true
}

// Require rejects requests without a valid bearer token (401, PSN error body)
// and otherwise puts the claims in the request context.
func Require(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, ok := BearerToken(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				httpx.WriteError(w, http.StatusUnauthorized, 2102016, "Missing bearer token")
				return
			}
			c, err := v.Verify(tok)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				httpx.WriteError(w, http.StatusUnauthorized, 2102016, "Invalid access token")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, c)))
		})
	}
}
