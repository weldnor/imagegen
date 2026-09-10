package telegrambot

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

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

// ---- interactive gallery ----

// seedGallery stores n images for the chat's user, oldest first, and returns
// them newest-first (the order the browser walks).
func seedGallery(t *testing.T, tb *testBot, chatID int64, n int) []gallery.Image {
	t.Helper()
	usr, _ := tb.bot.authenticate(t.Context(), chatID)
	for i := 0; i < n; i++ {
		if _, err := tb.gallery.Save(t.Context(), usr.userID, []byte("bytes"), gallery.Metadata{
			Prompt: "prompt " + strconv.Itoa(i), Model: DefaultModel, ModelName: "M", ContentType: "image/png",
		}); err != nil {
			t.Fatalf("seeding gallery: %v", err)
		}
	}
	imgs, err := tb.gallery.List(t.Context(), usr.userID)
	if err != nil {
		t.Fatalf("listing gallery: %v", err)
	}
	return imgs
}

// Covers /browse: the newest image is sent with its navigation buttons.
func TestCmdBrowseSendsNewestImage(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	imgs := seedGallery(t, tb, 1, 3)

	tb.bot.cmdBrowse(t.Context(), textMessage(1, "/browse"), "")

	if len(tb.api.photos) != 1 {
		t.Fatalf("sent %d photos, want 1", len(tb.api.photos))
	}
	photo := tb.api.photos[0]
	if !strings.HasPrefix(photo.filename, imgs[0].ID) {
		t.Errorf("browsed %q, want the newest image %q", photo.filename, imgs[0].ID)
	}
	if !strings.Contains(photo.caption, imgs[0].Prompt) {
		t.Errorf("caption = %q, want the prompt", photo.caption)
	}
	if !hasButton(photo.keyboard, "1/3") {
		t.Errorf("no position indicator in %+v", photo.keyboard)
	}
}

// The arrows walk the list and wrap around, replacing the browser's message
// instead of piling up photos.
func TestBrowseNavigationWraps(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	imgs := seedGallery(t, tb, 1, 3)

	// Opening the browser from the gallery listing leaves the listing alone.
	tb.press(t, 1, cbGalleryBrowse+":2")
	if got := tb.api.photos[len(tb.api.photos)-1].filename; !strings.HasPrefix(got, imgs[2].ID) {
		t.Errorf("browsed %q, want %q", got, imgs[2].ID)
	}
	if len(tb.api.deleted) != 0 {
		t.Errorf("opening the browser removed %d messages, want none", len(tb.api.deleted))
	}

	// An arrow press replaces the browser's own photo message.
	tb.pressOnPhoto(t, 1, cbGalleryBrowse+":1")
	if len(tb.api.deleted) != 1 {
		t.Errorf("an arrow press removed %d messages, want the previous photo", len(tb.api.deleted))
	}

	// Past the end wraps back to the newest.
	tb.press(t, 1, cbGalleryBrowse+":3")
	if got := tb.api.photos[len(tb.api.photos)-1].filename; !strings.HasPrefix(got, imgs[0].ID) {
		t.Errorf("browsed %q after the end, want the first image %q", got, imgs[0].ID)
	}
}

// The 🖼 Gallery button under a browser photo cannot edit the photo message in
// place (Telegram rejects that), so it sends the listing as a fresh message.
func TestGalleryButtonFromPhotoSendsFreshListing(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	seedGallery(t, tb, 1, 2)

	tb.pressOnPhoto(t, 1, cbGalleryList)

	last := tb.api.messages[len(tb.api.messages)-1]
	if !hasButton(last.keyboard, "🖼 Browse images") {
		t.Errorf("the gallery button did not open the listing: %+v", last.keyboard)
	}
	if _, ok := tb.api.lastEdit(1); ok {
		t.Error("the gallery button tried to edit the photo message")
	}
}

// Covers the buttons under an image: delete removes it and its message.
func TestImageDeleteButton(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	imgs := seedGallery(t, tb, 1, 2)
	usr, _ := tb.bot.authenticate(t.Context(), 1)

	tb.press(t, 1, cbPrefixImage+":"+cbImageDelete+":"+imgs[0].ID)

	if got := tb.api.lastAnswer(); got != "deleted" {
		t.Errorf("toast = %q", got)
	}
	if len(tb.api.deleted) != 1 {
		t.Error("the image's message was not removed")
	}
	left, _ := tb.gallery.List(t.Context(), usr.userID)
	if len(left) != 1 {
		t.Errorf("gallery has %d images, want 1", len(left))
	}

	// Pressing it again (a stale button) reports the miss instead of failing.
	tb.press(t, 1, cbPrefixImage+":"+cbImageDelete+":"+imgs[0].ID)
	if got := tb.api.lastAnswer(); got != "image not found" {
		t.Errorf("toast for a stale delete = %q", got)
	}
}

// "Again" regenerates from the stored request and sends a new image.
func TestImageRegenerateButton(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	usr, _ := tb.bot.authenticate(t.Context(), 1)
	src, _ := tb.gallery.Save(t.Context(), usr.userID, []byte("bytes"), gallery.Metadata{
		Prompt: "a cat on a bike", Model: "black-forest-labs/flux.2-pro", ModelName: "Flux 2 Pro",
		AspectRatio: "16:9", ContentType: "image/png",
	})

	tb.press(t, 1, cbPrefixImage+":"+cbImageRegen+":"+src.ID)

	if len(tb.gen.calls) != 1 {
		t.Fatalf("generated %d times, want 1", len(tb.gen.calls))
	}
	call := tb.gen.calls[0]
	if call.Prompt != "a cat on a bike" || call.Model != "black-forest-labs/flux.2-pro" || call.AspectRatio != "16:9" {
		t.Errorf("regenerated with %+v, want the stored request", call)
	}
	if len(tb.api.photos) != 1 {
		t.Fatalf("sent %d photos, want the regenerated one", len(tb.api.photos))
	}
	imgs, _ := tb.gallery.List(t.Context(), usr.userID)
	if len(imgs) != 2 {
		t.Errorf("gallery has %d images, want the original plus the new one", len(imgs))
	}
}

// "Original" resends the stored bytes as an uncompressed document so the user
// can get the full-quality file Telegram would otherwise re-encode.
func TestImageOriginalButton(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	usr, _ := tb.bot.authenticate(t.Context(), 1)
	src, _ := tb.gallery.Save(t.Context(), usr.userID, []byte("the original bytes"), gallery.Metadata{
		Prompt: "a cat on a bike", Model: DefaultModel, ModelName: "M", ContentType: "image/png",
	})

	tb.press(t, 1, cbPrefixImage+":"+cbImageOriginal+":"+src.ID)

	if len(tb.api.documents) != 1 {
		t.Fatalf("sent %d documents, want 1", len(tb.api.documents))
	}
	doc := tb.api.documents[0]
	if string(doc.data) != "the original bytes" {
		t.Errorf("document bytes = %q, want the stored original untouched", doc.data)
	}
	if !strings.HasPrefix(doc.filename, src.ID) || !strings.HasSuffix(doc.filename, ".png") {
		t.Errorf("filename = %q, want <id>.png", doc.filename)
	}
	if len(tb.api.photos) != 0 {
		t.Errorf("sent %d photos, want the original delivered as a document only", len(tb.api.photos))
	}

	// A stale button (the image is gone) reports the miss instead of failing.
	_ = tb.gallery.Delete(t.Context(), usr.userID, src.ID)
	tb.press(t, 1, cbPrefixImage+":"+cbImageOriginal+":"+src.ID)
	if got := tb.api.lastAnswer(); got != "image not found" {
		t.Errorf("toast for a stale original = %q", got)
	}
}

// "Delete all" asks before deleting; /clear stays the one-shot path.
func TestClearAllButtonConfirmsFirst(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	seedGallery(t, tb, 1, 2)
	usr, _ := tb.bot.authenticate(t.Context(), 1)

	tb.press(t, 1, cbGalleryClear)
	edit, ok := tb.api.lastEdit(1)
	if !ok || !strings.Contains(edit.text, "Delete all 2") {
		t.Fatalf("clear did not ask for confirmation: %q", edit.text)
	}
	if imgs, _ := tb.gallery.List(t.Context(), usr.userID); len(imgs) != 2 {
		t.Fatalf("images were deleted before confirmation")
	}

	tb.press(t, 1, cbGalleryClearYes)
	if imgs, _ := tb.gallery.List(t.Context(), usr.userID); len(imgs) != 0 {
		t.Errorf("gallery still has %d images after confirming", len(imgs))
	}
	if got := tb.api.lastAnswer(); got != "gallery cleared" {
		t.Errorf("toast = %q", got)
	}
}

// An empty gallery offers no destructive buttons.
func TestGalleryEmptyKeyboard(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdGallery(t.Context(), textMessage(1, "/gallery"), "")

	last := tb.api.messages[len(tb.api.messages)-1]
	if hasButton(last.keyboard, "🗑 Delete all") {
		t.Error("an empty gallery offers to delete everything")
	}
}

// The 🖼 Gallery button reaches the listing and offers every way out of it:
// the browser, the guarded delete-all, and the way back to the menu.
func TestGalleryButtonOpensTheListing(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	usr, _ := tb.bot.authenticate(t.Context(), 1)
	tb.gallery.Save(t.Context(), usr.userID, []byte("x"), gallery.Metadata{Prompt: "a cat", Model: "m", ModelName: "M"})

	tb.bot.handleUpdate(t.Context(), &models.Update{Message: textMessage(1, btnGallery)})

	last := tb.api.messages[len(tb.api.messages)-1]
	if !strings.Contains(last.text, "a cat") {
		t.Errorf("listing = %q", last.text)
	}
	for _, want := range []string{"🖼 Browse images", "🗑 Delete all", "⬅️ Back"} {
		if !hasButton(last.keyboard, want) {
			t.Errorf("the listing has no %q button: %+v", want, last.keyboard)
		}
	}
}
