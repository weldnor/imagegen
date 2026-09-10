package telegrambot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/gallery"
)

func (b *Bot) cmdGallery(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	authCtx, ok := b.requireAuth(ctx, chatID)
	if !ok {
		return
	}
	imgs, err := b.gallery.List(ctx, authCtx.userID)
	if err != nil {
		b.reply(ctx, chatID, "could not list the gallery")
		return
	}
	if len(imgs) == 0 {
		b.reply(ctx, chatID, "the gallery is empty")
		return
	}
	lines := make([]string, 0, len(imgs))
	for _, img := range imgs {
		lines = append(lines, fmt.Sprintf("%s — %q (%s, %s)",
			img.ID, img.Prompt, img.ModelName, img.Created.Format("2006-01-02 15:04")))
	}
	for _, page := range paginate(lines, maxMessageLen) {
		b.reply(ctx, chatID, page)
	}
}

func (b *Bot) cmdDelete(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	authCtx, ok := b.requireAuth(ctx, chatID)
	if !ok {
		return
	}
	id := strings.TrimSpace(args)
	if id == "" {
		b.reply(ctx, chatID, "usage: /delete <id>")
		return
	}
	if err := b.gallery.Delete(ctx, authCtx.userID, id); err != nil {
		if errors.Is(err, gallery.ErrNotFound) {
			b.reply(ctx, chatID, "image not found")
			return
		}
		b.reply(ctx, chatID, "could not delete image")
		return
	}
	b.reply(ctx, chatID, "deleted "+id)
}

func (b *Bot) cmdClear(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	authCtx, ok := b.requireAuth(ctx, chatID)
	if !ok {
		return
	}
	imgs, err := b.gallery.List(ctx, authCtx.userID)
	if err != nil {
		b.reply(ctx, chatID, "could not clear the gallery")
		return
	}
	if err := b.gallery.ClearForUser(ctx, authCtx.userID); err != nil {
		b.reply(ctx, chatID, "could not clear the gallery")
		return
	}
	b.reply(ctx, chatID, fmt.Sprintf("removed %d image(s)", len(imgs)))
}
