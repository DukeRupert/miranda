package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}
}

var defaultConfig = CORSConfig{
	AllowedOrigins: []string{"https://example.com"},
	AllowedMethods: []string{"GET", "POST"},
	AllowedHeaders: []string{"Content-Type", "Authorization"},
	MaxAge:         3600,
}

func TestCORS_NoOrigin_PassesThrough(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no Allow-Origin header when no Origin sent")
	}
	// Vary: Origin must still be set for caching correctness
	if rec.Header().Get("Vary") == "" {
		t.Fatal("expected Vary header even without Origin")
	}
}

func TestCORS_DisallowedOrigin_PassesThrough(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should still return 200 — CORS is browser-enforced, not server-enforced
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no Allow-Origin header for disallowed origin")
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", rec.Body.String())
	}
}

func TestCORS_AllowedOrigin_SetsHeaders(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected Allow-Origin %q, got %q", "https://example.com", got)
	}
	// Methods/Headers should NOT be on a normal response
	if rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatal("Allow-Methods should only appear on preflight")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") != "" {
		t.Fatal("Allow-Headers should only appear on preflight")
	}
}

func TestCORS_Preflight_ReturnsNoContent(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected Allow-Origin %q, got %q", "https://example.com", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("expected Allow-Methods %q, got %q", "GET, POST", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Fatalf("expected Allow-Headers %q, got %q", "Content-Type, Authorization", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Fatalf("expected Max-Age %q, got %q", "3600", got)
	}
	// Should NOT call through to the next handler
	if rec.Body.String() != "" {
		t.Fatalf("expected empty body on preflight, got %q", rec.Body.String())
	}
}

func TestCORS_Preflight_DisallowedOrigin(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "https://evil.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Passes through to next handler (no CORS headers, no 204 short-circuit)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no Allow-Origin for disallowed origin preflight")
	}
}

func TestCORS_Credentials(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowCredentials: true,
	}
	handler := CORS(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected Allow-Credentials %q, got %q", "true", got)
	}
}

func TestCORS_NoCredentials(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("expected no Allow-Credentials header when not configured")
	}
}

func TestCORS_Wildcard(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"*"},
	}
	handler := CORS(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anything.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.com" {
		t.Fatalf("expected Allow-Origin %q, got %q", "https://anything.com", got)
	}
}

func TestCORS_DefaultMethodsAndHeaders(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		MaxAge:         60,
	}
	handler := CORS(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, DELETE, OPTIONS" {
		t.Fatalf("expected default methods, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Fatalf("expected default headers, got %q", got)
	}
}

func TestCORS_NoMaxAge(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		MaxAge:         0,
	}
	handler := CORS(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Max-Age") != "" {
		t.Fatal("expected no Max-Age header when MaxAge is 0")
	}
}

func TestCORS_VaryHeaders_OnPreflight(t *testing.T) {
	handler := CORS(defaultConfig)(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	vary := rec.Header().Values("Vary")
	expected := map[string]bool{
		"Origin":                         false,
		"Access-Control-Request-Method":  false,
		"Access-Control-Request-Headers": false,
	}
	for _, v := range vary {
		expected[v] = true
	}
	for k, found := range expected {
		if !found {
			t.Fatalf("expected Vary to include %q", k)
		}
	}
}
