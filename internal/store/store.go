package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/firefly-software-mt/advanced-template/internal/db"

	"golang.org/x/crypto/bcrypt"
)

// Store wraps the sqlc-generated query layer and adds business logic
// (e.g. password hashing). SQL lives in queries/*.sql — run `mage generate`
// after editing queries to regenerate internal/db.
type Store struct {
	q *db.Queries
}

// New creates a new Store.
func New(sqlDB *sql.DB) *Store {
	return &Store{q: db.New(sqlDB)}
}

// CreateSession inserts a new session row.
func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error {
	_, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        token,
		UserID:    userID,
		ExpiresAt: expiresAt,
	})
	return err
}

// GetSession retrieves a valid (non-expired) session by token.
func (s *Store) GetSession(ctx context.Context, token string) (int64, time.Time, error) {
	sess, err := s.q.GetSession(ctx, token)
	if err != nil {
		return 0, time.Time{}, err
	}
	return sess.UserID, sess.ExpiresAt, nil
}

// DeleteSession removes a session by token.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	return s.q.DeleteSession(ctx, token)
}

// DeleteUserSessions removes all sessions for a user (e.g. on password change).
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	return s.q.DeleteUserSessions(ctx, userID)
}

// DeleteExpiredSessions removes all expired sessions.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	return s.q.DeleteExpiredSessions(ctx)
}

// GetUserByID retrieves a user's id and email by their ID.
func (s *Store) GetUserByID(ctx context.Context, id int64) (int64, string, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return 0, "", err
	}
	return u.ID, u.Email, nil
}

// GetUserByEmail retrieves a user by email, returning id, email, and password hash.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (int64, string, string, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return 0, "", "", err
	}
	return u.ID, u.Email, u.PasswordHash, nil
}

// CreateUser inserts a new user with a bcrypt-hashed password.
func (s *Store) CreateUser(ctx context.Context, email, password string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	u, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: string(hash),
	})
	if err != nil {
		return 0, err
	}
	return u.ID, nil
}
