package telegrambot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
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
		b.replyWithKeyboard(ctx, chatID, emptyGallery, galleryKeyboard(true))
		return
	}
	pages := paginate(galleryLines(imgs), maxMessageLen)
	for _, page := range pages[:len(pages)-1] {
		b.reply(ctx, chatID, page)
	}
	b.replyWithKeyboard(ctx, chatID, pages[len(pages)-1], galleryKeyboard(false))
}

// emptyGallery is what an empty gallery says: not an error, an invitation.
const emptyGallery = "🖼 Your gallery is empty.\n\nSend a prompt and the image will show up here."

// galleryLines renders the listing newest first, numbered to match the
// browser's counter. The id is on its own line because /delete needs it.
func galleryLines(imgs []gallery.Image) []string {
	lines := make([]string, 0, len(imgs)+1)
	lines = append(lines, fmt.Sprintf("🖼 %d image(s), newest first:", len(imgs)), "")
	for i, img := range imgs {
		lines = append(lines, fmt.Sprintf("%d. %q\n   %s · %s · id %s",
			i+1, truncate(img.Prompt, 120), img.ModelName, img.Created.Format("2006-01-02 15:04"), img.ID))
	}
	return lines
}

// truncate shortens a prompt to keep one listing entry to a couple of lines.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
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

// cmdBrowse opens the one-image-at-a-time gallery browser at the newest image.
func (b *Bot) cmdBrowse(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	b.showGalleryImage(ctx, chatID, 0)
}

// imageBytes reads one stored image's bytes.
func (b *Bot) imageBytes(img gallery.Image) ([]byte, error) {
	return os.ReadFile(b.gallery.AbsPath(img))
}

// imageCaption describes a stored image under its photo.
func imageCaption(img gallery.Image) string {
	parts := []string{img.ModelName}
	if img.AspectRatio != "" {
		parts = append(parts, img.AspectRatio)
	}
	if img.ImageSize != "" {
		parts = append(parts, img.ImageSize)
	}
	parts = append(parts, img.Created.Format("2006-01-02 15:04"))
	caption := img.Prompt + "\n" + strings.Join(parts, " · ")
	// Telegram caps captions at 1024 characters.
	if len(caption) > 1000 {
		caption = caption[:1000] + "…"
	}
	return caption
}

// showGalleryImage sends the image at index (newest first) with the browser's
// buttons. An out-of-range index wraps, so the arrows can loop.
func (b *Bot) showGalleryImage(ctx context.Context, chatID int64, index int) string {
	authCtx, ok := b.authenticate(ctx, chatID)
	if !ok {
		return loginPrompt
	}
	imgs, err := b.gallery.List(ctx, authCtx.userID)
	if err != nil {
		return "could not list the gallery"
	}
	if len(imgs) == 0 {
		b.replyWithKeyboard(ctx, chatID, emptyGallery, galleryKeyboard(true))
		return ""
	}
	index = ((index % len(imgs)) + len(imgs)) % len(imgs)
	img := imgs[index]

	data, err := b.imageBytes(img)
	if err != nil {
		log.Printf("telegrambot: reading image %s for chat %d: %v", img.ID, chatID, err)
		return "could not read that image"
	}
	filename := img.ID + "." + openrouter.ExtensionForContentType(img.ContentType)
	if err := b.api.SendPhoto(ctx, chatID, data, filename, imageCaption(img), browseKeyboard(img.ID, index, len(imgs))); err != nil {
		log.Printf("telegrambot: sending gallery photo to chat %d: %v", chatID, err)
		return "could not send that image"
	}
	return ""
}

// cbGallery handles the gallery buttons: the listing, the browser, and the
// guarded "delete all".
func (b *Bot) cbGallery(ctx context.Context, cb *callbackContext) string {
	authCtx, ok := b.authenticate(ctx, cb.chatID)
	if !ok {
		return loginPrompt
	}
	action, arg, _ := strings.Cut(cb.payload, ":")

	switch action {
	case "browse":
		index, err := strconv.Atoi(arg)
		if err != nil {
			index = 0
		}
		cb.ack(b, ctx, "")
		if cb.fromPhoto {
			// An arrow press replaces the browser's own photo, so the chat
			// does not fill up with one message per step. Opening the browser
			// from a listing leaves the listing in place.
			b.deleteMessage(ctx, cb.chatID, cb.messageID)
		}
		return b.showGalleryImage(ctx, cb.chatID, index)

	case "clear":
		imgs, err := b.gallery.List(ctx, authCtx.userID)
		if err != nil {
			return "could not list the gallery"
		}
		if len(imgs) == 0 {
			return "the gallery is already empty"
		}
		b.editMenu(ctx, cb, fmt.Sprintf("Delete all %d image(s)? This cannot be undone.", len(imgs)), confirmClearKeyboard())
		return ""

	case "clearyes":
		imgs, err := b.gallery.List(ctx, authCtx.userID)
		if err != nil {
			return "could not clear the gallery"
		}
		if err := b.gallery.ClearForUser(ctx, authCtx.userID); err != nil {
			return "could not clear the gallery"
		}
		b.editMenu(ctx, cb, fmt.Sprintf("removed %d image(s)", len(imgs)), galleryKeyboard(true))
		return "gallery cleared"

	default: // "list"
		imgs, err := b.gallery.List(ctx, authCtx.userID)
		if err != nil {
			return "could not list the gallery"
		}
		if len(imgs) == 0 {
			b.editMenu(ctx, cb, emptyGallery, galleryKeyboard(true))
			return ""
		}
		// The listing may not fit one message; the button opens the newest
		// page and says so.
		pages := paginate(galleryLines(imgs), maxMessageLen)
		text := pages[0]
		if len(pages) > 1 {
			text += fmt.Sprintf("\n\n(the newest of %d; /gallery lists them all)", len(imgs))
		}
		b.editMenu(ctx, cb, text, galleryKeyboard(false))
		return ""
	}
}

// cbImage handles the buttons under a single image: generate another one from
// the same request, or delete it.
func (b *Bot) cbImage(ctx context.Context, cb *callbackContext) string {
	authCtx, ok := b.authenticate(ctx, cb.chatID)
	if !ok {
		return loginPrompt
	}
	action, imageID, _ := strings.Cut(cb.payload, ":")

	switch action {
	case cbImageDelete:
		if err := b.gallery.Delete(ctx, authCtx.userID, imageID); err != nil {
			if errors.Is(err, gallery.ErrNotFound) {
				return "image not found"
			}
			return "could not delete image"
		}
		// The photo message's buttons would act on an image that is gone, so
		// the message goes with it.
		cb.ack(b, ctx, "deleted")
		b.deleteMessage(ctx, cb.chatID, cb.messageID)
		return ""

	case cbImageRegen:
		img, err := b.gallery.Get(ctx, authCtx.userID, imageID)
		if err != nil {
			return "image not found"
		}
		cb.ack(b, ctx, "generating one more…")
		b.regenerate(ctx, cb.chatID, authCtx, img)
		return ""

	case cbImageOriginal:
		img, err := b.gallery.Get(ctx, authCtx.userID, imageID)
		if err != nil {
			return "image not found"
		}
		data, err := b.imageBytes(img)
		if err != nil {
			log.Printf("telegrambot: reading image %s for chat %d: %v", img.ID, cb.chatID, err)
			return "could not read that image"
		}
		cb.ack(b, ctx, "sending the original…")
		filename := img.ID + "." + openrouter.ExtensionForContentType(img.ContentType)
		if err := b.api.SendDocument(ctx, cb.chatID, data, filename, imageCaption(img)); err != nil {
			log.Printf("telegrambot: sending original of %s to chat %d: %v", img.ID, cb.chatID, err)
			return "could not send the original"
		}
		return ""
	}
	return ""
}
