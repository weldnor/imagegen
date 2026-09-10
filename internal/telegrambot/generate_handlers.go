package telegrambot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
)

func (b *Bot) cmdGenerate(ctx context.Context, msg *models.Message, args string) {
	b.generateAndReply(ctx, msg, args)
}

func (b *Bot) cmdText(ctx context.Context, msg *models.Message, args string) {
	b.generateAndReply(ctx, msg, args)
}

func (b *Bot) cmdPhoto(ctx context.Context, msg *models.Message, args string) {
	b.generateAndReply(ctx, msg, args)
}

// largestPhoto returns the highest-resolution size Telegram sent for a photo.
func largestPhoto(sizes []models.PhotoSize) *models.PhotoSize {
	if len(sizes) == 0 {
		return nil
	}
	best := &sizes[0]
	for i := range sizes {
		if sizes[i].Width*sizes[i].Height > best.Width*best.Height {
			best = &sizes[i]
		}
	}
	return best
}

// referencePhoto returns the photo attached to msg, or to the message it
// replies to, if any.
func referencePhoto(msg *models.Message) *models.PhotoSize {
	if p := largestPhoto(msg.Photo); p != nil {
		return p
	}
	if msg.ReplyToMessage != nil {
		return largestPhoto(msg.ReplyToMessage.Photo)
	}
	return nil
}

func toDataURI(data []byte) string {
	ct := http.DetectContentType(data)
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func upstreamMessage(err error) string {
	var ue *openrouter.UpstreamError
	if errors.As(err, &ue) {
		return ue.Message
	}
	return "image generation failed"
}

// generateAndReply implements /generate, a plain-text message, and a photo
// message (caption as prompt), mirroring POST /api/generate: it builds the
// same GenerateParams from the chat's active model/options, runs `count`
// generations concurrently, persists and sends back every success, and
// reports failures the same way the HTTP handler does.
func (b *Bot) generateAndReply(ctx context.Context, msg *models.Message, prompt string) {
	chatID := msg.Chat.ID
	authCtx, ok := b.requireAuth(ctx, chatID)
	if !ok {
		return
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		b.reply(ctx, chatID, "please provide a prompt, e.g. /generate a cat riding a bike")
		return
	}

	modelID := b.activeModel(chatID)
	cfg, known := openrouter.Model(modelID)
	if !known {
		// The active model became unknown (should not normally happen); fall
		// back to the fixed default.
		modelID = DefaultModel
		cfg, _ = openrouter.Model(modelID)
	}
	aspect := b.activeAspect(chatID)
	size := b.activeSize(chatID)
	count := b.activeCount(chatID)

	var references []string
	refCount := 0
	if photo := referencePhoto(msg); photo != nil {
		if !cfg.SupportsImageInput {
			b.reply(ctx, chatID, "the active model ("+modelID+") does not accept reference images; the photo was ignored")
		} else {
			data, err := b.api.DownloadFile(ctx, photo.FileID)
			if err != nil {
				b.reply(ctx, chatID, "could not download the reference photo; generating from the prompt alone")
			} else {
				references = []string{toDataURI(data)}
				refCount = 1
			}
		}
	}

	params := openrouter.GenerateParams{
		Prompt:      prompt,
		Model:       modelID,
		ImageSize:   size,
		AspectRatio: aspect,
		References:  references,
	}

	type outcome struct {
		img *openrouter.Image
		err error
	}
	outcomes := make([]outcome, count)

	limit := b.concurrency
	if limit < 1 {
		limit = 1
	}
	if limit > count {
		limit = count
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			img, err := b.gen.Generate(ctx, params)
			outcomes[idx] = outcome{img: img, err: err}
		}(i)
	}
	wg.Wait()

	sent, failed := 0, 0
	var firstErr string
	for _, o := range outcomes {
		if o.err != nil {
			failed++
			if firstErr == "" {
				firstErr = upstreamMessage(o.err)
			}
			continue
		}
		saved, err := b.gallery.Save(ctx, authCtx.userID, o.img.Data, gallery.Metadata{
			Prompt:         prompt,
			Model:          modelID,
			ModelName:      cfg.Name,
			ImageSize:      size,
			AspectRatio:    aspect,
			ReferenceCount: refCount,
			ContentType:    o.img.ContentType,
		})
		if err != nil {
			failed++
			if firstErr == "" {
				firstErr = "failed to store a generated image"
			}
			continue
		}
		filename := saved.ID + "." + openrouter.ExtensionForContentType(saved.ContentType)
		if err := b.api.SendPhoto(ctx, chatID, o.img.Data, filename, ""); err != nil {
			log.Printf("telegrambot: sending photo to chat %d: %v", chatID, err)
			continue
		}
		sent++
	}

	if sent == 0 {
		if firstErr == "" {
			firstErr = "image generation failed"
		}
		b.reply(ctx, chatID, "generation failed: "+firstErr)
		return
	}
	if failed > 0 {
		b.reply(ctx, chatID, fmt.Sprintf("%d of %d images failed: %s", failed, count, firstErr))
	}
}
