package telegrambot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// API is the subset of the Telegram Bot API the bot's command handlers use.
// The production implementation wraps *bot.Bot (github.com/go-telegram/bot);
// tests inject a fake.
type API interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
	SendPhoto(ctx context.Context, chatID int64, data []byte, filename, caption string) error
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

// telegramClient adapts *bot.Bot to API and drives the long-polling loop.
type telegramClient struct {
	b *bot.Bot
}

// newTelegramClient builds a client for token whose every incoming update is
// delivered to handler. Construction never calls the Telegram API (so it
// cannot fail on a bad token); a bad token only surfaces once polling starts.
func newTelegramClient(token string, handler bot.HandlerFunc) (*telegramClient, error) {
	b, err := bot.New(token,
		bot.WithSkipGetMe(),
		bot.WithDefaultHandler(handler),
	)
	if err != nil {
		return nil, err
	}
	return &telegramClient{b: b}, nil
}

// Start runs the long-polling loop until ctx is canceled.
func (c *telegramClient) Start(ctx context.Context) {
	c.b.Start(ctx)
}

func (c *telegramClient) SendMessage(ctx context.Context, chatID int64, text string) error {
	_, err := c.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
	return err
}

func (c *telegramClient) SendPhoto(ctx context.Context, chatID int64, data []byte, filename, caption string) error {
	_, err := c.b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)},
		Caption: caption,
	})
	return err
}

func (c *telegramClient) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	_, err := c.b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	return err
}

func (c *telegramClient) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	f, err := c.b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, err
	}
	url := c.b.FileDownloadLink(f)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading telegram file: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}
