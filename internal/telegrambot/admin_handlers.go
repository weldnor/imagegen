package telegrambot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/auth"
)

func (b *Bot) isAdmin(chatID int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.chats[chatID]
	return ok && !s.adminUntil.IsZero() && time.Now().Before(s.adminUntil)
}

// requireAdmin replies and returns false if chatID is not currently
// admin-authorized.
func (b *Bot) requireAdmin(ctx context.Context, chatID int64) bool {
	if !b.isAdmin(chatID) {
		b.reply(ctx, chatID, "please /admin <password> first")
		return false
	}
	return true
}

func (b *Bot) cmdAdmin(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	defer b.deleteMessage(ctx, chatID, msg.ID)

	key := strconv.FormatInt(chatID, 10)
	if !b.adminLimiter.Allowed(key) {
		b.reply(ctx, chatID, "too many failed attempts; try again later")
		return
	}
	if args == "" || args != b.adminPassword {
		b.adminLimiter.RecordFailure(key)
		b.reply(ctx, chatID, "incorrect admin password")
		return
	}
	b.adminLimiter.Reset(key)

	b.mu.Lock()
	b.state(chatID).adminUntil = time.Now().Add(b.adminTTL)
	b.mu.Unlock()
	b.reply(ctx, chatID, "admin authorized")
}

func (b *Bot) cmdAddUser(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if !b.requireAdmin(ctx, chatID) {
		return
	}
	fields := strings.Fields(args)
	if len(fields) < 2 {
		b.reply(ctx, chatID, "usage: /adduser <username> <password> [telegram_id...]")
		return
	}
	username, password := fields[0], fields[1]

	tgIDs := make([]int64, 0, len(fields)-2)
	for _, f := range fields[2:] {
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			b.reply(ctx, chatID, "invalid telegram id: "+f)
			return
		}
		tgIDs = append(tgIDs, id)
	}

	if _, err := b.users.CreateUser(ctx, username, password, tgIDs); err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			b.reply(ctx, chatID, "username "+username+" is already taken")
		case errors.Is(err, auth.ErrTelegramIDLinked):
			b.reply(ctx, chatID, "one of the given telegram ids is already linked to another user")
		default:
			b.reply(ctx, chatID, "could not create user")
		}
		return
	}
	b.reply(ctx, chatID, "created user "+username)
}

func (b *Bot) cmdAddTelegramID(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if !b.requireAdmin(ctx, chatID) {
		return
	}
	fields := strings.Fields(args)
	if len(fields) != 2 {
		b.reply(ctx, chatID, "usage: /addtelegramid <username> <telegram_id>")
		return
	}
	username := fields[0]
	id, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		b.reply(ctx, chatID, "invalid telegram id: "+fields[1])
		return
	}

	switch err := b.users.AddTelegramID(ctx, username, id); {
	case err == nil:
		b.reply(ctx, chatID, "linked telegram id to "+username)
	case errors.Is(err, auth.ErrUserNotFound):
		b.reply(ctx, chatID, "user "+username+" not found")
	case errors.Is(err, auth.ErrTelegramIDLinked):
		b.reply(ctx, chatID, "that telegram id is already linked to a user")
	default:
		b.reply(ctx, chatID, "could not link telegram id")
	}
}

func (b *Bot) cmdListUsers(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if !b.requireAdmin(ctx, chatID) {
		return
	}
	users, err := b.users.ListUsers(ctx)
	if err != nil {
		b.reply(ctx, chatID, "could not list users")
		return
	}
	if len(users) == 0 {
		b.reply(ctx, chatID, "no users configured")
		return
	}
	lines := make([]string, 0, len(users))
	for _, u := range users {
		line := u.Username
		if len(u.TelegramIDs) > 0 {
			ids := make([]string, len(u.TelegramIDs))
			for i, id := range u.TelegramIDs {
				ids[i] = strconv.FormatInt(id, 10)
			}
			line += " (" + strings.Join(ids, ", ") + ")"
		}
		lines = append(lines, line)
	}
	for _, page := range paginate(lines, maxMessageLen) {
		b.reply(ctx, chatID, page)
	}
}
