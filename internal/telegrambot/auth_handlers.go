package telegrambot

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
)

// authContext identifies the app user a chat is authenticated as.
type authContext struct {
	userID   string
	username string
}

// authenticate resolves a chat's authentication: a linked Telegram ID first
// (no expiry), then a live temporary /login binding, then unauthenticated.
func (b *Bot) authenticate(ctx context.Context, chatID int64) (authContext, bool) {
	if userID, username, ok, err := b.users.LookupByTelegramID(ctx, chatID); err == nil && ok {
		return authContext{userID: userID, username: username}, true
	}
	if bind, ok, err := b.bindings.Lookup(ctx, chatID); err == nil && ok {
		return authContext{userID: bind.UserID, username: bind.Username}, true
	}
	return authContext{}, false
}

// requireAuth authenticates chatID, replying and returning ok=false if it is
// neither linked nor logged in.
func (b *Bot) requireAuth(ctx context.Context, chatID int64) (authContext, bool) {
	a, ok := b.authenticate(ctx, chatID)
	if !ok {
		b.reply(ctx, chatID, "please /login <username> <password> first")
	}
	return a, ok
}

func (b *Bot) deleteMessage(ctx context.Context, chatID int64, messageID int) {
	_ = b.api.DeleteMessage(ctx, chatID, messageID) // best-effort; Telegram may refuse (e.g. too old)
}

func (b *Bot) cmdLogin(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	defer b.deleteMessage(ctx, chatID, msg.ID)

	parts := strings.Fields(args)
	if len(parts) != 2 {
		b.reply(ctx, chatID, "usage: /login <username> <password>")
		return
	}
	username, password := parts[0], parts[1]

	key := strconv.FormatInt(chatID, 10)
	if !b.loginLimiter.Allowed(key) {
		b.reply(ctx, chatID, "too many failed login attempts; try again later")
		return
	}

	userID, ok, err := b.users.VerifyPassword(ctx, username, password)
	if err != nil {
		b.reply(ctx, chatID, "login failed; please try again")
		return
	}
	if !ok {
		b.loginLimiter.RecordFailure(key)
		b.reply(ctx, chatID, "invalid username or password")
		return
	}
	b.loginLimiter.Reset(key)

	if _, err := b.bindings.Create(ctx, chatID, userID); err != nil {
		b.reply(ctx, chatID, "login failed; please try again")
		return
	}
	b.reply(ctx, chatID, "logged in as "+username)
}

func (b *Bot) cmdLogout(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID

	if _, _, ok, err := b.users.LookupByTelegramID(ctx, chatID); err == nil && ok {
		b.reply(ctx, chatID, "this chat is linked to an account; only an admin removing the link can end its access")
		return
	}

	_ = b.bindings.Delete(ctx, chatID)
	b.mu.Lock()
	if s, ok := b.chats[chatID]; ok {
		s.hasModel = false
		s.model = ""
		s.aspectRatio = ""
		s.imageSize = ""
		s.count = 0
	}
	b.mu.Unlock()
	b.reply(ctx, chatID, "logged out")
}
