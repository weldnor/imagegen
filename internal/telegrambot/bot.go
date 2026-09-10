// Package telegrambot implements a Telegram bot front-end for Imagen: chat
// authentication (linked Telegram ID or temporary /login), admin-gated user
// management, model/option selection, image generation, and gallery browsing
// — all driven directly against the same collaborators the HTTP API uses.
package telegrambot

import (
	"context"
	"log"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
)

// Generator produces one image from a generation request. Satisfied by
// *openrouter.Client; tests inject a fake.
type Generator interface {
	Generate(ctx context.Context, p openrouter.GenerateParams) (*openrouter.Image, error)
}

// GalleryStore is the subset of *gallery.Store the bot uses.
type GalleryStore interface {
	Save(ctx context.Context, userID string, data []byte, meta gallery.Metadata) (gallery.Image, error)
	List(ctx context.Context, userID string) ([]gallery.Image, error)
	Get(ctx context.Context, userID, imageID string) (gallery.Image, error)
	Delete(ctx context.Context, userID, imageID string) error
	ClearForUser(ctx context.Context, userID string) error
}

// UserStore is the subset of *auth.Users the bot uses for authentication and
// admin user-management commands.
type UserStore interface {
	VerifyPassword(ctx context.Context, username, password string) (userID string, ok bool, err error)
	LookupByTelegramID(ctx context.Context, telegramID int64) (userID, username string, ok bool, err error)
	CreateUser(ctx context.Context, username, password string, telegramIDs []int64) (userID string, err error)
	AddTelegramID(ctx context.Context, username string, telegramID int64) error
	ListUsers(ctx context.Context) ([]auth.UserSummary, error)
}

// BindingStore is the subset of *auth.TelegramBindings the bot uses for its
// temporary /login sessions.
type BindingStore interface {
	Create(ctx context.Context, chatID int64, userID string) (auth.TelegramBinding, error)
	Lookup(ctx context.Context, chatID int64) (auth.TelegramBinding, bool, error)
	Delete(ctx context.Context, chatID int64) error
}

// chatState is one chat's ephemeral (in-memory) state: admin authorization and
// selected generation options. Lost on restart by design (see design.md).
type chatState struct {
	adminUntil time.Time // zero if not admin-authorized

	hasModel    bool
	model       string
	aspectRatio string
	imageSize   string
	count       int
}

// DefaultModel is used for a chat that has not yet picked one with /model.
const DefaultModel = "google/gemini-2.5-flash-image"

// DefaultCount is the generation count for a chat that has not set one.
const DefaultCount = 1

// Config configures a Bot.
type Config struct {
	Token         string
	AdminPassword string
	// AdminTTL bounds how long a chat's /admin authorization lasts.
	AdminTTL time.Duration
	// Concurrency bounds how many generations of one /generate run
	// concurrently. Defaults to 4.
	Concurrency int
}

// Bot holds the collaborators and per-chat state for the Telegram front-end.
type Bot struct {
	api      API
	gen      Generator
	gallery  GalleryStore
	users    UserStore
	bindings BindingStore

	adminPassword string
	adminTTL      time.Duration

	concurrency int

	loginLimiter *auth.RateLimiter
	adminLimiter *auth.RateLimiter

	mu    sync.Mutex
	chats map[int64]*chatState

	client interface{ Start(ctx context.Context) } // set only by NewBot; nil in most tests
}

// NewBot builds a production Bot whose transport is the real Telegram Bot
// API. Construction never calls Telegram (a bad token only surfaces once
// polling starts).
func NewBot(cfg Config, gen Generator, gal GalleryStore, users UserStore, bindings BindingStore) (*Bot, error) {
	b := newBot(nil, cfg, gen, gal, users, bindings)
	client, err := newTelegramClient(cfg.Token, func(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
		b.handleUpdateSafely(ctx, update)
	})
	if err != nil {
		return nil, err
	}
	b.api = client
	b.client = client
	return b, nil
}

// newBot builds a Bot around an already-constructed API (a fake in tests, the
// production telegramClient in NewBot).
func newBot(api API, cfg Config, gen Generator, gal GalleryStore, users UserStore, bindings BindingStore) *Bot {
	ttl := cfg.AdminTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	return &Bot{
		api:           api,
		gen:           gen,
		gallery:       gal,
		users:         users,
		bindings:      bindings,
		adminPassword: cfg.AdminPassword,
		adminTTL:      ttl,
		concurrency:   concurrency,
		loginLimiter:  auth.NewRateLimiter(10, 5*time.Minute),
		adminLimiter:  auth.NewRateLimiter(10, 5*time.Minute),
		chats:         make(map[int64]*chatState),
	}
}

// Start runs the polling loop until ctx is canceled. It is a no-op if the Bot
// was built without a real transport (e.g. via newBot directly in a test).
func (b *Bot) Start(ctx context.Context) {
	if b.client == nil {
		return
	}
	b.client.Start(ctx)
}

// handleUpdateSafely dispatches update, recovering from any panic so a bug in
// one handler cannot take down the process (or the long-polling loop).
func (b *Bot) handleUpdateSafely(ctx context.Context, update *models.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("telegrambot: recovered panic handling update: %v", r)
		}
	}()
	b.handleUpdate(ctx, update)
}

// handleUpdate routes one update to the matching command handler.
func (b *Bot) handleUpdate(ctx context.Context, update *models.Update) {
	msg := update.Message
	if msg == nil {
		return
	}
	cmd, args := route(msg)
	if cmd == "" {
		return
	}
	h, ok := commandHandlers[cmd]
	if !ok {
		return
	}
	h(b, ctx, msg, args)
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if err := b.api.SendMessage(ctx, chatID, text); err != nil {
		log.Printf("telegrambot: sending message to chat %d: %v", chatID, err)
	}
}

// state returns (creating if necessary) the ephemeral state for chatID. Callers
// must hold b.mu.
func (b *Bot) state(chatID int64) *chatState {
	s, ok := b.chats[chatID]
	if !ok {
		s = &chatState{}
		b.chats[chatID] = s
	}
	return s
}
