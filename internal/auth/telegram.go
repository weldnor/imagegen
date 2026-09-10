package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelegramBinding is a temporary chat -> user authentication created by the
// bot's /login command.
type TelegramBinding struct {
	ChatID    int64
	UserID    string
	Username  string
	ExpiresAt time.Time
}

// TelegramBindings persists temporary per-chat /login bindings in PostgreSQL.
type TelegramBindings struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

// NewTelegramBindings returns a store whose new bindings live for ttl.
func NewTelegramBindings(pool *pgxpool.Pool, ttl time.Duration) *TelegramBindings {
	return &TelegramBindings{pool: pool, ttl: ttl}
}

// Create replaces any existing binding for chatID with a fresh one for userID,
// expiring ttl from now.
func (b *TelegramBindings) Create(ctx context.Context, chatID int64, userID string) (TelegramBinding, error) {
	expires := time.Now().Add(b.ttl)
	if _, err := b.pool.Exec(ctx, `
		INSERT INTO telegram_bindings (chat_id, user_id, expires_at) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE SET user_id = EXCLUDED.user_id, expires_at = EXCLUDED.expires_at`,
		chatID, userID, expires); err != nil {
		return TelegramBinding{}, err
	}
	return TelegramBinding{ChatID: chatID, UserID: userID, ExpiresAt: expires}, nil
}

// Lookup returns the live binding for chatID, if any. An expired binding is
// treated as not found (and opportunistically deleted).
func (b *TelegramBindings) Lookup(ctx context.Context, chatID int64) (TelegramBinding, bool, error) {
	var bind TelegramBinding
	err := b.pool.QueryRow(ctx, `
		SELECT tb.chat_id, tb.user_id, u.username, tb.expires_at
		FROM telegram_bindings tb JOIN users u ON u.id = tb.user_id
		WHERE tb.chat_id = $1`, chatID).Scan(&bind.ChatID, &bind.UserID, &bind.Username, &bind.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TelegramBinding{}, false, nil
	}
	if err != nil {
		return TelegramBinding{}, false, err
	}
	if time.Now().After(bind.ExpiresAt) {
		_, _ = b.pool.Exec(ctx, `DELETE FROM telegram_bindings WHERE chat_id = $1`, chatID)
		return TelegramBinding{}, false, nil
	}
	return bind, true, nil
}

// Delete removes chatID's binding, if any. Deleting a non-existent binding is
// not an error.
func (b *TelegramBindings) Delete(ctx context.Context, chatID int64) error {
	_, err := b.pool.Exec(ctx, `DELETE FROM telegram_bindings WHERE chat_id = $1`, chatID)
	return err
}
