package telegrambot

import (
	"context"
	"strings"

	"github.com/go-telegram/bot/models"
)

// route extracts the command to dispatch and its argument text from an
// incoming message. It recognizes "/command args", "/command@BotName args",
// a plain-text message ("text"), and a message carrying a photo ("photo").
// It returns cmd == "" for a message that should be ignored.
func route(msg *models.Message) (cmd, args string) {
	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	if strings.HasPrefix(text, "/") {
		fields := strings.SplitN(strings.TrimSpace(text), " ", 2)
		name := strings.TrimPrefix(fields[0], "/")
		if at := strings.IndexByte(name, '@'); at >= 0 {
			name = name[:at]
		}
		rest := ""
		if len(fields) > 1 {
			rest = strings.TrimSpace(fields[1])
		}
		return strings.ToLower(name), rest
	}
	if len(msg.Photo) > 0 {
		return "photo", strings.TrimSpace(msg.Caption)
	}
	if strings.TrimSpace(msg.Text) != "" {
		return "text", strings.TrimSpace(msg.Text)
	}
	return "", ""
}

// commandHandler handles one routed command.
type commandHandler func(b *Bot, ctx context.Context, msg *models.Message, args string)

// commandHandlers maps every command name route can produce to its handler.
var commandHandlers = map[string]commandHandler{
	"admin":         (*Bot).cmdAdmin,
	"adduser":       (*Bot).cmdAddUser,
	"addtelegramid": (*Bot).cmdAddTelegramID,
	"listusers":     (*Bot).cmdListUsers,
	"login":         (*Bot).cmdLogin,
	"logout":        (*Bot).cmdLogout,
	"models":        (*Bot).cmdModels,
	"model":         (*Bot).cmdModel,
	"aspect":        (*Bot).cmdAspect,
	"size":          (*Bot).cmdSize,
	"count":         (*Bot).cmdCount,
	"generate":      (*Bot).cmdGenerate,
	"gallery":       (*Bot).cmdGallery,
	"delete":        (*Bot).cmdDelete,
	"clear":         (*Bot).cmdClear,
	"text":          (*Bot).cmdText,
	"photo":         (*Bot).cmdPhoto,
}
