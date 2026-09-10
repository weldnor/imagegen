package telegrambot

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/weldnor/imagegen/internal/openrouter"
)

// Covers 7.1: /generate builds the same params the HTTP handler would.
func TestGenerateBuildsExpectedParams(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")
	tb.bot.cmdAspect(context.Background(), textMessage(1, ""), "16:9")
	tb.bot.cmdSize(context.Background(), textMessage(1, ""), "2K")

	tb.bot.cmdGenerate(context.Background(), textMessage(1, "/generate a cat"), "a cat")

	if len(tb.gen.calls) != 1 {
		t.Fatalf("Generate called %d times, want 1", len(tb.gen.calls))
	}
	got := tb.gen.calls[0]
	want := openrouter.GenerateParams{
		Prompt: "a cat", Model: "google/gemini-2.5-flash-image", ImageSize: "2K", AspectRatio: "16:9",
	}
	if got.Prompt != want.Prompt || got.Model != want.Model || got.ImageSize != want.ImageSize || got.AspectRatio != want.AspectRatio {
		t.Errorf("Generate params = %+v, want %+v", got, want)
	}
}

// Covers 7.2: generated photos are sent and persisted with matching metadata.
func TestGenerateSendsAndPersists(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image")
	tb.bot.cmdAspect(context.Background(), textMessage(1, ""), "1:1")

	tb.bot.cmdGenerate(context.Background(), textMessage(1, ""), "a dog")

	if len(tb.api.photos) != 1 {
		t.Fatalf("sent %d photos, want 1", len(tb.api.photos))
	}
	if len(tb.gallery.saved) != 1 {
		t.Fatalf("saved %d gallery entries, want 1", len(tb.gallery.saved))
	}
	meta := tb.gallery.saved[0]
	if meta.Prompt != "a dog" || meta.Model != "google/gemini-2.5-flash-image" || meta.AspectRatio != "1:1" {
		t.Errorf("saved metadata = %+v", meta)
	}
}

// Covers 7.3: partial and total failure reporting.
func TestGeneratePartialFailure(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdCount(context.Background(), textMessage(1, ""), "4")

	var n atomic.Int64
	tb.gen.fn = func(openrouter.GenerateParams) (*openrouter.Image, error) {
		if n.Add(1)%2 == 0 {
			return nil, &openrouter.UpstreamError{Status: 502, Message: "boom"}
		}
		return &openrouter.Image{Data: []byte("x"), ContentType: "image/png"}, nil
	}

	tb.bot.cmdGenerate(context.Background(), textMessage(1, ""), "prompt")

	if len(tb.api.photos) != 2 {
		t.Fatalf("sent %d photos, want 2", len(tb.api.photos))
	}
	if got := tb.api.lastText(1); !strings.Contains(got, "2 of 4") {
		t.Errorf("reply = %q, want it to report 2 of 4 failed", got)
	}
}

func TestGenerateAllFail(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.gen.fn = func(openrouter.GenerateParams) (*openrouter.Image, error) {
		return nil, &openrouter.UpstreamError{Status: 502, Message: "all broken"}
	}

	tb.bot.cmdGenerate(context.Background(), textMessage(1, ""), "prompt")

	if len(tb.api.photos) != 0 {
		t.Fatalf("sent %d photos, want 0", len(tb.api.photos))
	}
	if got := tb.api.lastText(1); !strings.Contains(got, "all broken") {
		t.Errorf("reply = %q, want it to include the upstream failure reason", got)
	}
}

// Covers 7.4: reference-image handling.
func TestGenerateWithReferenceImage(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "google/gemini-2.5-flash-image") // SupportsImageInput

	msg := &models.Message{ID: 1, Chat: models.Chat{ID: 1}, Photo: []models.PhotoSize{{FileID: "f1", Width: 100, Height: 100}}, Caption: "make it cooler"}
	tb.bot.cmdPhoto(context.Background(), msg, "make it cooler")

	if len(tb.gen.calls) != 1 || len(tb.gen.calls[0].References) != 1 {
		t.Fatalf("Generate calls = %+v, want one reference image", tb.gen.calls)
	}
}

func TestGenerateReferenceIgnoredForUnsupportedModel(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModel(context.Background(), textMessage(1, ""), "black-forest-labs/flux.2-pro") // no image input

	msg := &models.Message{ID: 1, Chat: models.Chat{ID: 1}, Photo: []models.PhotoSize{{FileID: "f1", Width: 100, Height: 100}}, Caption: "make it cooler"}
	tb.bot.cmdPhoto(context.Background(), msg, "make it cooler")

	if len(tb.gen.calls) != 1 || len(tb.gen.calls[0].References) != 0 {
		t.Fatalf("Generate calls = %+v, want zero references", tb.gen.calls)
	}
	if got := tb.api.lastText(1); !strings.Contains(got, "does not accept reference images") {
		t.Errorf("reply = %q", got)
	}
}

// Covers 7.5: empty prompt is rejected without calling Generate.
func TestGenerateEmptyPromptRejected(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	tb.bot.cmdGenerate(context.Background(), textMessage(1, "/generate"), "")

	if len(tb.gen.calls) != 0 {
		t.Fatalf("Generate called %d times, want 0", len(tb.gen.calls))
	}
	if got := tb.api.lastText(1); !strings.Contains(got, "provide a prompt") {
		t.Errorf("reply = %q", got)
	}
}

func TestUpstreamMessage(t *testing.T) {
	if got := upstreamMessage(&openrouter.UpstreamError{Message: "x"}); got != "x" {
		t.Errorf("upstreamMessage(*UpstreamError) = %q", got)
	}
	if got := upstreamMessage(errors.New("other")); got != "image generation failed" {
		t.Errorf("upstreamMessage(other) = %q", got)
	}
}
