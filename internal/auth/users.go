// Package auth handles user credentials, password verification, server-side
// sessions, and the session middleware that gates the data API.
package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/weldnor/imagegen/internal/config"
)

// bcryptPlaceholderHash is compared against when an unknown username is
// submitted, so a login attempt for a non-existent user takes about the same
// time as one for a real user (mitigates username enumeration by timing). It is
// a bcrypt hash of a random string; nothing matches it.
const bcryptPlaceholderHash = "$2a$10$C6UzMDM.H6dfI/f/IKcEeO.eZC0l1i/nW8W1kZzZ0zq7Yx3mXQ5Iu"

// Credential is one configured user held in memory.
type Credential struct {
	UserID   string // uuid; empty until SyncToDB has run
	Username string
	Hash     string
}

// Users is the in-memory credential map built from AUTH_USERS.
type Users struct {
	byName map[string]*Credential
}

// NewUsers builds the credential map from parsed config entries. It rejects an
// empty list, empty usernames, empty hashes, non-bcrypt hashes, and duplicate
// usernames.
func NewUsers(creds []config.UserCred) (*Users, error) {
	if len(creds) == 0 {
		return nil, fmt.Errorf("no users configured")
	}
	m := make(map[string]*Credential, len(creds))
	for _, c := range creds {
		name := strings.TrimSpace(c.Username)
		hash := strings.TrimSpace(c.Hash)
		if name == "" {
			return nil, fmt.Errorf("configured user has an empty username")
		}
		if hash == "" {
			return nil, fmt.Errorf("user %q has an empty password hash", name)
		}
		if !looksLikeBcrypt(hash) {
			return nil, fmt.Errorf("user %q: password hash is not a bcrypt hash", name)
		}
		if _, dup := m[name]; dup {
			return nil, fmt.Errorf("user %q is configured more than once", name)
		}
		m[name] = &Credential{Username: name, Hash: hash}
	}
	return &Users{byName: m}, nil
}

func looksLikeBcrypt(h string) bool {
	return strings.HasPrefix(h, "$2a$") || strings.HasPrefix(h, "$2b$") || strings.HasPrefix(h, "$2y$")
}

// Usernames returns the configured usernames (unordered).
func (u *Users) Usernames() []string {
	out := make([]string, 0, len(u.byName))
	for name := range u.byName {
		out = append(out, name)
	}
	return out
}

// SyncToDB upserts a users row for every configured username and records the
// resulting id back into the in-memory map. Re-running it does not create
// duplicate rows.
func (u *Users) SyncToDB(ctx context.Context, pool *pgxpool.Pool) error {
	for name, cred := range u.byName {
		var id string
		err := pool.QueryRow(ctx, `
			INSERT INTO users (username) VALUES ($1)
			ON CONFLICT (username) DO UPDATE SET username = EXCLUDED.username
			RETURNING id`, name).Scan(&id)
		if err != nil {
			return fmt.Errorf("upserting user %q: %w", name, err)
		}
		cred.UserID = id
	}
	return nil
}

// Verify checks a username/password pair. On success it returns the user's id
// and true. On an unknown username it still runs a bcrypt comparison against a
// placeholder hash so the response time does not reveal whether the username
// exists.
func (u *Users) Verify(username, password string) (userID string, ok bool) {
	cred, found := u.byName[username]
	hash := bcryptPlaceholderHash
	if found {
		hash = cred.Hash
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if !found || err != nil {
		return "", false
	}
	return cred.UserID, true
}
