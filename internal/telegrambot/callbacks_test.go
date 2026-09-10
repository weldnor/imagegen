package telegrambot

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/openrouter"
)

// callbackUpdate builds the update Telegram sends when a button is tapped.
func callbackUpdate(chatID int64, messageID int, data string) *models.Update {
	return &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "query-1",
		Data: data,
		Message: models.MaybeInaccessibleMessage{
			Type: models.MaybeInaccessibleMessageTypeMessage,
			Message: &models.Message{
				ID:   messageID,
				Chat: models.Chat{ID: chatID, Type: models.ChatTypePrivate},
			},
		},
	}}
}

// press dispatches one button press on a text message, the way the polling
// loop does.
func (tb *testBot) press(t *testing.T, chatID int64, data string) {
	t.Helper()
	tb.bot.handleUpdate(t.Context(), callbackUpdate(chatID, 42, data))
}

// pressOnPhoto dispatches a press on a button hanging under an image.
func (tb *testBot) pressOnPhoto(t *testing.T, chatID int64, data string) {
	t.Helper()
	update := callbackUpdate(chatID, 42, data)
	update.CallbackQuery.Message.Message.Photo = []models.PhotoSize{{FileID: "f1", Width: 1, Height: 1}}
	tb.bot.handleUpdate(t.Context(), update)
}

// allKeyboards returns every keyboard the bot can send, so the checks below
// cover the whole button surface.
func allKeyboards() []keyboard {
	set := activeSettings{modelID: DefaultModel, count: DefaultCount}
	set.cfg, _ = openrouter.Model(DefaultModel)
	return []keyboard{
		mainMenuKeyboard(),
		settingsKeyboard(set),
		modelKeyboard(DefaultModel),
		aspectKeyboard("1:1"),
		sizeKeyboard("1K"),
		countKeyboard(1),
		galleryKeyboard(false),
		galleryKeyboard(true),
		browseKeyboard("11111111-2222-3333-4444-555555555555", 0, 3),
		generatedImageKeyboard("11111111-2222-3333-4444-555555555555"),
		confirmClearKeyboard(),
		backOnlyKeyboard(),
	}
}

// Every button must carry callback data Telegram accepts (1-64 bytes) and a
// prefix some handler is registered for, or the button would do nothing.
func TestEveryButtonIsRoutableAndWithinLimits(t *testing.T) {
	for _, kb := range allKeyboards() {
		for _, row := range kb.InlineKeyboard {
			for _, btn := range row {
				if btn.Text == "" {
					t.Errorf("button with data %q has no label", btn.CallbackData)
				}
				if n := len(btn.CallbackData); n == 0 || n > 64 {
					t.Errorf("button %q has %d bytes of callback data; Telegram allows 1-64", btn.Text, n)
				}
				prefix, _, _ := strings.Cut(btn.CallbackData, ":")
				if _, ok := callbackHandlers[prefix]; !ok {
					t.Errorf("button %q uses unrouted prefix %q", btn.Text, prefix)
				}
			}
		}
	}
}

// Covers the option buttons: each applies to chat state, reports what it did,
// and re-renders the screen with the new selection marked.
func TestOptionButtonsApplyAndRerender(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	tb.press(t, 1, cbPrefixModel+":black-forest-labs/flux.2-pro")
	if got := tb.bot.activeModel(1); got != "black-forest-labs/flux.2-pro" {
		t.Errorf("activeModel = %q", got)
	}
	if got := tb.api.lastAnswer(); !strings.Contains(got, "Flux 2 Pro") {
		t.Errorf("toast = %q", got)
	}

	tb.press(t, 1, cbPrefixAspect+":16:9")
	if got := tb.bot.activeAspect(1); got != "16:9" {
		t.Errorf("activeAspect = %q", got)
	}

	tb.press(t, 1, cbPrefixCount+":4")
	if got := tb.bot.activeCount(1); got != 4 {
		t.Errorf("activeCount = %d", got)
	}

	edit, ok := tb.api.lastEdit(1)
	if !ok {
		t.Fatal("no menu message was edited")
	}
	if edit.messageID != 42 {
		t.Errorf("edited message %d, want the one the button came from", edit.messageID)
	}
	if !strings.Contains(edit.text, "16:9") || !strings.Contains(edit.text, "Flux 2 Pro") {
		t.Errorf("menu text does not reflect the selection: %q", edit.text)
	}
	if !hasButton(edit.keyboard, "✅ 4") {
		t.Errorf("count keyboard does not mark the selection: %+v", edit.keyboard)
	}
}

func hasButton(kb keyboard, text string) bool {
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.Text == text {
				return true
			}
		}
	}
	return false
}

// A model that cannot use an option must not offer it, and must refuse it if
// a stale button is pressed anyway.
func TestUnsupportedOptionButtons(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.press(t, 1, cbPrefixModel+":black-forest-labs/flux.2-pro") // no image-size support

	for _, btn := range settingsKeyboard(tb.bot.settings(1)).InlineKeyboard {
		if strings.HasPrefix(btn[0].Text, "🖼 Image size") {
			t.Error("settings offers image size for a model that does not support it")
		}
	}

	tb.press(t, 1, cbPrefixSize+":2048x2048")
	if got := tb.api.lastAnswer(); !strings.Contains(got, "does not support") {
		t.Errorf("toast = %q", got)
	}
	if got := tb.bot.activeSize(1); got != "" {
		t.Errorf("activeSize = %q, want it left unset", got)
	}
}

// Buttons are as authenticated as commands: a stale menu in a logged-out chat
// changes nothing.
func TestButtonsRequireAuth(t *testing.T) {
	tb := newTestBot()

	tb.press(t, 1, cbPrefixModel+":black-forest-labs/flux.2-pro")

	if got := tb.api.lastAnswer(); !strings.Contains(got, "/login") {
		t.Errorf("toast = %q, want a login prompt", got)
	}
	if got := tb.bot.activeModel(1); got != DefaultModel {
		t.Errorf("activeModel = %q, want it unchanged", got)
	}
	if _, edited := tb.api.lastEdit(1); edited {
		t.Error("an unauthenticated press edited the menu")
	}
}

// Covers menu navigation: every screen answers the query and rewrites the
// same message, and Close removes it.
func TestMenuNavigation(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	for _, data := range []string{cbMenuSettings, cbMenuModel, cbMenuAspect, cbMenuSize, cbMenuCount, cbMenuHelp, cbMenuMain} {
		tb.press(t, 1, data)
		edit, ok := tb.api.lastEdit(1)
		if !ok {
			t.Fatalf("%s did not edit the menu", data)
		}
		if len(edit.keyboard.InlineKeyboard) == 0 {
			t.Errorf("%s left the message without buttons", data)
		}
	}

	before := len(tb.api.deleted)
	tb.press(t, 1, cbMenuClose)
	if len(tb.api.deleted) != before+1 {
		t.Error("Close did not remove the menu message")
	}
	if len(tb.api.answers) == 0 {
		t.Error("Close did not answer the callback query")
	}
}

// The settings submenu shows the values on the buttons that change them, and
// Reset puts every one of them back to its default.
func TestSettingsSubmenuAndReset(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.press(t, 1, cbPrefixAspect+":16:9")
	tb.press(t, 1, cbPrefixCount+":4")

	tb.press(t, 1, cbMenuSettings)
	edit, ok := tb.api.lastEdit(1)
	if !ok {
		t.Fatal("Settings did not edit the menu")
	}
	if !hasButton(edit.keyboard, "📐 Aspect ratio · 16:9") || !hasButton(edit.keyboard, "🔢 Images per prompt · 4") {
		t.Errorf("settings buttons do not carry their values: %+v", edit.keyboard)
	}

	tb.press(t, 1, cbMenuReset)
	if got := tb.bot.activeAspect(1); got != "" {
		t.Errorf("aspect after reset = %q", got)
	}
	if got := tb.bot.activeCount(1); got != DefaultCount {
		t.Errorf("count after reset = %d", got)
	}
	if got := tb.api.lastAnswer(); !strings.Contains(got, "reset") {
		t.Errorf("toast = %q", got)
	}
}

// The "auto" button of the aspect-ratio and image-size pickers clears the
// option instead of failing validation.
func TestAutoButtonClearsAnOption(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.press(t, 1, cbPrefixAspect+":16:9")

	tb.press(t, 1, cbPrefixAspect+":auto")
	if got := tb.bot.activeAspect(1); got != "" {
		t.Errorf("aspect after auto = %q, want it unset", got)
	}
	if got := tb.api.lastAnswer(); !strings.Contains(got, "auto") {
		t.Errorf("toast = %q", got)
	}
}

// An unrecognized callback (an old button after a redeploy) is acknowledged
// rather than left spinning.
func TestUnknownCallbackIsAcknowledged(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.press(t, 1, "nosuchprefix:whatever")
	if len(tb.api.answers) != 1 {
		t.Errorf("answered %d queries, want 1", len(tb.api.answers))
	}
}

// The pickers are ordered for the eye, not by the catalog, so this guards the
// thing that ordering can break: a value the API accepts having no button.
func TestPickersOfferEveryValue(t *testing.T) {
	kb := aspectKeyboard("")
	for _, r := range openrouter.AspectRatios {
		if !hasButton(kb, r) && !hasButton(kb, "✅ "+r) {
			t.Errorf("the aspect picker has no button for %q", r)
		}
	}
	if !hasButton(kb, "✅ auto") {
		t.Error("the aspect picker cannot clear the ratio")
	}

	sizes := sizeKeyboard("")
	for _, size := range openrouter.ImageSizes {
		if !hasButton(sizes, size) && !hasButton(sizes, "✅ "+size) {
			t.Errorf("the size picker has no button for %q", size)
		}
	}
	if !hasButton(sizes, "✅ auto") {
		t.Error("the size picker cannot clear the size")
	}
}
