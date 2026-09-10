package telegrambot

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
)

// ---- fake API ----

type sentMessage struct {
	chatID int64
	text   string
}

type sentPhoto struct {
	chatID   int64
	data     []byte
	filename string
	caption  string
}

type fakeAPI struct {
	mu sync.Mutex

	messages []sentMessage
	photos   []sentPhoto
	deleted  []int64 // chat ids for which DeleteMessage was called

	downloadFn func(fileID string) ([]byte, error)
}

func (f *fakeAPI) SendMessage(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, sentMessage{chatID: chatID, text: text})
	return nil
}

func (f *fakeAPI) SendPhoto(_ context.Context, chatID int64, data []byte, filename, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.photos = append(f.photos, sentPhoto{chatID: chatID, data: data, filename: filename, caption: caption})
	return nil
}

func (f *fakeAPI) DeleteMessage(_ context.Context, chatID int64, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, chatID)
	return nil
}

func (f *fakeAPI) DownloadFile(_ context.Context, fileID string) ([]byte, error) {
	if f.downloadFn != nil {
		return f.downloadFn(fileID)
	}
	return []byte{0xFF, 0xD8, 0xFF}, nil // minimal JPEG-ish header
}

func (f *fakeAPI) lastText(chatID int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var last string
	for _, m := range f.messages {
		if m.chatID == chatID {
			last = m.text
		}
	}
	return last
}

func (f *fakeAPI) allText(chatID int64) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.messages {
		if m.chatID == chatID {
			out = append(out, m.text)
		}
	}
	return out
}

// ---- fake Generator ----

type fakeGen struct {
	fn func(p openrouter.GenerateParams) (*openrouter.Image, error)

	mu    sync.Mutex
	calls []openrouter.GenerateParams
}

func (f *fakeGen) Generate(_ context.Context, p openrouter.GenerateParams) (*openrouter.Image, error) {
	f.mu.Lock()
	f.calls = append(f.calls, p)
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(p)
	}
	return &openrouter.Image{Data: []byte("img"), ContentType: "image/png"}, nil
}

func okImage(openrouter.GenerateParams) (*openrouter.Image, error) {
	return &openrouter.Image{Data: []byte("img"), ContentType: "image/png"}, nil
}

// ---- fake GalleryStore ----

type fakeGallery struct {
	mu     sync.Mutex
	images map[string]gallery.Image // by id
	nextID int
	saved  []gallery.Metadata
}

func newFakeGallery() *fakeGallery {
	return &fakeGallery{images: map[string]gallery.Image{}}
}

func (g *fakeGallery) Save(_ context.Context, userID string, data []byte, meta gallery.Metadata) (gallery.Image, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	id := "img-" + strconv.Itoa(g.nextID)
	img := gallery.Image{ID: id, UserID: userID, Created: time.Now(), Metadata: meta}
	g.images[id] = img
	g.saved = append(g.saved, meta)
	_ = data
	return img, nil
}

func (g *fakeGallery) List(_ context.Context, userID string) ([]gallery.Image, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []gallery.Image
	for _, img := range g.images {
		if img.UserID == userID {
			out = append(out, img)
		}
	}
	return out, nil
}

func (g *fakeGallery) Get(_ context.Context, userID, imageID string) (gallery.Image, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	img, ok := g.images[imageID]
	if !ok || img.UserID != userID {
		return gallery.Image{}, gallery.ErrNotFound
	}
	return img, nil
}

func (g *fakeGallery) Delete(_ context.Context, userID, imageID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	img, ok := g.images[imageID]
	if !ok || img.UserID != userID {
		return gallery.ErrNotFound
	}
	delete(g.images, imageID)
	return nil
}

func (g *fakeGallery) ClearForUser(_ context.Context, userID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, img := range g.images {
		if img.UserID == userID {
			delete(g.images, id)
		}
	}
	return nil
}

// ---- fake UserStore ----

type fakeUser struct {
	id          string
	username    string
	password    string
	telegramIDs map[int64]bool
}

type fakeUsers struct {
	mu     sync.Mutex
	byName map[string]*fakeUser
	byTgID map[int64]*fakeUser
	nextID int
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byName: map[string]*fakeUser{}, byTgID: map[int64]*fakeUser{}}
}

func (u *fakeUsers) addUser(username, password string, tgIDs ...int64) *fakeUser {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.nextID++
	usr := &fakeUser{id: "uid" + strconv.Itoa(u.nextID), username: username, password: password, telegramIDs: map[int64]bool{}}
	u.byName[username] = usr
	for _, id := range tgIDs {
		usr.telegramIDs[id] = true
		u.byTgID[id] = usr
	}
	return usr
}

func (u *fakeUsers) VerifyPassword(_ context.Context, username, password string) (string, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	usr, ok := u.byName[username]
	if !ok || usr.password != password {
		return "", false, nil
	}
	return usr.id, true, nil
}

func (u *fakeUsers) LookupByTelegramID(_ context.Context, telegramID int64) (string, string, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	usr, ok := u.byTgID[telegramID]
	if !ok {
		return "", "", false, nil
	}
	return usr.id, usr.username, true, nil
}

func (u *fakeUsers) CreateUser(_ context.Context, username, password string, telegramIDs []int64) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, exists := u.byName[username]; exists {
		return "", auth.ErrUsernameTaken
	}
	for _, id := range telegramIDs {
		if _, exists := u.byTgID[id]; exists {
			return "", auth.ErrTelegramIDLinked
		}
	}
	u.nextID++
	usr := &fakeUser{id: "uid" + strconv.Itoa(u.nextID), username: username, password: password, telegramIDs: map[int64]bool{}}
	u.byName[username] = usr
	for _, id := range telegramIDs {
		usr.telegramIDs[id] = true
		u.byTgID[id] = usr
	}
	return usr.id, nil
}

func (u *fakeUsers) AddTelegramID(_ context.Context, username string, telegramID int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	usr, ok := u.byName[username]
	if !ok {
		return auth.ErrUserNotFound
	}
	if _, exists := u.byTgID[telegramID]; exists {
		return auth.ErrTelegramIDLinked
	}
	usr.telegramIDs[telegramID] = true
	u.byTgID[telegramID] = usr
	return nil
}

func (u *fakeUsers) ListUsers(_ context.Context) ([]auth.UserSummary, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []auth.UserSummary
	for _, usr := range u.byName {
		s := auth.UserSummary{Username: usr.username}
		for id := range usr.telegramIDs {
			s.TelegramIDs = append(s.TelegramIDs, id)
		}
		out = append(out, s)
	}
	return out, nil
}

// ---- fake BindingStore ----

type fakeBindings struct {
	mu   sync.Mutex
	byID map[int64]auth.TelegramBinding
}

func newFakeBindings() *fakeBindings {
	return &fakeBindings{byID: map[int64]auth.TelegramBinding{}}
}

func (bs *fakeBindings) Create(_ context.Context, chatID int64, userID string) (auth.TelegramBinding, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	b := auth.TelegramBinding{ChatID: chatID, UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}
	bs.byID[chatID] = b
	return b, nil
}

func (bs *fakeBindings) Lookup(_ context.Context, chatID int64) (auth.TelegramBinding, bool, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	b, ok := bs.byID[chatID]
	if !ok {
		return auth.TelegramBinding{}, false, nil
	}
	if time.Now().After(b.ExpiresAt) {
		delete(bs.byID, chatID)
		return auth.TelegramBinding{}, false, nil
	}
	return b, true, nil
}

func (bs *fakeBindings) Delete(_ context.Context, chatID int64) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	delete(bs.byID, chatID)
	return nil
}

// ---- setup ----

type testBot struct {
	bot      *Bot
	api      *fakeAPI
	gen      *fakeGen
	gallery  *fakeGallery
	users    *fakeUsers
	bindings *fakeBindings
}

func newTestBot() *testBot {
	tb := &testBot{
		api:      &fakeAPI{},
		gen:      &fakeGen{fn: okImage},
		gallery:  newFakeGallery(),
		users:    newFakeUsers(),
		bindings: newFakeBindings(),
	}
	tb.bot = newBot(tb.api, Config{AdminPassword: "adminpw", AdminTTL: time.Hour}, tb.gen, tb.gallery, tb.users, tb.bindings)
	return tb
}

func textMessage(chatID int64, text string) *models.Message {
	return &models.Message{ID: 1, Chat: models.Chat{ID: chatID}, Text: text}
}
