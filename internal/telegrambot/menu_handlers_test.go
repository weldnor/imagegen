package telegrambot

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestCmdHelpListsCommandsAndOffersTheMenu(t *testing.T) {
	tb := newTestBot()
	tb.bot.cmdHelp(t.Context(), textMessage(1, "/help"), "")

	help := strings.Join(tb.api.allText(1), "\n")
	for _, want := range []string{"/generate", "/gallery", "/model", "/admin", "reference image"} {
		if !strings.Contains(help, want) {
			t.Errorf("/help output does not mention %q", want)
		}
	}
	if len(tb.api.messages) == 0 || len(tb.api.messages[len(tb.api.messages)-1].keyboard.InlineKeyboard) == 0 {
		t.Error("/help did not attach the menu buttons")
	}
}

// /help works before logging in: it is how a new user finds out how to.
func TestCmdHelpWithoutAuth(t *testing.T) {
	tb := newTestBot()
	tb.bot.cmdHelp(t.Context(), textMessage(1, "/help"), "")
	if got := strings.Join(tb.api.allText(1), "\n"); !strings.Contains(got, "/login") {
		t.Errorf("/help = %q, want it to mention /login", got)
	}
}

func TestCmdStart(t *testing.T) {
	tb := newTestBot()

	tb.bot.cmdStart(t.Context(), textMessage(1, "/start"), "")
	if got := tb.api.lastText(1); !strings.Contains(got, loginPrompt) {
		t.Errorf("/start before login = %q, want a login prompt", got)
	}

	loggedInBot(tb, 1)
	tb.bot.cmdStart(t.Context(), textMessage(1, "/start"), "")

	// The persistent keyboard goes up first, then the inline menu.
	var installed bool
	for _, m := range tb.api.messages {
		if len(m.reply.Keyboard) > 0 {
			installed = true
		}
	}
	if !installed {
		t.Error("/start did not install the persistent keyboard")
	}
	last := tb.api.messages[len(tb.api.messages)-1]
	if !strings.Contains(last.text, "Gemini 2.5 Flash Image") {
		t.Errorf("/start after login = %q, want the active model", last.text)
	}
	if !hasButton(last.keyboard, "⚙️ Settings") || !hasButton(last.keyboard, "🖼 Gallery") {
		t.Errorf("/start menu = %+v, want Gallery and Settings", last.keyboard)
	}
}

// The persistent keyboard is only useful if its labels route: a tap sends the
// label as plain text, which must not be mistaken for a prompt.
func TestPersistentKeyboardLabelsRoute(t *testing.T) {
	for _, row := range homeKeyboard().Keyboard {
		for _, btn := range row {
			cmd, args := route(&models.Message{
				Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate},
				Text: btn.Text,
			})
			if cmd == "text" || cmd == "" {
				t.Errorf("keyboard button %q routes to %q, want a command", btn.Text, cmd)
			}
			if _, ok := commandHandlers[cmd]; !ok {
				t.Errorf("keyboard button %q routes to unhandled command %q", btn.Text, cmd)
			}
			if args != "" {
				t.Errorf("keyboard button %q passed args %q", btn.Text, args)
			}
		}
	}
}

// The button opens the settings submenu, not the flat old menu.
func TestCmdSettingsOpensTheSubmenu(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.handleUpdate(t.Context(), &models.Update{Message: textMessage(1, btnSettings)})

	last := tb.api.messages[len(tb.api.messages)-1]
	if !hasButton(last.keyboard, "♻️ Reset to defaults") {
		t.Errorf("⚙️ Settings did not open the settings submenu: %+v", last.keyboard)
	}
	if !hasButton(last.keyboard, "🔢 Images per prompt · 1") {
		t.Errorf("settings do not show the current count: %+v", last.keyboard)
	}
}

func TestCmdMenuAndSettingsRequireAuth(t *testing.T) {
	tb := newTestBot()
	tb.bot.cmdMenu(t.Context(), textMessage(1, "/menu"), "")
	if got := tb.api.lastText(1); got != loginPrompt {
		t.Errorf("/menu without auth = %q", got)
	}

	loggedInBot(tb, 2)
	tb.bot.cmdSettings(t.Context(), textMessage(2, "/settings"), "")
	last := tb.api.messages[len(tb.api.messages)-1]
	if !strings.Contains(last.text, "Reference images") {
		t.Errorf("/settings = %q", last.text)
	}
	if !hasButton(last.keyboard, "🎨 Model · Gemini 2.5 Flash Image") {
		t.Errorf("/settings does not show the active model: %+v", last.keyboard)
	}
}

// An unknown command in a private chat is answered with a hint; in a group,
// where it may belong to another bot, it is ignored.
func TestUnknownCommand(t *testing.T) {
	tb := newTestBot()

	private := &models.Message{ID: 1, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}, Text: "/galery"}
	tb.bot.handleUpdate(t.Context(), &models.Update{Message: private})
	got := tb.api.lastText(1)
	if !strings.Contains(got, "unknown command") || !strings.Contains(got, "/gallery") {
		t.Errorf("reply = %q", got)
	}

	group := &models.Message{ID: 2, Chat: models.Chat{ID: 2, Type: models.ChatTypeGroup}, Text: "/somethingelse"}
	tb.bot.handleUpdate(t.Context(), &models.Update{Message: group})
	if got := tb.api.lastText(2); got != "" {
		t.Errorf("group reply = %q, want silence", got)
	}
}

// The settings screen hides options the active model cannot use, and says so
// for the one setting that has no button.
func TestSettingsScreenSkipsUnsupportedOptions(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)
	tb.bot.cmdModel(t.Context(), textMessage(1, ""), "black-forest-labs/flux.2-pro")
	set := tb.bot.settings(1)

	kb := settingsKeyboard(set)
	for _, row := range kb.InlineKeyboard {
		if strings.HasPrefix(row[0].Text, "🖼 Image size") {
			t.Errorf("settings offer image size for a model without it: %+v", kb)
		}
	}
	if !hasButton(kb, "📐 Aspect ratio · auto") {
		t.Errorf("settings omit aspect ratio, which the model supports: %+v", kb)
	}
	if got := settingsScreen(set); !strings.Contains(got, "not supported") {
		t.Errorf("settings screen does not say reference images are unsupported: %q", got)
	}
}

// The persistent keyboard costs a message of its own, so /menu installs it
// once and then leaves it alone.
func TestMenuInstallsTheKeyboardOnce(t *testing.T) {
	tb := newTestBot()
	loggedInBot(tb, 1)

	tb.bot.cmdMenu(t.Context(), textMessage(1, "/menu"), "")
	tb.bot.cmdMenu(t.Context(), textMessage(1, "/menu"), "")

	installs := 0
	for _, m := range tb.api.messages {
		if len(m.reply.Keyboard) > 0 {
			installs++
		}
	}
	if installs != 1 {
		t.Errorf("/menu installed the persistent keyboard %d times, want 1", installs)
	}
}
