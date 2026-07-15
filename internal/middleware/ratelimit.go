package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig controls per-client request throttling.
type RateLimitConfig struct {
	// Rate is the number of requests allowed per second (steady state).
	Rate rate.Limit

	// Burst is the maximum number of requests allowed in a single instant.
	// Should be >= Rate for most use cases.
	Burst int

	// TrustedProxies is the number of reverse proxies in front of the app.
	// 0 = use RemoteAddr only (direct connection)
	// 1 = trust one proxy's X-Forwarded-For entry (default for Caddy on Hetzner)
	TrustedProxies int

	// KeyFunc extracts the rate limit key from the request.
	// Defaults to clientIP(r, TrustedProxies) if nil.
	KeyFunc func(r *http.Request) string

	// CleanupInterval controls how often the visitor map is swept.
	// Visitors not seen for 2x this duration are removed.
	// Defaults to 5 minutes if zero.
	CleanupInterval time.Duration
}

// RateLimit returns middleware that throttles requests per client using a
// token bucket algorithm. Each unique key (default: client IP) gets its
// own bucket with the configured rate and burst.
func RateLimit(config RateLimitConfig) func(http.Handler) http.Handler {
	// 1. Apply defaults for zero-value fields.
	if config.Burst == 0 {
		config.Burst = 1
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 5 * time.Minute
	}

	// 2. Create the visitor store and start a background goroutine
	//    that periodically evicts stale entries to bound memory.
	store := newVisitorStore(config.Rate, config.Burst)
	go store.cleanup(config.CleanupInterval)

	// 3. Resolve the key function — default to client IP extraction.
	keyFunc := config.KeyFunc
	if keyFunc == nil {
		keyFunc = func(r *http.Request) string {
			return clientIP(r, config.TrustedProxies)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 4. Look up (or create) the rate limiter for this client.
			key := keyFunc(r)
			limiter := store.get(key)

			// 5. Set informational headers so clients know their limits.
			w.Header().Set("X-RateLimit-Limit", formatRate(config.Rate))
			w.Header().Set("X-RateLimit-Burst", strconv.Itoa(config.Burst))

			// 6. If the bucket is empty, reject with 429 and a Retry-After hint.
			if !limiter.Allow() {
				retryAfter := int(math.Ceil(1.0 / float64(config.Rate)))
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			// 7. Token consumed — pass through to the next handler.
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the real client IP, respecting the number of trusted proxies.
// With TrustedProxies == 1 (Caddy), we take the rightmost entry in X-Forwarded-For
// that Caddy appended — which is the actual client IP as Caddy saw it.
// With TrustedProxies == 0 we use RemoteAddr directly, ignoring the header entirely.
func clientIP(r *http.Request, trustedProxies int) string {
	if trustedProxies > 0 {
		xff := r.Header.Get("X-Forwarded-For")
		if xff != "" {
			parts := strings.Split(xff, ",")
			// Walk in from the right, skipping one entry per trusted proxy.
			// The entry just beyond the trusted proxies is the client.
			idx := max(len(parts)-trustedProxies, 0)
			ip := strings.TrimSpace(parts[idx])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Fall back to RemoteAddr, stripping the port.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// visitor pairs a token-bucket limiter with a last-seen timestamp for cleanup.
type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// visitorStore is a thread-safe map of rate limiters keyed by client identity.
type visitorStore struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
}

func newVisitorStore(r rate.Limit, burst int) *visitorStore {
	return &visitorStore{
		visitors: make(map[string]*visitor),
		rate:     r,
		burst:    burst,
	}
}

// get returns the limiter for the given key, creating one if needed.
func (s *visitorStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.visitors[key]
	if !ok {
		v = &visitor{
			limiter: rate.NewLimiter(s.rate, s.burst),
		}
		s.visitors[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// cleanup runs in a background goroutine and evicts visitors that haven't
// been seen for 2x the cleanup interval to bound memory growth.
func (s *visitorStore) cleanup(interval time.Duration) {
	for {
		time.Sleep(interval)
		cutoff := time.Now().Add(-2 * interval)
		s.mu.Lock()
		for key, v := range s.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(s.visitors, key)
			}
		}
		s.mu.Unlock()
	}
}

// formatRate renders rate.Limit as a clean string for the header.
func formatRate(r rate.Limit) string {
	if r == rate.Inf {
		return "unlimited"
	}
	f := float64(r)
	if f == math.Trunc(f) {
		return strconv.Itoa(int(f))
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}
