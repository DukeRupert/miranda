package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	cookieName     = "session_token"
	sessionMaxAge  = 7 * 24 * time.Hour
)

type ctxKey struct{}

// User represents the authenticated user attached to a request context.
type User struct {
	ID    int64
	Email string
}

// FromContext returns the authenticated user from the request context, or nil.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// withUser attaches a user to the context.
func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// Store defines the session persistence interface.
type Store interface {
	CreateSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error
	GetSession(ctx context.Context, token string) (userID int64, expiresAt time.Time, err error)
	DeleteSession(ctx context.Context, token string) error
	GetUserByID(ctx context.Context, id int64) (int64, string, error)
}

// Middleware loads the session from the cookie and attaches the user to the request context.
func Middleware(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			userID, expiresAt, err := store.GetSession(r.Context(), cookie.Value)
			if err != nil || time.Now().After(expiresAt) {
				next.ServeHTTP(w, r)
				return
			}

			id, email, err := store.GetUserByID(r.Context(), userID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := withUser(r.Context(), &User{ID: id, Email: email})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth wraps a handler and redirects unauthenticated users to /login.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Create generates a new session token, persists it, and sets the cookie.
func Create(ctx context.Context, w http.ResponseWriter, store Store, userID int64) error {
	token, err := generateToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(sessionMaxAge)
	if err := store.CreateSession(ctx, token, userID, expiresAt); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
	return nil
}

// Destroy removes the session from the store and clears the cookie.
func Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request, store Store) error {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}

	if err := store.DeleteSession(ctx, cookie.Value); err != nil && err != sql.ErrNoRows {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	return nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
