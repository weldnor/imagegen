package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session is a server-side login session.
type Session struct {
	ID        string
	UserID    string
	Username  string
	ExpiresAt time.Time
}

// SessionStore persists sessions in PostgreSQL.
type SessionStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

// NewSessionStore returns a store whose new sessions live for ttl.
func NewSessionStore(pool *pgxpool.Pool, ttl time.Duration) *SessionStore {
	return &SessionStore{pool: pool, ttl: ttl}
}

// newID returns 32 cryptographically-random bytes, base64url-encoded without
// padding, for use as a cookie value.
func newID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create makes a new session for userID expiring ttl from now.
func (s *SessionStore) Create(ctx context.Context, userID string) (Session, error) {
	id, err := newID()
	if err != nil {
		return Session{}, fmt.Errorf("generating session id: %w", err)
	}
	expires := time.Now().Add(s.ttl)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		id, userID, expires); err != nil {
		return Session{}, fmt.Errorf("inserting session: %w", err)
	}
	return Session{ID: id, UserID: userID, ExpiresAt: expires}, nil
}

// Lookup returns the session for id and true when it exists and has not
// expired. A missing or expired session returns ok=false and a nil error;
// err is non-nil only on an actual database failure.
func (s *SessionStore) Lookup(ctx context.Context, id string) (sess Session, ok bool, err error) {
	if id == "" {
		return Session{}, false, nil
	}
	var expires time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, u.username, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = $1`, id).Scan(&sess.ID, &sess.UserID, &sess.Username, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	sess.ExpiresAt = expires
	if time.Now().After(expires) {
		// Opportunistically drop the dead row; ignore any error.
		_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
		return Session{}, false, nil
	}
	return sess, true, nil
}

// Touch refreshes last_seen_at for a live session. Errors are non-fatal to a
// request and may be ignored by callers.
func (s *SessionStore) Touch(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, id)
	return err
}

// Delete removes a session by id. Deleting a non-existent session is not an error.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// PurgeExpired deletes every expired session and returns how many were removed.
func (s *SessionStore) PurgeExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// StartPurgeLoop runs PurgeExpired every interval until ctx is done.
func (s *SessionStore) StartPurgeLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = s.PurgeExpired(ctx)
		}
	}
}
