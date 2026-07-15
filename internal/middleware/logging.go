package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Logging returns middleware that logs each request with slog.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Record the start time for duration measurement.
			start := time.Now()

			// 2. Generate a random request ID and attach it to the response
			//    so it can be correlated in logs and by the client.
			id := requestID()
			w.Header().Set("X-Request-Id", id)

			// 3. Wrap the ResponseWriter to capture the status code
			//    written by downstream handlers.
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			// 4. Pass control to the next handler in the chain.
			next.ServeHTTP(sw, r)

			// 5. Log the completed request with method, path, status,
			//    duration, and remote address.
			logger.Info("request",
				"id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", float64(time.Since(start).Microseconds())/1000,
				"remote", r.RemoteAddr,
			)
		})
	}
}

func requestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
