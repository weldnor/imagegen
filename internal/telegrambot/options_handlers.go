package telegrambot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/openrouter"
)

func (b *Bot) activeModel(chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.chats[chatID]; ok && s.hasModel {
		return s.model
	}
	return DefaultModel
}

func (b *Bot) activeAspect(chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.chats[chatID]; ok {
		return s.aspectRatio
	}
	return ""
}

func (b *Bot) activeSize(chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.chats[chatID]; ok {
		return s.imageSize
	}
	return ""
}

func (b *Bot) activeCount(chatID int64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.chats[chatID]; ok && s.count > 0 {
		return s.count
	}
	return DefaultCount
}

func (b *Bot) cmdModels(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	ids := openrouter.KnownModels()
	lines := make([]string, 0, len(ids))
	for _, id := range ids {
		lines = append(lines, id+" — "+openrouter.Models[id].Name)
	}
	for _, page := range paginate(lines, maxMessageLen) {
		b.reply(ctx, chatID, page)
	}
}

func (b *Bot) cmdModel(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	id := strings.TrimSpace(args)
	if id == "" {
		b.reply(ctx, chatID, "active model: "+b.activeModel(chatID))
		return
	}
	cfg, known := openrouter.Model(id)
	if !known {
		b.reply(ctx, chatID, "unknown model: "+id)
		return
	}
	b.mu.Lock()
	s := b.state(chatID)
	s.hasModel = true
	s.model = id
	if s.aspectRatio != "" && !cfg.SupportsAspectRatio {
		s.aspectRatio = ""
	}
	if s.imageSize != "" && !cfg.SupportsImageSize {
		s.imageSize = ""
	}
	b.mu.Unlock()
	b.reply(ctx, chatID, "active model set to "+id)
}

func (b *Bot) cmdAspect(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	ratio := strings.TrimSpace(args)
	if ratio == "" {
		b.reply(ctx, chatID, "usage: /aspect <ratio>")
		return
	}
	modelID := b.activeModel(chatID)
	cfg, _ := openrouter.Model(modelID)
	if !cfg.SupportsAspectRatio {
		b.reply(ctx, chatID, "the active model ("+modelID+") does not support aspect ratio selection")
		return
	}
	b.mu.Lock()
	b.state(chatID).aspectRatio = ratio
	b.mu.Unlock()
	b.reply(ctx, chatID, "aspect ratio set to "+ratio)
}

func (b *Bot) cmdSize(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	size := strings.TrimSpace(args)
	if size == "" {
		b.reply(ctx, chatID, "usage: /size <size>")
		return
	}
	modelID := b.activeModel(chatID)
	cfg, _ := openrouter.Model(modelID)
	if !cfg.SupportsImageSize {
		b.reply(ctx, chatID, "the active model ("+modelID+") does not support image size selection")
		return
	}
	b.mu.Lock()
	b.state(chatID).imageSize = size
	b.mu.Unlock()
	b.reply(ctx, chatID, "image size set to "+size)
}

func (b *Bot) cmdCount(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil || n < 1 || n > 8 {
		b.reply(ctx, chatID, "count must be an integer between 1 and 8")
		return
	}
	b.mu.Lock()
	b.state(chatID).count = n
	b.mu.Unlock()
	b.reply(ctx, chatID, fmt.Sprintf("count set to %d", n))
}
