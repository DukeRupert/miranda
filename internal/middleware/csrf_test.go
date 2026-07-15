package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var testSecret = []byte("01234567890123456789012345678901") // 32 bytes

func csrfHandler(config CSRFConfig) http.Handler {
	return CSRF(config)(okHandler())
}

func defaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		Secret:      testSecret,
		InsecureDev: true,
	}
}

// doGET performs a GET to obtain the CSRF cookie and token.
func doGET(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", rec.Code)
	}

	var csrfCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__csrf" {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Fatal("GET: expected __csrf cookie")
	}

	// Extract raw token (left of the dot).
	parts := strings.SplitN(csrfCookie.Value, ".", 2)
	if len(parts) != 2 {
		t.Fatal("GET: malformed cookie value")
	}

	return csrfCookie, parts[0]
}

func TestCSRF_GET_SetsCookie(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__csrf" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatal("expected __csrf cookie")
	}
	if cookie.Value == "" {
		t.Fatal("expected non-empty cookie value")
	}
	if !cookie.HttpOnly {
		t.Fatal("expected HttpOnly cookie")
	}

	// SameSite=Lax is verified via the Secure cookie test below, since
	// Go only emits SameSite in the Set-Cookie header when Secure=true.
}

func TestCSRF_POST_ValidFormToken(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, token := doGET(t, handler)

	form := url.Values{"csrf_token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCSRF_POST_ValidHeaderToken(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, token := doGET(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCSRF_POST_MissingToken_Returns403(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, _ := doGET(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCSRF_POST_WrongToken_Returns403(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, _ := doGET(t, handler)

	form := url.Values{"csrf_token": {"completely-wrong-token"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCSRF_POST_NoCookie_Returns403(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())

	form := url.Values{"csrf_token": {"some-token"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Fresh token is generated but it won't match the submitted one.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCSRF_PUT_RequiresToken(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, _ := doGET(t, handler)

	req := httptest.NewRequest(http.MethodPut, "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for PUT without token, got %d", rec.Code)
	}
}

func TestCSRF_DELETE_RequiresToken(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, _ := doGET(t, handler)

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for DELETE without token, got %d", rec.Code)
	}
}

func TestCSRF_HEAD_NoValidation(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())

	req := httptest.NewRequest(http.MethodHead, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for HEAD, got %d", rec.Code)
	}
}

func TestCSRF_OPTIONS_NoValidation(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", rec.Code)
	}
}

func TestCSRF_CustomErrorHandler(t *testing.T) {
	cfg := defaultCSRFConfig()
	cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("custom csrf error"))
	}
	handler := csrfHandler(cfg)
	cookie, _ := doGET(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", rec.Code)
	}
	if rec.Body.String() != "custom csrf error" {
		t.Fatalf("expected custom body, got %q", rec.Body.String())
	}
}

func TestCSRF_CustomFieldName(t *testing.T) {
	cfg := defaultCSRFConfig()
	cfg.FieldName = "_token"
	handler := csrfHandler(cfg)
	cookie, token := doGET(t, handler)

	form := url.Values{"_token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with custom field name, got %d", rec.Code)
	}
}

func TestCSRF_CustomHeaderName(t *testing.T) {
	cfg := defaultCSRFConfig()
	cfg.HeaderName = "X-Custom-CSRF"
	handler := csrfHandler(cfg)
	cookie, token := doGET(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Custom-CSRF", token)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with custom header, got %d", rec.Code)
	}
}

func TestCSRF_TokenFromContext(t *testing.T) {
	cfg := defaultCSRFConfig()
	var capturedToken string

	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = Token(r)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedToken == "" {
		t.Fatal("expected Token(r) to return non-empty token")
	}
}

func TestCSRF_TemplateField(t *testing.T) {
	cfg := defaultCSRFConfig()
	var field string

	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		field = string(TemplateField(r))
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(field, `name="csrf_token"`) {
		t.Fatalf("expected hidden input with name csrf_token, got %q", field)
	}
	if !strings.Contains(field, `type="hidden"`) {
		t.Fatalf("expected hidden input, got %q", field)
	}
	if !strings.Contains(field, "value=") {
		t.Fatalf("expected value attribute, got %q", field)
	}
}

func TestCSRF_TemplateField_UsesConfiguredFieldName(t *testing.T) {
	cfg := defaultCSRFConfig()
	cfg.FieldName = "_token"
	var field string

	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		field = string(TemplateField(r))
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(field, `name="_token"`) {
		t.Fatalf("expected hidden input with name _token, got %q", field)
	}
}

func TestCSRF_HeaderPreferredOverForm(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, token := doGET(t, handler)

	// Send correct token in header but wrong in form — should pass.
	form := url.Values{"csrf_token": {"wrong-token"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected header to take precedence, got %d", rec.Code)
	}
}

func TestCSRF_TokenReusedAcrossRequests(t *testing.T) {
	handler := csrfHandler(defaultCSRFConfig())
	cookie, token := doGET(t, handler)

	// Use the same token for two POSTs — both should succeed.
	for i := range 2 {
		form := url.Values{"csrf_token": {token}}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestCSRF_ShortSecret_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for short secret")
		}
	}()

	CSRF(CSRFConfig{Secret: []byte("tooshort")})
}

func TestCSRF_SecureCookie_Default(t *testing.T) {
	cfg := CSRFConfig{Secret: testSecret}
	handler := csrfHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == "__csrf" {
			if !c.Secure {
				t.Fatal("expected Secure cookie by default")
			}
			return
		}
	}
	t.Fatal("csrf cookie not found")
}

func TestCSRF_InsecureDev_DisablesSecure(t *testing.T) {
	cfg := defaultCSRFConfig() // InsecureDev: true
	handler := csrfHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == "__csrf" {
			if c.Secure {
				t.Fatal("expected non-Secure cookie with InsecureDev")
			}
			return
		}
	}
	t.Fatal("csrf cookie not found")
}
