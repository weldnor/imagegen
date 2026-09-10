package telegrambot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// keyboard is the inline keyboard attached to a message. The zero value (no
// rows) means "no keyboard"; passing it to an edit removes the buttons.
type keyboard = models.InlineKeyboardMarkup

// replyKeyboard is the persistent keyboard Telegram draws under the input
// field. Unlike an inline keyboard it belongs to the chat rather than to one
// message: setting it once keeps the bot's entry points in reach until it is
// replaced.
type replyKeyboard = models.ReplyKeyboardMarkup

// API is the subset of the Telegram Bot API the bot's command handlers use.
// The production implementation wraps *bot.Bot (github.com/go-telegram/bot);
// tests inject a fake.
type API interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
	SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, kb keyboard) error
	// SendMessageWithReplyKeyboard sends text and (re)installs the chat's
	// persistent keyboard.
	SendMessageWithReplyKeyboard(ctx context.Context, chatID int64, text string, kb replyKeyboard) error
	SendPhoto(ctx context.Context, chatID int64, data []byte, filename, caption string, kb keyboard) error
	// SendDocument sends the bytes as an uncompressed file attachment, so the
	// recipient gets the original rather than Telegram's re-encoded "photo".
	SendDocument(ctx context.Context, chatID int64, data []byte, filename, caption string) error
	EditMessage(ctx context.Context, chatID int64, messageID int, text string, kb keyboard) error
	AnswerCallbackQuery(ctx context.Context, queryID, text string) error
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
	SetCommands(ctx context.Context, cmds []models.BotCommand) error
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

func (c *telegramClient) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, kb keyboard) error {
	_, err := c.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: kb})
	return err
}

func (c *telegramClient) SendMessageWithReplyKeyboard(ctx context.Context, chatID int64, text string, kb replyKeyboard) error {
	_, err := c.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: kb})
	return err
}

func (c *telegramClient) SendPhoto(ctx context.Context, chatID int64, data []byte, filename, caption string, kb keyboard) error {
	params := &bot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)},
		Caption: caption,
	}
	if len(kb.InlineKeyboard) > 0 {
		params.ReplyMarkup = kb
	}
	_, err := c.b.SendPhoto(ctx, params)
	return err
}

func (c *telegramClient) SendDocument(ctx context.Context, chatID int64, data []byte, filename, caption string) error {
	_, err := c.b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)},
		Caption:  caption,
	})
	return err
}

// EditMessage replaces a message's text and buttons. Telegram rejects an edit
// that changes nothing; that is not an error worth surfacing (a user can
// re-tap the button that is already selected), so it is swallowed.
func (c *telegramClient) EditMessage(ctx context.Context, chatID int64, messageID int, text string, kb keyboard) error {
	_, err := c.b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ReplyMarkup: kb,
	})
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

func (c *telegramClient) AnswerCallbackQuery(ctx context.Context, queryID, text string) error {
	_, err := c.b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: queryID,
		Text:            text,
	})
	return err
}

func (c *telegramClient) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	_, err := c.b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	return err
}

// SetCommands publishes the command list Telegram shows in the "/" menu.
func (c *telegramClient) SetCommands(ctx context.Context, cmds []models.BotCommand) error {
	_, err := c.b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: cmds})
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
