// Package httpx holds the small HTTP helpers every ps5 service uses.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// MaxBody is the largest request body ReadJSON accepts.
const MaxBody = 1 << 20

// ErrorBody is the error shape the PSN web API returns.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the inner object of ErrorBody.
type ErrorDetail struct {
	ReferenceID string `json:"referenceId"`
	Code        int    `json:"code"`
	Message     string `json:"message"`
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a PSN-shaped error body with a fresh reference id.
func WriteError(w http.ResponseWriter, status, code int, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{ReferenceID: NewReferenceID(), Code: code, Message: message}})
}

// ReadJSON decodes the request body (at most MaxBody bytes, one value) into v.
func ReadJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, MaxBody))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON value")
	}
	return nil
}

// NewReferenceID returns a random UUID-shaped id, as PSN error bodies carry.
func NewReferenceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// Mux returns a ServeMux with GET /healthz answering 200 {"status":"ok"}.
func Mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger logs one line per request (method, path, status, duration) to l.
func Logger(l *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		l.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start))
	})
}
