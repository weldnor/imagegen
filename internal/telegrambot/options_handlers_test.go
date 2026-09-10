package telegrambot

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func loggedInBot(tb *testBot, chatID int64) {
	usr := tb.users.addUser("u", "pw")
	tb.bindings.Create(context.Background(), chatID, usr.id)
}

// Covers 6.1: concurrent access to per-chat option state is race-free.
func TestChatOptionsConcurrentAccess(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")
		}()
		go func() {
			defer wg.Done()
			_ = tb.bot.activeModel(1)
		}()
	}
	wg.Wait()

	if got := tb.bot.activeModel(1); got != "google/gemini-2.5-flash-image" {
		t.Errorf("activeModel = %q", got)
	}
}

func TestDefaultModelWhenUnset(t *testing.T) {
	tb := newTestBot()
	if got := tb.bot.activeModel(999); got != DefaultModel {
		t.Errorf("activeModel(unset) = %q, want default %q", got, DefaultModel)
	}
}

// Covers 6.2: /models and /model, known and unknown ids.
func TestCmdModel(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")
	if got := tb.bot.activeModel(1); got != "google/gemini-2.5-flash-image" {
		t.Errorf("activeModel = %q", got)
	}

	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "not/a-model")
	if got := tb.api.lastText(1); !strings.Contains(got, "unknown model") {
		t.Errorf("reply = %q", got)
	}
	// Previous selection unchanged.
	if got := tb.bot.activeModel(1); got != "google/gemini-2.5-flash-image" {
		t.Errorf("activeModel after unknown selection = %q", got)
	}
}

func TestCmdModels(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModels(context.Background(), textMessage(1, ""), "")
	if got := tb.api.lastText(1); !strings.Contains(got, "google/gemini-2.5-flash-image") {
		t.Errorf("reply = %q, want it to list known models", got)
	}
}

// Covers 6.3: aspect/size/count validated against the active model.
func TestCmdAspectSizeCount(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	// google/gemini-2.5-flash-image supports both aspect ratio and image size.
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")

	tb.bot.cmdAspect(context.Background(), textMessage(1, ""), "16:9")
	if got := tb.bot.activeAspect(1); got != "16:9" {
		t.Errorf("activeAspect = %q", got)
	}

	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "2K")
	if got := tb.bot.activeSize(1); got != "2K" {
		t.Errorf("activeSize = %q", got)
	}

	tb.bot.cmdCount(context.Background(), textMessage(1, ""), "5")
	if got := tb.bot.activeCount(1); got != 5 {
		t.Errorf("activeCount = %d", got)
	}

	// Unsupported model for aspect ratio: black-forest-labs/flux.2-pro
	// supports aspect ratio but not image size.
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "black-forest-labs/flux.2-pro")
	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "2K")
	if got := tb.api.lastText(1); !strings.Contains(got, "does not support") {
		t.Errorf("reply = %q", got)
	}

	// Out-of-range count.
	tb.bot.cmdCount(context.Background(), textMessage(1, ""), "9")
	if got := tb.api.lastText(1); !strings.Contains(got, "between 1 and 8") {
		t.Errorf("reply = %q", got)
	}
	if got := tb.bot.activeCount(1); got != 5 {
		t.Errorf("activeCount after invalid input changed to %d", got)
	}
}

// The upstream API rejects anything outside its option lists, so a bad
// /aspect or /size argument is refused here rather than at generation time.
func TestCmdAspectSizeRejectInvalidValues(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")
	tb.bot.cmdAspect(context.Background(), textMessage(1, ""), "16:9")
	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "2K")

	tb.bot.cmdAspect(context.Background(), textMessage(1, ""), "16x9")
	if got := tb.api.lastText(1); !strings.Contains(got, "aspect ratio must be one of") {
		t.Errorf("reply = %q", got)
	}
	if got := tb.bot.activeAspect(1); got != "16:9" {
		t.Errorf("activeAspect changed to %q after invalid input", got)
	}

	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "1024")
	if got := tb.api.lastText(1); !strings.Contains(got, "image size must be one of") {
		t.Errorf("reply = %q", got)
	}
	if got := tb.bot.activeSize(1); got != "2K" {
		t.Errorf("activeSize changed to %q after invalid input", got)
	}

	// Valid values are canonicalised, so "1k" is stored as the API's "1K".
	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "1k")
	if got := tb.bot.activeSize(1); got != "1K" {
		t.Errorf("activeSize = %q, want the canonical 1K", got)
	}
}

// Covers 6.4: switching to a model that doesn't support a set option clears it.
func TestSwitchingModelClearsUnsupportedOptions(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")
	tb.bot.cmdAspect(context.Background(), textMessage(1, ""), "16:9")
	if got := tb.bot.activeAspect(1); got != "16:9" {
		t.Fatalf("precondition: activeAspect = %q", got)
	}

	// bytedance-seed/seedream-4.5 does not support aspect ratio... actually it
	// does; use a model that truly lacks SupportsAspectRatio. All current
	// models support aspect ratio, so instead verify image size clearing,
	// which flux.2-pro lacks.
	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "2K")
	if got := tb.bot.activeSize(1); got != "2K" {
		t.Fatalf("precondition: activeSize = %q", got)
	}

	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "black-forest-labs/flux.2-pro")
	if got := tb.bot.activeSize(1); got != "" {
		t.Errorf("activeSize not cleared after switching to a model without SupportsImageSize: %q", got)
	}
	// Aspect ratio remained set since flux.2-pro still supports it.
	if got := tb.bot.activeAspect(1); got != "16:9" {
		t.Errorf("activeAspect unexpectedly cleared: %q", got)
	}
}
