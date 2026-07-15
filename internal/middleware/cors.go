package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSConfig controls which cross-origin requests are permitted.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
	MaxAge           int // seconds; only sent on preflight
}

func (c *CORSConfig) isOriginAllowed(origin string) bool {
	for _, allowed := range c.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (c *CORSConfig) allowedMethods() string {
	if len(c.AllowedMethods) == 0 {
		return "GET, POST, PUT, DELETE, OPTIONS"
	}
	return strings.Join(c.AllowedMethods, ", ")
}

func (c *CORSConfig) allowedHeaders() string {
	if len(c.AllowedHeaders) == 0 {
		return "Content-Type"
	}
	return strings.Join(c.AllowedHeaders, ", ")
}

// CORS returns middleware that handles cross-origin request headers.
// Disallowed origins receive no CORS headers — the browser enforces the block.
func CORS(config CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Always set Vary: Origin so shared caches don't serve a
			//    CORS-headered response to a same-origin request (or vice versa).
			w.Header().Add("Vary", "Origin")

			// 2. If there's no Origin header (same-origin / non-browser) or the
			//    origin isn't in our allow list, skip CORS headers entirely and
			//    pass through. The browser will block the response on its side.
			origin := r.Header.Get("Origin")
			if origin == "" || !config.isOriginAllowed(origin) {
				next.ServeHTTP(w, r)
				return
			}

			// 3. Echo the matched origin back (not "*") so credentials work
			//    and caches key correctly.
			w.Header().Set("Access-Control-Allow-Origin", origin)

			// 4. If credentials (cookies, auth headers) are allowed, tell the browser.
			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// 5. Handle preflight (OPTIONS) requests. These headers only matter
			//    here — they're meaningless on actual requests.
			if r.Method == http.MethodOptions {
				// Tell caches this response varies by the preflight request details.
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")

				// Declare which methods and headers the server accepts.
				w.Header().Set("Access-Control-Allow-Methods", config.allowedMethods())
				w.Header().Set("Access-Control-Allow-Headers", config.allowedHeaders())

				// Let the browser cache this preflight for MaxAge seconds.
				if config.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
				}

				// Respond with 204 No Content — no body needed for preflight.
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// 6. Actual request — CORS headers are set, pass through to handler.
			next.ServeHTTP(w, r)
		})
	}
}
