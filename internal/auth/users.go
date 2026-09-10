// Package auth handles user credentials, password verification, server-side
// sessions, and the session middleware that gates the data API.
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// bcryptPlaceholderHash is compared against when an unknown username is
// submitted, so a login attempt for a non-existent user takes about the same
// time as one for a real user (mitigates username enumeration by timing). It is
// a bcrypt hash of a random string; nothing matches it.
const bcryptPlaceholderHash = "$2a$10$C6UzMDM.H6dfI/f/IKcEeO.eZC0l1i/nW8W1kZzZ0zq7Yx3mXQ5Iu"

// ErrUsernameTaken is returned by CreateUser when the username already exists.
var ErrUsernameTaken = errors.New("username already exists")

// ErrTelegramIDLinked is returned by CreateUser/AddTelegramID when a Telegram
// ID is already linked to a (possibly different) user.
var ErrTelegramIDLinked = errors.New("telegram id is already linked to a user")

// ErrUserNotFound is returned by AddTelegramID when the named user does not exist.
var ErrUserNotFound = errors.New("user not found")

// UserSummary describes one configured user for /listusers.
type UserSummary struct {
	Username    string
	TelegramIDs []int64
}

// Users is the DB-backed credential and account-management store, backing both
// the web login and the Telegram bot's authentication and admin commands.
type Users struct {
	pool *pgxpool.Pool
}

// NewUsers returns a Users store backed by pool.
func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

// VerifyPassword checks a username/password pair against the users table. On
// success it returns the user's id and true. On an unknown username, or one
// with no password set, it still runs a bcrypt comparison against a
// placeholder hash so the response time does not reveal whether the username
// exists.
func (u *Users) VerifyPassword(ctx context.Context, username, password string) (userID string, ok bool, err error) {
	var id string
	var hash *string
	err = u.pool.QueryRow(ctx,
		`SELECT id, password_hash FROM users WHERE username = $1`, username).Scan(&id, &hash)
	found := true
	if errors.Is(err, pgx.ErrNoRows) {
		found = false
		err = nil
	} else if err != nil {
		return "", false, err
	}

	checkHash := bcryptPlaceholderHash
	if found && hash != nil && *hash != "" {
		checkHash = *hash
	} else {
		found = false
	}

	cmpErr := bcrypt.CompareHashAndPassword([]byte(checkHash), []byte(password))
	if !found || cmpErr != nil {
		return "", false, nil
	}
	return id, true, nil
}

// LookupByTelegramID returns the user linked to telegramID, if any.
func (u *Users) LookupByTelegramID(ctx context.Context, telegramID int64) (userID, username string, ok bool, err error) {
	err = u.pool.QueryRow(ctx, `
		SELECT u.id, u.username
		FROM user_telegram_ids t JOIN users u ON u.id = t.user_id
		WHERE t.telegram_id = $1`, telegramID).Scan(&userID, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return userID, username, true, nil
}

// CreateUser hashes password and creates a new user, optionally linking every
// given Telegram ID to it in the same transaction. It returns ErrUsernameTaken
// or ErrTelegramIDLinked without creating anything on conflict.
func (u *Users) CreateUser(ctx context.Context, username, password string, telegramIDs []int64) (userID string, err error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit

	err = tx.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, string(hash)).Scan(&userID)
	if isUniqueViolation(err, "users_username_key") {
		return "", ErrUsernameTaken
	}
	if err != nil {
		return "", err
	}

	for _, tgID := range telegramIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_telegram_ids (telegram_id, user_id) VALUES ($1, $2)`,
			tgID, userID); err != nil {
			if isUniqueViolation(err, "user_telegram_ids_pkey") {
				return "", ErrTelegramIDLinked
			}
			return "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

// AddTelegramID links telegramID to the existing user named username. It
// returns ErrUserNotFound or ErrTelegramIDLinked without making a change.
func (u *Users) AddTelegramID(ctx context.Context, username string, telegramID int64) error {
	var userID string
	err := u.pool.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}

	_, err = u.pool.Exec(ctx,
		`INSERT INTO user_telegram_ids (telegram_id, user_id) VALUES ($1, $2)`, telegramID, userID)
	if isUniqueViolation(err, "user_telegram_ids_pkey") {
		return ErrTelegramIDLinked
	}
	return err
}

// ListUsers returns every user with its linked Telegram IDs, ordered by
// username.
func (u *Users) ListUsers(ctx context.Context) ([]UserSummary, error) {
	rows, err := u.pool.Query(ctx, `
		SELECT u.username, t.telegram_id
		FROM users u LEFT JOIN user_telegram_ids t ON t.user_id = u.id
		ORDER BY u.username, t.telegram_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byName := map[string]*UserSummary{}
	var order []string
	for rows.Next() {
		var username string
		var tgID *int64
		if err := rows.Scan(&username, &tgID); err != nil {
			return nil, err
		}
		s, ok := byName[username]
		if !ok {
			s = &UserSummary{Username: username}
			byName[username] = s
			order = append(order, username)
		}
		if tgID != nil {
			s.TelegramIDs = append(s.TelegramIDs, *tgID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]UserSummary, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

// isUniqueViolation reports whether err is a Postgres unique_violation, and
// (when constraint is non-empty) matches the named constraint.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}
