package telegrambot

import (
	"context"
	"log"

	"github.com/weldnor/imagegen/internal/openrouter"
)

// Callback data is "<prefix>:<payload>", split on the first colon so a payload
// may itself contain colons (an aspect ratio, an image id). Telegram caps the
// whole string at 64 bytes, which every value built here stays well under.
const (
	cbPrefixMenu    = "menu"
	cbPrefixModel   = "model"
	cbPrefixAspect  = "aspect"
	cbPrefixSize    = "size"
	cbPrefixCount   = "count"
	cbPrefixGallery = "gal"
	cbPrefixImage   = "img"
	cbPrefixNoop    = "noop"
)

const (
	cbMenuMain     = cbPrefixMenu + ":main"
	cbMenuSettings = cbPrefixMenu + ":settings"
	cbMenuModel    = cbPrefixMenu + ":model"
	cbMenuAspect   = cbPrefixMenu + ":aspect"
	cbMenuSize     = cbPrefixMenu + ":size"
	cbMenuCount    = cbPrefixMenu + ":count"
	cbMenuReset    = cbPrefixMenu + ":reset"
	cbMenuHelp     = cbPrefixMenu + ":help"
	cbMenuClose    = cbPrefixMenu + ":close"

	cbGalleryList     = cbPrefixGallery + ":list"
	cbGalleryBrowse   = cbPrefixGallery + ":browse" // + ":<index>"
	cbGalleryClear    = cbPrefixGallery + ":clear"
	cbGalleryClearYes = cbPrefixGallery + ":clearyes"

	cbImageRegen    = "regen"  // img:regen:<image id>
	cbImageDelete   = "delete" // img:delete:<image id>
	cbImageOriginal = "orig"   // img:orig:<image id>, resends the image as an uncompressed file

	cbNoop = cbPrefixNoop + ":"
)

// callbackContext is one button press: which chat and message it came from,
// and the payload after the prefix.
type callbackContext struct {
	queryID   string
	chatID    int64
	messageID int
	payload   string
	// fromPhoto is true when the button hangs under an image rather than a
	// text message; the gallery browser replaces its own photo messages but
	// leaves a listing it was opened from alone.
	fromPhoto bool

	answered bool
}

// ack answers the callback query, dismissing the button's spinner. The
// dispatcher acks with whatever the handler returns; a handler that is about
// to do slow work should ack first so Telegram does not time the query out.
func (cb *callbackContext) ack(b *Bot, ctx context.Context, text string) {
	if cb.answered {
		return
	}
	cb.answered = true
	if err := b.api.AnswerCallbackQuery(ctx, cb.queryID, text); err != nil {
		log.Printf("telegrambot: answering callback query for chat %d: %v", cb.chatID, err)
	}
}

// callbackHandler handles one button press and returns the toast to show the
// user ("" for a silent acknowledgement).
type callbackHandler func(b *Bot, ctx context.Context, cb *callbackContext) string

// callbackHandlers maps every callback-data prefix to its handler.
var callbackHandlers = map[string]callbackHandler{
	cbPrefixMenu:    (*Bot).cbMenu,
	cbPrefixModel:   (*Bot).cbModel,
	cbPrefixAspect:  (*Bot).cbAspect,
	cbPrefixSize:    (*Bot).cbSize,
	cbPrefixCount:   (*Bot).cbCount,
	cbPrefixGallery: (*Bot).cbGallery,
	cbPrefixImage:   (*Bot).cbImage,
	cbPrefixNoop:    func(*Bot, context.Context, *callbackContext) string { return "" },
}

// editMenu replaces the menu message in place with a new screen. When the
// button came from a photo (the gallery browser), there is no text to edit —
// Telegram rejects that — so the screen is sent as a fresh message instead.
func (b *Bot) editMenu(ctx context.Context, cb *callbackContext, text string, kb keyboard) {
	if cb.fromPhoto {
		b.replyWithKeyboard(ctx, cb.chatID, text, kb)
		return
	}
	if err := b.api.EditMessage(ctx, cb.chatID, cb.messageID, text, kb); err != nil {
		log.Printf("telegrambot: editing message %d in chat %d: %v", cb.messageID, cb.chatID, err)
	}
}

// cbMenu navigates between menu screens. Every screen is drawn into the menu
// message in place, so the chat does not fill up with menus.
func (b *Bot) cbMenu(ctx context.Context, cb *callbackContext) string {
	if cb.payload == "close" {
		b.deleteMessage(ctx, cb.chatID, cb.messageID)
		return ""
	}
	if _, ok := b.authenticate(ctx, cb.chatID); !ok {
		return loginPrompt
	}

	set := b.settings(cb.chatID)

	switch cb.payload {
	case "settings":
		b.showSettings(ctx, cb, set)
	case "model":
		b.editMenu(ctx, cb, optionScreen("🎨 Pick a model", set), modelKeyboard(set.modelID))
	case "aspect":
		if !set.cfg.SupportsAspectRatio {
			return unsupported("aspect ratio", set.modelID)
		}
		b.editMenu(ctx, cb, optionScreen("📐 Pick an aspect ratio", set), aspectKeyboard(set.aspect))
	case "size":
		if !set.cfg.SupportsImageSize {
			return unsupported("image size", set.modelID)
		}
		b.editMenu(ctx, cb, optionScreen("🖼 Pick an image size", set), sizeKeyboard(set.size))
	case "count":
		b.editMenu(ctx, cb, optionScreen("🔢 How many images per prompt?", set), countKeyboard(set.count))
	case "reset":
		b.resetSettings(cb.chatID)
		b.showSettings(ctx, cb, b.settings(cb.chatID))
		return "settings reset"
	case "help":
		b.editMenu(ctx, cb, helpScreen(), backOnlyKeyboard())
	default: // "main" and anything unrecognized
		b.editMenu(ctx, cb, homeScreen(set), mainMenuKeyboard())
	}
	return ""
}

// showSettings draws the settings submenu: the summary in the text, the same
// values on the buttons that change them.
func (b *Bot) showSettings(ctx context.Context, cb *callbackContext, set activeSettings) {
	b.editMenu(ctx, cb, settingsScreen(set), settingsKeyboard(set))
}

// unsetIfAuto maps the "auto" button of the aspect-ratio and image-size
// pickers onto the empty value, which means "let the model decide".
func unsetIfAuto(payload string) string {
	if payload == "auto" {
		return ""
	}
	return payload
}

func unsupported(option, modelID string) string {
	return "the active model (" + modelID + ") does not support " + option + " selection"
}

// cbModel applies a model chosen from the model keyboard.
func (b *Bot) cbModel(ctx context.Context, cb *callbackContext) string {
	if _, ok := b.authenticate(ctx, cb.chatID); !ok {
		return loginPrompt
	}
	cfg, known := openrouter.Model(cb.payload)
	if !known {
		return "unknown model"
	}
	b.setModel(cb.chatID, cb.payload, cfg)
	b.editMenu(ctx, cb, optionScreen("🎨 Pick a model", b.settings(cb.chatID)), modelKeyboard(cb.payload))
	return "model: " + cfg.Name
}

func (b *Bot) cbAspect(ctx context.Context, cb *callbackContext) string {
	if _, ok := b.authenticate(ctx, cb.chatID); !ok {
		return loginPrompt
	}
	modelID := b.activeModel(cb.chatID)
	if cfg, _ := openrouter.Model(modelID); !cfg.SupportsAspectRatio {
		return unsupported("aspect ratio", modelID)
	}
	ratio, ok := openrouter.NormalizeAspectRatio(unsetIfAuto(cb.payload))
	if !ok {
		return aspectUsage
	}
	b.mu.Lock()
	b.state(cb.chatID).aspectRatio = ratio
	b.mu.Unlock()
	b.editMenu(ctx, cb, optionScreen("📐 Pick an aspect ratio", b.settings(cb.chatID)), aspectKeyboard(ratio))
	return "aspect ratio: " + shortValue(ratio)
}

func (b *Bot) cbSize(ctx context.Context, cb *callbackContext) string {
	if _, ok := b.authenticate(ctx, cb.chatID); !ok {
		return loginPrompt
	}
	modelID := b.activeModel(cb.chatID)
	if cfg, _ := openrouter.Model(modelID); !cfg.SupportsImageSize {
		return unsupported("image size", modelID)
	}
	size, ok := openrouter.NormalizeImageSize(unsetIfAuto(cb.payload))
	if !ok {
		return sizeUsage
	}
	b.mu.Lock()
	b.state(cb.chatID).imageSize = size
	b.mu.Unlock()
	b.editMenu(ctx, cb, optionScreen("🖼 Pick an image size", b.settings(cb.chatID)), sizeKeyboard(size))
	return "image size: " + shortValue(size)
}

func (b *Bot) cbCount(ctx context.Context, cb *callbackContext) string {
	if _, ok := b.authenticate(ctx, cb.chatID); !ok {
		return loginPrompt
	}
	n, ok := parseCount(cb.payload)
	if !ok {
		return countUsage
	}
	b.mu.Lock()
	b.state(cb.chatID).count = n
	b.mu.Unlock()
	b.editMenu(ctx, cb, optionScreen("🔢 How many images per prompt?", b.settings(cb.chatID)), countKeyboard(n))
	return "count: " + cb.payload
}
