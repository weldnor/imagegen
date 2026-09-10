package telegrambot

import (
	"context"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

// Covers 3.1: Bot construction wires the given collaborators and compiles.
func TestNewBotConstruction(t *testing.T) {
	tb := newTestBot()
	if tb.bot == nil {
		t.Fatal("newBot returned nil")
	}
	if tb.bot.adminPassword != "adminpw" {
		t.Errorf("adminPassword = %q", tb.bot.adminPassword)
	}
}

// NewBot (the production constructor) must not touch the network: a bogus
// token still succeeds because construction only sets up local state.
func TestNewBotDoesNotCallNetwork(t *testing.T) {
	b, err := NewBot(Config{Token: "123:not-a-real-token", AdminPassword: "x"},
		&fakeGen{fn: okImage}, newFakeGallery(), newFakeUsers(), newFakeBindings())
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	if b == nil {
		t.Fatal("NewBot returned nil")
	}
}

// Covers 3.2: a panic in a handler is recovered and does not escape / crash
// the process (or deadlock the caller).
func TestHandleUpdateSafelyRecoversPanic(t *testing.T) {
	b := newTestBot().bot
	commandHandlers["__panic_test__"] = func(*Bot, context.Context, *models.Message, string) {
		panic("boom")
	}
	defer delete(commandHandlers, "__panic_test__")

	msg := &models.Message{ID: 1, Chat: models.Chat{ID: 1}, Text: "/__panic_test__"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.handleUpdateSafely(context.Background(), &models.Update{Message: msg})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleUpdateSafely did not return; panic escaped or deadlocked")
	}
}
