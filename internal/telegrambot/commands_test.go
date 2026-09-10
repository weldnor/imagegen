package telegrambot

import (
	"regexp"
	"strings"
	"testing"
)

// Telegram's own constraints on a command published to the "/" menu.
var commandNameRE = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func TestCommandCatalogIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commandCatalog {
		if !commandNameRE.MatchString(c.name) {
			t.Errorf("command %q is not a valid Telegram command name", c.name)
		}
		if seen[c.name] {
			t.Errorf("command %q listed twice", c.name)
		}
		seen[c.name] = true
		if c.desc == "" || len(c.desc) > 256 {
			t.Errorf("command %q has a description Telegram would reject: %q", c.name, c.desc)
		}
		if _, ok := commandHandlers[c.name]; !ok {
			t.Errorf("catalog command %q has no handler", c.name)
		}
	}
}

// Every handler a user can reach by typing "/x" must be documented, so /help
// and the "/" menu never fall behind the router.
func TestEveryTypedCommandIsDocumented(t *testing.T) {
	for name := range commandHandlers {
		if name == "text" || name == "photo" {
			continue // synthesized by route, not typed
		}
		if !knownCommand(name) {
			t.Errorf("handler %q is not in commandCatalog", name)
		}
	}
}

// The "/" menu is what every user sees. It stays short — the buttons cover
// the rest — and admin commands never appear in it.
func TestMenuCommandsAreTheShortPublicList(t *testing.T) {
	cmds := menuCommands()
	got := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		got[c.Command] = true
	}

	for _, want := range []string{"start", "menu", "help", "login", "settings", "gallery"} {
		if !got[want] {
			t.Errorf("/%s is not published to the \"/\" menu", want)
		}
	}
	for _, unwanted := range []string{"admin", "adduser", "listusers", "addtelegramid"} {
		if got[unwanted] {
			t.Errorf("admin command %q published to the public menu", unwanted)
		}
	}
	// The point of the menu is that nobody has to scroll it.
	if len(cmds) > 8 {
		t.Errorf("menuCommands returned %d commands; keep the \"/\" list short", len(cmds))
	}
	if len(cmds) >= len(commandCatalog) {
		t.Error("every command is published; the buttons should be covering some of them")
	}
}

func TestHelpPagesListEveryCommand(t *testing.T) {
	help := strings.Join(helpPages(), "\n")
	for _, c := range commandCatalog {
		if !strings.Contains(help, "/"+c.name) {
			t.Errorf("/help does not mention /%s", c.name)
		}
	}
	for _, g := range groupOrder {
		if !strings.Contains(help, string(g)) {
			t.Errorf("/help is missing the %q section", g)
		}
	}
	for _, page := range helpPages() {
		if len(page) > maxMessageLen {
			t.Errorf("help page of %d chars exceeds the message limit", len(page))
		}
	}
}

func TestUnknownCommandHint(t *testing.T) {
	if got := unknownCommandHint("galery"); got != "gallery" {
		t.Errorf("hint for %q = %q, want gallery", "galery", got)
	}
	if got := unknownCommandHint("hel"); got != "help" {
		t.Errorf("hint for %q = %q, want help", "hel", got)
	}
	if got := unknownCommandHint("zzz"); got != "" {
		t.Errorf("hint for an unrelated command = %q, want none", got)
	}
}

// Start publishes the menu; without a transport it must stay a no-op.
func TestPublishCommands(t *testing.T) {
	tb := newTestBot()
	tb.bot.publishCommands(t.Context())
	if len(tb.api.commands) != len(menuCommands()) {
		t.Errorf("published %d commands, want %d", len(tb.api.commands), len(menuCommands()))
	}
}
