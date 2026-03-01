package middleware

import (
	"crypto/sha256"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"
)

// Fingerprint generates a SHA-256 hash of IP + User-Agent for anonymous tracking (FRD Section 8.6).
// No personally identifiable data is stored.
func Fingerprint(r *http.Request) string {
	ip := r.RemoteAddr
	// If behind a reverse proxy, prefer X-Forwarded-For
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = strings.Split(fwd, ",")[0]
	}
	ua := r.UserAgent()
	hash := sha256.Sum256([]byte(ip + "|" + ua))
	return fmt.Sprintf("%x", hash)
}

// SecurityHeaders adds standard security headers to all responses (FRD Section 7.1).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https://tile.openstreetmap.org; "+
				"connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// RequestLogger logs each HTTP request.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(wrapped, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.statusCode, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// SanitizeString performs basic HTML sanitization on user input (FRD Section 7.1).
func SanitizeString(s string) string {
	s = strings.TrimSpace(s)
	s = html.EscapeString(s)
	return s
}

// SanitizeStringRaw trims whitespace but does not HTML-escape.
// Used for fields that will be escaped at render time by the template engine.
func SanitizeStringRaw(s string) string {
	return strings.TrimSpace(s)
}
