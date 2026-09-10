package telegrambot

import (
	"context"
	"strings"
	"testing"

	"github.com/weldnor/imagegen/internal/gallery"
)

// Covers 8.1: gallery listing, paged.
func TestCmdGalleryLists(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	usr, _ := tb.bot.authenticate(context.Background(), 1)

	for i := 0; i < 200; i++ {
		tb.gallery.Save(context.Background(), usr.userID, []byte("x"), gallery.Metadata{
			Prompt: strings.Repeat("a very long prompt to force paging ", 3), Model: "m", ModelName: "M",
		})
	}

	tb.bot.cmdGallery(context.Background(), textMessage(1, ""), "")
	if len(tb.api.allText(1)) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(tb.api.allText(1)))
	}
}

func TestCmdGalleryEmpty(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdGallery(context.Background(), textMessage(1, ""), "")
	if got := tb.api.lastText(1); !strings.Contains(got, "empty") {
		t.Errorf("reply = %q", got)
	}
}

// Covers 8.2: deleting an owned, a not-owned/nonexistent image.
func TestCmdDelete(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	usr, _ := tb.bot.authenticate(context.Background(), 1)
	img, _ := tb.gallery.Save(context.Background(), usr.userID, []byte("x"), gallery.Metadata{Prompt: "p", Model: "m"})

	tb.bot.cmdDelete(context.Background(), textMessage(1, ""), img.ID)
	if got := tb.api.lastText(1); !strings.Contains(got, "deleted") {
		t.Errorf("reply = %q", got)
	}

	tb.bot.cmdDelete(context.Background(), textMessage(1, ""), "does-not-exist")
	if got := tb.api.lastText(1); !strings.Contains(got, "not found") {
		t.Errorf("reply = %q", got)
	}

	// Not owned: created for a different user.
	other, _ := tb.gallery.Save(context.Background(), "someone-else", []byte("x"), gallery.Metadata{Prompt: "p", Model: "m"})
	tb.bot.cmdDelete(context.Background(), textMessage(1, ""), other.ID)
	if got := tb.api.lastText(1); !strings.Contains(got, "not found") {
		t.Errorf("reply = %q", got)
	}
}

// Covers 8.3: clearing the gallery reports the count removed.
func TestCmdClear(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	usr, _ := tb.bot.authenticate(context.Background(), 1)
	for i := 0; i < 3; i++ {
		tb.gallery.Save(context.Background(), usr.userID, []byte("x"), gallery.Metadata{Prompt: "p", Model: "m"})
	}

	tb.bot.cmdClear(context.Background(), textMessage(1, ""), "")

	if got := tb.api.lastText(1); !strings.Contains(got, "removed 3") {
		t.Errorf("reply = %q", got)
	}
	imgs, _ := tb.gallery.List(context.Background(), usr.userID)
	if len(imgs) != 0 {
		t.Errorf("gallery still has %d images after clear", len(imgs))
	}
}
