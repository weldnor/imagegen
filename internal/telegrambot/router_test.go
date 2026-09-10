package telegrambot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

// Covers 3.3: table-driven mapping from a sample update to the expected
// handler (command name route resolves to).
func TestRoute(t *testing.T) {
	cases := []struct {
		name     string
		msg      *models.Message
		wantCmd  string
		wantArgs string
	}{
		{"admin", &models.Message{Text: "/admin secret"}, "admin", "secret"},
		{"adduser", &models.Message{Text: "/adduser alice pw 123"}, "adduser", "alice pw 123"},
		{"addtelegramid", &models.Message{Text: "/addtelegramid alice 123"}, "addtelegramid", "alice 123"},
		{"listusers", &models.Message{Text: "/listusers"}, "listusers", ""},
		{"login", &models.Message{Text: "/login alice pw"}, "login", "alice pw"},
		{"logout", &models.Message{Text: "/logout"}, "logout", ""},
		{"models", &models.Message{Text: "/models"}, "models", ""},
		{"model", &models.Message{Text: "/model google/gemini-2.5-flash-image"}, "model", "google/gemini-2.5-flash-image"},
		{"aspect", &models.Message{Text: "/aspect 16:9"}, "aspect", "16:9"},
		{"size", &models.Message{Text: "/size 2K"}, "size", "2K"},
		{"count", &models.Message{Text: "/count 3"}, "count", "3"},
		{"generate", &models.Message{Text: "/generate a cat"}, "generate", "a cat"},
		{"generate with bot suffix", &models.Message{Text: "/generate@MyBot a cat"}, "generate", "a cat"},
		{"gallery", &models.Message{Text: "/gallery"}, "gallery", ""},
		{"delete", &models.Message{Text: "/delete abc-123"}, "delete", "abc-123"},
		{"clear", &models.Message{Text: "/clear"}, "clear", ""},
		{"plain text", &models.Message{Text: "a dog on the moon"}, "text", "a dog on the moon"},
		{"photo with caption", &models.Message{Photo: []models.PhotoSize{{FileID: "f1", Width: 10, Height: 10}}, Caption: "a cat"}, "photo", "a cat"},
		{"photo without caption", &models.Message{Photo: []models.PhotoSize{{FileID: "f1", Width: 10, Height: 10}}}, "photo", ""},
		{"empty message ignored", &models.Message{}, "", ""},
		{"unknown command falls through to handler lookup", &models.Message{Text: "/nope"}, "nope", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args := route(tc.msg)
			if cmd != tc.wantCmd || args != tc.wantArgs {
				t.Errorf("route() = (%q, %q), want (%q, %q)", cmd, args, tc.wantCmd, tc.wantArgs)
			}
		})
	}
}

// TestCommandHandlersCoverEveryRoutableCommand verifies every command route
// can produce (other than an unregistered one like "nope" above) has a
// registered handler, so dispatch does not silently drop a known command.
func TestCommandHandlersCoverEveryRoutableCommand(t *testing.T) {
	want := []string{
		"admin", "adduser", "addtelegramid", "listusers",
		"login", "logout",
		"models", "model", "aspect", "size", "count",
		"generate", "gallery", "delete", "clear",
		"text", "photo",
	}
	for _, cmd := range want {
		if _, ok := commandHandlers[cmd]; !ok {
			t.Errorf("no handler registered for command %q", cmd)
		}
	}
}
