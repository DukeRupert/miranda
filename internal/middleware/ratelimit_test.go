package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func rateLimitedHandler(config RateLimitConfig) http.Handler {
	return RateLimit(config)(okHandler())
}

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	handler := rateLimitedHandler(RateLimitConfig{
		Rate:  10,
		Burst: 10,
	})

	for i := range 10 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimit_BlocksOverBurst(t *testing.T) {
	handler := rateLimitedHandler(RateLimitConfig{
		Rate:  1,
		Burst: 2,
	})

	// First 2 should succeed (burst).
	for i := range 2 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	// Third should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
}

func TestRateLimit_SeparateLimitersPerIP(t *testing.T) {
	handler := rateLimitedHandler(RateLimitConfig{
		Rate:  1,
		Burst: 1,
	})

	// Exhaust client A's bucket.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1111"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("client A first request: expected 200, got %d", rec.Code)
	}

	// Client A should be blocked.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:2222"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("client A second request: expected 429, got %d", rec.Code)
	}

	// Client B should still be allowed.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:1111"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("client B: expected 200, got %d", rec.Code)
	}
}

func TestRateLimit_SetsInfoHeaders(t *testing.T) {
	handler := rateLimitedHandler(RateLimitConfig{
		Rate:  5,
		Burst: 10,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-RateLimit-Limit"); got != "5" {
		t.Fatalf("expected X-RateLimit-Limit %q, got %q", "5", got)
	}
	if got := rec.Header().Get("X-RateLimit-Burst"); got != "10" {
		t.Fatalf("expected X-RateLimit-Burst %q, got %q", "10", got)
	}
}

func TestRateLimit_CustomKeyFunc(t *testing.T) {
	handler := rateLimitedHandler(RateLimitConfig{
		Rate:  1,
		Burst: 1,
		KeyFunc: func(r *http.Request) string {
			return r.Header.Get("X-API-Key")
		},
	})

	// Exhaust key "abc".
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("key abc first: expected 200, got %d", rec.Code)
	}

	// Key "abc" should be blocked.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "abc")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("key abc second: expected 429, got %d", rec.Code)
	}

	// Key "xyz" should still be allowed.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "xyz")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("key xyz: expected 200, got %d", rec.Code)
	}
}

func TestRateLimit_DefaultBurst(t *testing.T) {
	handler := rateLimitedHandler(RateLimitConfig{
		Rate: 1,
		// Burst intentionally zero — should default to 1.
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Second should be blocked (burst=1 exhausted).
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rec.Code)
	}
}

func TestClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:54321"

	ip := clientIP(req, 0)
	if ip != "203.0.113.1" {
		t.Fatalf("expected %q, got %q", "203.0.113.1", ip)
	}
}

func TestClientIP_XForwardedFor_OneProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	ip := clientIP(req, 1)
	if ip != "203.0.113.50" {
		t.Fatalf("expected %q, got %q", "203.0.113.50", ip)
	}
}

func TestClientIP_XForwardedFor_ChainWithOneProxy(t *testing.T) {
	// Client -> Cloudflare -> Caddy -> App
	// XFF: "client_ip, cloudflare_ip" (Caddy appended cloudflare_ip)
	// With TrustedProxies=1, we want the entry Caddy added's upstream: index len-1=cloudflare_ip
	// Actually: Caddy sees the request from Cloudflare and appends the real client IP.
	// XFF from Caddy: "spoofed_by_client, real_client_ip"
	// TrustedProxies=1 means trust Caddy -> take parts[len-1]
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.50")

	ip := clientIP(req, 1)
	if ip != "203.0.113.50" {
		t.Fatalf("expected %q, got %q", "203.0.113.50", ip)
	}
}

func TestClientIP_XForwardedFor_TwoProxies(t *testing.T) {
	// XFF: "real_client, proxy1_added, proxy2_added"
	// TrustedProxies=2 -> parts[len-2] = proxy1_added... that's wrong.
	// Actually: with 2 trusted proxies, the last 2 entries are from proxies,
	// so the client is at parts[len-2-1]... no.
	// len=3, trustedProxies=2, idx=3-2=1 -> parts[1]
	// "real_client, cdn_ip, caddy_ip" -> parts[1] = cdn_ip
	// Hmm, that's the CDN, not the client. Let me re-read the code.
	// Actually the convention is: each proxy appends the IP it received from.
	// So Caddy (rightmost proxy) appended cdn_ip, CDN appended real_client.
	// XFF = "real_client, cdn_ip" (Caddy appends its own view as RemoteAddr, not to XFF)
	// Wait — each proxy appends the connecting IP to XFF.
	// Client (1.1.1.1) -> CDN (2.2.2.2) -> Caddy -> App
	// CDN receives from 1.1.1.1, sets XFF: "1.1.1.1"
	// Caddy receives from 2.2.2.2, appends: XFF: "1.1.1.1, 2.2.2.2"
	// TrustedProxies=2: idx = 2-2 = 0 -> parts[0] = "1.1.1.1" ✓
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")

	ip := clientIP(req, 2)
	if ip != "1.1.1.1" {
		t.Fatalf("expected %q, got %q", "1.1.1.1", ip)
	}
}

func TestClientIP_IgnoresXFF_WhenZeroProxies(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:54321"
	req.Header.Set("X-Forwarded-For", "10.0.0.99")

	ip := clientIP(req, 0)
	if ip != "203.0.113.1" {
		t.Fatalf("expected %q, got %q", "203.0.113.1", ip)
	}
}

func TestClientIP_InvalidXFF_FallsBack(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:54321"
	req.Header.Set("X-Forwarded-For", "not-an-ip")

	ip := clientIP(req, 1)
	if ip != "203.0.113.1" {
		t.Fatalf("expected fallback to RemoteAddr, got %q", ip)
	}
}

func TestFormatRate_Integer(t *testing.T) {
	if got := formatRate(10); got != "10" {
		t.Fatalf("expected %q, got %q", "10", got)
	}
}

func TestFormatRate_Fractional(t *testing.T) {
	if got := formatRate(0.5); got != "0.50" {
		t.Fatalf("expected %q, got %q", "0.50", got)
	}
}

func TestFormatRate_Unlimited(t *testing.T) {
	if got := formatRate(rate.Inf); got != "unlimited" {
		t.Fatalf("expected %q, got %q", "unlimited", got)
	}
}
