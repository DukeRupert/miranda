package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// csrfContextKey is an unexported type for context keys in this package.
type csrfContextKey struct{}

// csrfFieldKey stores the configured field name in context for TemplateField.
type csrfFieldKey struct{}

// CSRFConfig holds all configuration for the CSRF middleware.
type CSRFConfig struct {
	// Secret is the HMAC signing key. Must be at least 32 bytes.
	// Load from environment — never hardcode.
	// A missing or short secret causes a panic at middleware init.
	Secret []byte

	// CookieName is the name of the CSRF cookie.
	// Defaults to "__csrf" if empty.
	CookieName string

	// FieldName is the hidden form field name checked on mutation requests.
	// Defaults to "csrf_token" if empty.
	FieldName string

	// HeaderName is checked before the form field on mutation requests.
	// Pair with htmx via hx-headers='{"X-CSRF-Token": "..."}' or a global
	// htmx config. Defaults to "X-CSRF-Token" if empty.
	HeaderName string

	// CookiePath scopes the cookie. Defaults to "/".
	CookiePath string

	// SameSite controls the SameSite cookie attribute.
	// Defaults to http.SameSiteLaxMode if unset (zero value).
	// Use http.SameSiteStrictMode for admin panels.
	SameSite http.SameSite

	// InsecureDev disables the Secure cookie flag.
	// Must be explicitly set to true for local HTTP development.
	// The Secure flag is on by default.
	InsecureDev bool

	// ErrorHandler writes the full response on CSRF validation failure.
	// Defaults to plain-text 403 Forbidden if nil.
	ErrorHandler func(w http.ResponseWriter, r *http.Request)
}

// newCSRFConfig applies defaults and validates the config.
// Panics on startup misconfiguration — a short or missing secret is never
// a runtime condition we should attempt to recover from.
func newCSRFConfig(c CSRFConfig) CSRFConfig {
	if len(c.Secret) < 32 {
		panic(fmt.Sprintf(
			"csrf: Secret must be at least 32 bytes, got %d — "+
				"load from environment with os.Getenv and never hardcode",
			len(c.Secret),
		))
	}
	if c.CookieName == "" {
		c.CookieName = "__csrf"
	}
	if c.FieldName == "" {
		c.FieldName = "csrf_token"
	}
	if c.HeaderName == "" {
		c.HeaderName = "X-CSRF-Token"
	}
	if c.CookiePath == "" {
		c.CookiePath = "/"
	}
	if c.SameSite == http.SameSiteDefaultMode {
		c.SameSite = http.SameSiteLaxMode
	}
	if c.ErrorHandler == nil {
		c.ErrorHandler = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		}
	}
	return c
}

// safeMethods are the HTTP methods that do not require CSRF validation.
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// CSRF returns middleware that issues and validates CSRF tokens using the
// signed double-submit cookie pattern.
func CSRF(config CSRFConfig) func(http.Handler) http.Handler {
	c := newCSRFConfig(config)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Retrieve or generate the raw token from the cookie.
			//    Reuse only if the signature verifies against the current
			//    secret — a stale signature (e.g. after secret rotation)
			//    gets a fresh token instead of failing every POST until
			//    the cookie expires.
			token := ""
			cookieVal := ""
			if cookie, err := r.Cookie(c.CookieName); err == nil {
				parts := strings.SplitN(cookie.Value, ".", 2)
				if len(parts) == 2 && verifyToken(c.Secret, cookie.Value, parts[0]) {
					token = parts[0]
					cookieVal = cookie.Value
				}
			}
			if token == "" {
				token = generateToken()
				cookieVal = cookieValue(c.Secret, token)
			}

			// 2. Always reissue the cookie so it stays fresh.
			http.SetCookie(w, &http.Cookie{
				Name:     c.CookieName,
				Value:    cookieVal,
				Path:     c.CookiePath,
				HttpOnly: true,
				Secure:   !c.InsecureDev,
				SameSite: c.SameSite,
			})

			// 3. Store the raw token and field name in context for
			//    Token(r) and TemplateField(r).
			ctx := context.WithValue(r.Context(), csrfContextKey{}, token)
			ctx = context.WithValue(ctx, csrfFieldKey{}, c.FieldName)
			r = r.WithContext(ctx)

			// 4. Safe methods pass through without validation.
			if safeMethods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}

			// 5. Mutation request — extract the submitted token.
			//    Header is checked first (htmx/fetch), form field second.
			submitted := r.Header.Get(c.HeaderName)
			if submitted == "" {
				if err := r.ParseForm(); err != nil {
					c.ErrorHandler(w, r)
					return
				}
				submitted = r.FormValue(c.FieldName)
			}

			if submitted == "" {
				c.ErrorHandler(w, r)
				return
			}

			// 6. Verify the submitted token against the cookie value using
			//    constant-time comparison to prevent timing attacks.
			if !verifyToken(c.Secret, cookieVal, submitted) {
				c.ErrorHandler(w, r)
				return
			}

			// 7. Token valid — pass through to the next handler.
			next.ServeHTTP(w, r)
		})
	}
}

// Token returns the raw CSRF token from the request context.
// Returns an empty string if the middleware has not been applied.
func Token(r *http.Request) string {
	return TokenFromContext(r.Context())
}

// TokenFromContext returns the raw CSRF token from a context. Useful in
// templ components, which receive the request context rather than the
// request itself. Returns an empty string if the middleware has not run.
func TokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

// TemplateField returns an html/template.HTML snippet containing a hidden
// input field populated with the CSRF token. Safe to render directly in
// Go templates without further escaping.
//
//	{{ csrfField .Request }}
func TemplateField(r *http.Request) template.HTML {
	token := Token(r)
	if token == "" {
		return ""
	}
	fieldName, _ := r.Context().Value(csrfFieldKey{}).(string)
	if fieldName == "" {
		fieldName = "csrf_token"
	}
	return template.HTML(fmt.Sprintf(
		`<input type="hidden" name="%s" value="%s">`,
		template.HTMLEscapeString(fieldName),
		template.HTMLEscapeString(token),
	))
}

// generateToken returns a cryptographically random 32-byte token
// encoded as a base64 URL string. Panics if the system CSPRNG fails —
// if crypto/rand is broken, nothing security-related should proceed.
func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("csrf: failed to read from crypto/rand: %v", err))
	}
	return base64.URLEncoding.EncodeToString(b)
}

// signToken returns an HMAC-SHA256 signature of the token using the secret.
func signToken(secret []byte, token string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(token))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// cookieValue encodes the token and its HMAC signature as "token.signature".
func cookieValue(secret []byte, token string) string {
	return token + "." + signToken(secret, token)
}

// verifyToken checks the submitted token against the cookie value using
// constant-time comparison to prevent timing attacks.
func verifyToken(secret []byte, cookieVal, submitted string) bool {
	expected := cookieValue(secret, submitted)
	return hmac.Equal([]byte(expected), []byte(cookieVal))
}
