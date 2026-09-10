package telegrambot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weldnor/imagegen/internal/auth"
)

// Covers 4.1: the three authentication outcomes.
func TestAuthenticate(t *testing.T) {
	tb := newTestBot()
	ctx := context.Background()

	linked := tb.users.addUser("alice", "alice-pw", 111)
	loggedIn := tb.users.addUser("bob", "bob-pw")
	_, _ = tb.bindings.Create(ctx, 222, loggedIn.id)

	t.Run("linked telegram id, no login", func(t *testing.T) {
		a, ok := tb.bot.authenticate(ctx, 111)
		if !ok || a.userID != linked.id {
			t.Fatalf("authenticate(111) = (%+v, %v), want linked alice", a, ok)
		}
	})
	t.Run("live temporary binding", func(t *testing.T) {
		a, ok := tb.bot.authenticate(ctx, 222)
		if !ok || a.userID != loggedIn.id {
			t.Fatalf("authenticate(222) = (%+v, %v), want logged-in bob", a, ok)
		}
	})
	t.Run("neither linked nor logged in", func(t *testing.T) {
		if _, ok := tb.bot.authenticate(ctx, 333); ok {
			t.Fatal("authenticate(333) should be unauthenticated")
		}
	})
	t.Run("linked takes precedence over a temporary binding", func(t *testing.T) {
		other := tb.users.addUser("carol", "carol-pw")
		if _, err := tb.bindings.Create(ctx, 111, other.id); err != nil {
			t.Fatal(err)
		}
		a, ok := tb.bot.authenticate(ctx, 111)
		if !ok || a.userID != linked.id {
			t.Fatalf("authenticate(111) with conflicting binding = (%+v, %v), want still-linked alice", a, ok)
		}
	})
}

// Covers 4.2: successful /login.
func TestCmdLoginSuccess(t *testing.T) {
	tb := newTestBot()
	tb.users.addUser("alice", "alice-pw")

	msg := textMessage(1, "/login alice alice-pw")
	tb.bot.cmdLogin(context.Background(), msg, "alice alice-pw")

	if _, ok, _ := tb.bindings.Lookup(context.Background(), 1); !ok {
		t.Fatal("no binding created on successful login")
	}
	for _, text := range tb.api.allText(1) {
		if strings.Contains(strings.ToLower(text), "alice-pw") {
			t.Fatalf("password echoed back in reply: %q", text)
		}
	}
	if len(tb.api.deleted) != 1 || tb.api.deleted[0] != 1 {
		t.Fatal("login message was not deleted")
	}
}

// Covers 4.2/4.3: failed login is generic and does not create a binding.
func TestCmdLoginFailure(t *testing.T) {
	tb := newTestBot()
	tb.users.addUser("alice", "alice-pw")

	tb.bot.cmdLogin(context.Background(), textMessage(1, ""), "alice wrong-pw")

	if _, ok, _ := tb.bindings.Lookup(context.Background(), 1); ok {
		t.Fatal("binding created on failed login")
	}
	if got := tb.api.lastText(1); got != "invalid username or password" {
		t.Errorf("reply = %q", got)
	}
}

// Covers 4.3: rate limiting after repeated failures.
func TestCmdLoginRateLimited(t *testing.T) {
	tb := newTestBot()
	tb.users.addUser("alice", "alice-pw")

	for i := 0; i < 11; i++ {
		tb.bot.cmdLogin(context.Background(), textMessage(1, ""), "alice wrong-pw")
	}
	if got := tb.api.lastText(1); got != "too many failed login attempts; try again later" {
		t.Errorf("after repeated failures, reply = %q", got)
	}
}

// Covers 4.4: /logout on an unlinked (temporarily logged-in) chat.
func TestCmdLogoutClearsBinding(t *testing.T) {
	tb := newTestBot()
	ctx := context.Background()
	usr := tb.users.addUser("bob", "bob-pw")
	tb.bindings.Create(ctx, 1, usr.id)
	tb.bot.mu.Lock()
	tb.bot.state(1).aspectRatio = "16:9"
	tb.bot.mu.Unlock()

	tb.bot.cmdLogout(ctx, textMessage(1, "/logout"), "")

	if _, ok, _ := tb.bindings.Lookup(ctx, 1); ok {
		t.Fatal("binding still present after logout")
	}
	if got := tb.bot.activeAspect(1); got != "" {
		t.Errorf("aspect ratio not cleared on logout: %q", got)
	}
}

// Covers 4.4: /logout on a linked chat stays authenticated.
func TestCmdLogoutOnLinkedChat(t *testing.T) {
	tb := newTestBot()
	ctx := context.Background()
	tb.users.addUser("alice", "alice-pw", 111)

	tb.bot.cmdLogout(ctx, textMessage(111, "/logout"), "")

	if _, ok := tb.bot.authenticate(ctx, 111); !ok {
		t.Fatal("linked chat lost authentication after /logout")
	}
	if got := tb.api.lastText(111); !strings.Contains(strings.ToLower(got), "linked") {
		t.Errorf("reply = %q, want it to mention the chat is linked", got)
	}
}

// Covers 4.4: the auth-gate on protected commands.
func TestRequireAuthGate(t *testing.T) {
	tb := newTestBot()
	ctx := context.Background()

	// Unauthenticated.
	if _, ok := tb.bot.requireAuth(ctx, 1); ok {
		t.Fatal("unauthenticated chat treated as authenticated")
	}
	if got := tb.api.lastText(1); !strings.Contains(got, "/login") {
		t.Errorf("reply = %q, want it to mention /login", got)
	}

	// Logged in.
	usr := tb.users.addUser("bob", "bob-pw")
	tb.bindings.Create(ctx, 2, usr.id)
	if _, ok := tb.bot.requireAuth(ctx, 2); !ok {
		t.Fatal("logged-in chat rejected")
	}

	// Linked.
	tb.users.addUser("alice", "alice-pw", 3)
	if _, ok := tb.bot.requireAuth(ctx, 3); !ok {
		t.Fatal("linked chat rejected")
	}
}

// Covers 4.5: an expired binding is treated as unauthenticated.
func TestExpiredBindingIsUnauthenticated(t *testing.T) {
	tb := newTestBot()
	ctx := context.Background()
	usr := tb.users.addUser("bob", "bob-pw")

	tb.bindings.mu.Lock()
	tb.bindings.byID[1] = auth.TelegramBinding{ChatID: 1, UserID: usr.id, ExpiresAt: time.Now().Add(-time.Hour)}
	tb.bindings.mu.Unlock()

	if _, ok := tb.bot.authenticate(ctx, 1); ok {
		t.Fatal("expired binding treated as live")
	}
}
