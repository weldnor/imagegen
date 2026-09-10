package telegrambot

import (
	"context"
	"strings"

	"github.com/go-telegram/bot/models"
)

// The labels of the persistent keyboard (see homeKeyboard). A tap on one of
// these sends its label as an ordinary text message, so route maps the labels
// back to commands before anything else can treat them as a prompt.
const (
	btnGenerate = "🎨 New image"
	btnGallery  = "🖼 Gallery"
	btnSettings = "⚙️ Settings"
	btnHelp     = "❓ Help"
)

var replyButtonCommands = map[string]string{
	btnGenerate: "new",
	btnGallery:  "gallery",
	btnSettings: "settings",
	btnHelp:     "help",
}

// route extracts the command to dispatch and its argument text from an
// incoming message. It recognizes "/command args", "/command@BotName args",
// a tap on the persistent keyboard, a plain-text message ("text"), and a
// message carrying a photo ("photo"). It returns cmd == "" for a message that
// should be ignored.
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
	if trimmed := strings.TrimSpace(msg.Text); trimmed != "" {
		if cmd, ok := replyButtonCommands[trimmed]; ok {
			return cmd, ""
		}
		return "text", trimmed
	}
	return "", ""
}

// commandHandler handles one routed command.
type commandHandler func(b *Bot, ctx context.Context, msg *models.Message, args string)

// commandHandlers maps every command name route can produce to its handler.
var commandHandlers = map[string]commandHandler{
	"start":         (*Bot).cmdStart,
	"help":          (*Bot).cmdHelp,
	"menu":          (*Bot).cmdMenu,
	"settings":      (*Bot).cmdSettings,
	"new":           (*Bot).cmdNewImage,
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
	"browse":        (*Bot).cmdBrowse,
	"delete":        (*Bot).cmdDelete,
	"clear":         (*Bot).cmdClear,
	"text":          (*Bot).cmdText,
	"photo":         (*Bot).cmdPhoto,
}
