package telegrambot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/openrouter"
)

// maxCount bounds how many images one prompt may generate.
const maxCount = 8

const countUsage = "count must be an integer between 1 and 8"

// aspectUsage and sizeUsage list the values the upstream API accepts.
var (
	aspectUsage = "aspect ratio must be one of: " + strings.Join(openrouter.AspectRatios, ", ")
	sizeUsage   = "image size must be one of: " + strings.Join(openrouter.ImageSizes, ", ")
)

// activeSettings is a snapshot of everything a chat generates with: taken
// under one lock, so every screen built from it is self-consistent.
type activeSettings struct {
	modelID string
	cfg     openrouter.ModelConfig
	aspect  string
	size    string
	count   int
}

// settings snapshots chatID's options, filling in the defaults for whatever
// it has not chosen.
func (b *Bot) settings(chatID int64) activeSettings {
	b.mu.Lock()
	s := b.chats[chatID]
	set := activeSettings{modelID: DefaultModel, count: DefaultCount}
	if s != nil {
		if s.hasModel {
			set.modelID = s.model
		}
		set.aspect, set.size = s.aspectRatio, s.imageSize
		if s.count > 0 {
			set.count = s.count
		}
	}
	b.mu.Unlock()

	cfg, known := openrouter.Model(set.modelID)
	if !known {
		// The active model dropped out of the catalog; fall back rather than
		// building screens around a model that cannot generate.
		set.modelID = DefaultModel
		cfg, _ = openrouter.Model(DefaultModel)
	}
	set.cfg = cfg
	return set
}

// resetSettings returns a chat to the defaults, leaving its authentication
// and admin authorization alone.
func (b *Bot) resetSettings(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.chats[chatID]
	if !ok {
		return // nothing selected, so nothing to reset
	}
	s.hasModel, s.model = false, ""
	s.aspectRatio, s.imageSize, s.count = "", "", 0
}

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

// setModel selects id for chatID, dropping any option the new model cannot
// use.
func (b *Bot) setModel(chatID int64, id string, cfg openrouter.ModelConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state(chatID)
	s.hasModel = true
	s.model = id
	if s.aspectRatio != "" && !cfg.SupportsAspectRatio {
		s.aspectRatio = ""
	}
	if s.imageSize != "" && !cfg.SupportsImageSize {
		s.imageSize = ""
	}
}

func parseCount(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > maxCount {
		return 0, false
	}
	return n, true
}

// replyWithKeyboard sends text with buttons attached, falling back to nothing
// more than a log line if Telegram refuses.
func (b *Bot) replyWithKeyboard(ctx context.Context, chatID int64, text string, kb keyboard) {
	if err := b.api.SendMessageWithKeyboard(ctx, chatID, text, kb); err != nil {
		log.Printf("telegrambot: sending message with keyboard to chat %d: %v", chatID, err)
	}
}

// cmdModels lists the catalog (ids included, so /model <id> stays usable) and
// attaches the picker to the last page.
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
	pages := paginate(lines, maxMessageLen)
	for _, page := range pages[:len(pages)-1] {
		b.reply(ctx, chatID, page)
	}
	b.replyWithKeyboard(ctx, chatID, pages[len(pages)-1], modelKeyboard(b.activeModel(chatID)))
}

func (b *Bot) cmdModel(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	id := strings.TrimSpace(args)
	if id == "" {
		set := b.settings(chatID)
		b.replyWithKeyboard(ctx, chatID, optionScreen("🎨 Pick a model", set), modelKeyboard(set.modelID))
		return
	}
	cfg, known := openrouter.Model(id)
	if !known {
		b.replyWithKeyboard(ctx, chatID, "unknown model: "+id, modelKeyboard(b.activeModel(chatID)))
		return
	}
	b.setModel(chatID, id, cfg)
	b.reply(ctx, chatID, "active model set to "+id)
}

func (b *Bot) cmdAspect(ctx context.Context, msg *models.Message, args string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	modelID := b.activeModel(chatID)
	cfg, _ := openrouter.Model(modelID)
	if !cfg.SupportsAspectRatio {
		b.reply(ctx, chatID, unsupported("aspect ratio", modelID))
		return
	}
	ratio, ok := openrouter.NormalizeAspectRatio(args)
	if ratio == "" && ok {
		set := b.settings(chatID)
		b.replyWithKeyboard(ctx, chatID, optionScreen("📐 Pick an aspect ratio", set), aspectKeyboard(set.aspect))
		return
	}
	if !ok {
		b.reply(ctx, chatID, aspectUsage)
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
	modelID := b.activeModel(chatID)
	cfg, _ := openrouter.Model(modelID)
	if !cfg.SupportsImageSize {
		b.reply(ctx, chatID, unsupported("image size", modelID))
		return
	}
	size, ok := openrouter.NormalizeImageSize(args)
	if size == "" && ok {
		set := b.settings(chatID)
		b.replyWithKeyboard(ctx, chatID, optionScreen("🖼 Pick an image size", set), sizeKeyboard(set.size))
		return
	}
	if !ok {
		b.reply(ctx, chatID, sizeUsage)
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
	if strings.TrimSpace(args) == "" {
		set := b.settings(chatID)
		b.replyWithKeyboard(ctx, chatID, optionScreen("🔢 How many images per prompt?", set), countKeyboard(set.count))
		return
	}
	n, ok := parseCount(args)
	if !ok {
		b.reply(ctx, chatID, countUsage)
		return
	}
	b.mu.Lock()
	b.state(chatID).count = n
	b.mu.Unlock()
	b.reply(ctx, chatID, fmt.Sprintf("count set to %d", n))
}
