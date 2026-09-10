package telegrambot

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
)

// loginPrompt is what an unauthenticated chat is told, whether it typed a
// command or tapped a button.
const loginPrompt = "please /login <username> <password> first"

const welcome = `🎨 Imagen — send a prompt, get an image.

Just type what you want to see and send it. Attach a photo (or reply to one)
to use it as a reference.

Everything else is on the buttons below: 🖼 Gallery for your images,
⚙️ Settings for the model and options, ❓ Help for the rest.`

// generateHint answers the "New image" button: the one thing the buttons
// cannot do for the user is write the prompt.
const generateHint = `Send me a prompt and I'll generate it, for example:

  a red fox asleep in a snowy forest, golden hour
  an isometric pixel-art coffee shop

Attach a photo to the message to use it as a reference image.`

// referenceLine describes what the active model does with an attached photo —
// the one setting that is not a button, because there is nothing to pick.
func referenceLine(s activeSettings) string {
	if !s.cfg.SupportsImageInput {
		return "Reference images: not supported by this model."
	}
	return "Reference images: up to " + strconv.Itoa(s.cfg.MaxReferences) +
		" — attach a photo to your prompt to use it as one."
}

// settingsLine is the one-line form of the summary, for screens that are
// about something else but should still show what a prompt would produce.
func settingsLine(s activeSettings) string {
	parts := []string{s.cfg.Name}
	if s.cfg.SupportsAspectRatio && s.aspect != "" {
		parts = append(parts, s.aspect)
	}
	if s.cfg.SupportsImageSize && s.size != "" {
		parts = append(parts, s.size)
	}
	if s.count > 1 {
		parts = append(parts, strconv.Itoa(s.count)+" images")
	}
	return strings.Join(parts, " · ")
}

// homeScreen is the root menu message.
func homeScreen(s activeSettings) string {
	return welcome + "\n\nNow generating with: " + settingsLine(s)
}

// settingsScreen is the settings submenu message. The values live on the
// buttons that change them, so the text says only what the buttons cannot.
func settingsScreen(s activeSettings) string {
	return "⚙️ Settings — tap a row to change it.\n\n" + referenceLine(s) +
		"\n\nGenerating with: " + settingsLine(s)
}

// optionScreen is one option picker: what it asks for, then the options in
// force, so every tap shows its own effect.
func optionScreen(header string, s activeSettings) string {
	return header + "\n\nNow generating with: " + settingsLine(s)
}

// helpScreen is the help rendered inside the menu; it stays one message, so
// it points at /help when the catalog outgrows a single one.
func helpScreen() string {
	pages := helpPages()
	if len(pages) == 1 {
		return pages[0]
	}
	return pages[0] + "\n\n(send /help for the rest)"
}

// sendHome installs the persistent keyboard, so the four entry points are in
// reach from then on. It costs a message of its own — a message carries either
// an inline keyboard or a persistent one, not both.
func (b *Bot) sendHome(ctx context.Context, chatID int64, text string) {
	b.mu.Lock()
	b.state(chatID).homeKeyboardSent = true
	b.mu.Unlock()
	if err := b.api.SendMessageWithReplyKeyboard(ctx, chatID, text, homeKeyboard()); err != nil {
		log.Printf("telegrambot: installing the home keyboard in chat %d: %v", chatID, err)
	}
}

// ensureHome installs the persistent keyboard unless this chat already has
// it, so a screen that is opened often does not repeat itself.
func (b *Bot) ensureHome(ctx context.Context, chatID int64, text string) {
	b.mu.Lock()
	sent := b.state(chatID).homeKeyboardSent
	b.mu.Unlock()
	if !sent {
		b.sendHome(ctx, chatID, text)
	}
}

func (b *Bot) cmdStart(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if _, ok := b.authenticate(ctx, chatID); !ok {
		b.reply(ctx, chatID, welcome+"\n\n"+loginPrompt)
		return
	}
	b.sendHome(ctx, chatID, welcome)
	b.replyWithKeyboard(ctx, chatID, homeScreen(b.settings(chatID)), mainMenuKeyboard())
}

func (b *Bot) cmdHelp(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	pages := helpPages()
	for _, page := range pages[:len(pages)-1] {
		b.reply(ctx, chatID, page)
	}
	b.replyWithKeyboard(ctx, chatID, pages[len(pages)-1], mainMenuKeyboard())
}

// cmdMenu opens the root menu. It also (re)installs the persistent keyboard,
// so a chat that was linked by an admin and never ran /start can still get it
// without knowing that /start is where it comes from.
func (b *Bot) cmdMenu(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	b.ensureHome(ctx, chatID, "The buttons below are always here.")
	b.replyWithKeyboard(ctx, chatID, homeScreen(b.settings(chatID)), mainMenuKeyboard())
}

// cmdSettings opens the settings submenu directly, which is also where the
// ⚙️ Settings button lands.
func (b *Bot) cmdSettings(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	set := b.settings(chatID)
	b.replyWithKeyboard(ctx, chatID, settingsScreen(set), settingsKeyboard(set))
}

// cmdNewImage answers the 🎨 New image button. There is nothing to do but ask
// for a prompt, so it says how, with examples.
func (b *Bot) cmdNewImage(ctx context.Context, msg *models.Message, _ string) {
	chatID := msg.Chat.ID
	if _, ok := b.requireAuth(ctx, chatID); !ok {
		return
	}
	b.reply(ctx, chatID, generateHint+"\n\nNow generating with: "+settingsLine(b.settings(chatID)))
}

// cmdUnknown answers a command nobody handles, suggesting the closest one. It
// stays quiet outside private chats, where a command may well belong to
// another bot.
func (b *Bot) cmdUnknown(ctx context.Context, msg *models.Message, name string) {
	if msg.Chat.Type != models.ChatTypePrivate {
		return
	}
	text := "unknown command /" + name
	if hint := unknownCommandHint(name); hint != "" {
		text += "; did you mean /" + hint + "?"
	}
	b.reply(ctx, msg.Chat.ID, text+"\n\nUse the buttons below, or /help for every command.")
}
