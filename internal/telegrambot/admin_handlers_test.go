package telegrambot

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Covers 5.1: successful/failed /admin, expiry, and rate limiting.
func TestCmdAdmin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tb := newTestBot()
		tb.bot.cmdAdmin(context.Background(), textMessage(1, ""), "adminpw")
		if !tb.bot.isAdmin(1) {
			t.Fatal("chat not admin-authorized after correct password")
		}
		if len(tb.api.deleted) != 1 {
			t.Fatal("/admin message was not deleted")
		}
	})

	t.Run("failure", func(t *testing.T) {
		tb := newTestBot()
		tb.bot.cmdAdmin(context.Background(), textMessage(1, ""), "wrong")
		if tb.bot.isAdmin(1) {
			t.Fatal("chat authorized with wrong password")
		}
		if got := tb.api.lastText(1); got != "incorrect admin password" {
			t.Errorf("reply = %q", got)
		}
	})

	t.Run("expiry", func(t *testing.T) {
		tb := newTestBot()
		tb.bot.adminTTL = -time.Second // already expired once set
		tb.bot.cmdAdmin(context.Background(), textMessage(1, ""), "adminpw")
		if tb.bot.isAdmin(1) {
			t.Fatal("admin authorization should have already expired")
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		tb := newTestBot()
		for i := 0; i < 11; i++ {
			tb.bot.cmdAdmin(context.Background(), textMessage(1, ""), "wrong")
		}
		if got := tb.api.lastText(1); !strings.Contains(got, "too many failed attempts") {
			t.Errorf("reply = %q", got)
		}
	})
}

// Covers 5.2: admin-gated commands reject an unauthorized chat.
func TestAdminGateRejectsUnauthorized(t *testing.T) {
	cases := []struct {
		name string
		run  func(tb *testBot)
	}{
		{"adduser", func(tb *testBot) { tb.bot.cmdAddUser(context.Background(), textMessage(1, ""), "alice pw") }},
		{"addtelegramid", func(tb *testBot) { tb.bot.cmdAddTelegramID(context.Background(), textMessage(1, ""), "alice 1") }},
		{"listusers", func(tb *testBot) { tb.bot.cmdListUsers(context.Background(), textMessage(1, ""), "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb := newTestBot()
			tc.run(tb)
			if got := tb.api.lastText(1); !strings.Contains(got, "/admin") {
				t.Errorf("reply = %q, want it to ask for /admin", got)
			}
		})
	}
}

func adminChat(tb *testBot, chatID int64) {
	tb.bot.mu.Lock()
	tb.bot.state(chatID).adminUntil = time.Now().Add(time.Hour)
	tb.bot.mu.Unlock()
}

// Covers 5.3: creating a user, with and without linked Telegram IDs; duplicate
// username and an already-linked Telegram ID are rejected.
func TestCmdAddUser(t *testing.T) {
	t.Run("success without telegram ids", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.bot.cmdAddUser(context.Background(), textMessage(1, ""), "alice alice-pw")
		if _, ok, _ := tb.users.VerifyPassword(context.Background(), "alice", "alice-pw"); !ok {
			t.Fatal("user was not created")
		}
	})

	t.Run("success with telegram ids", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.bot.cmdAddUser(context.Background(), textMessage(1, ""), "alice alice-pw 111 222")
		if _, _, ok, _ := tb.users.LookupByTelegramID(context.Background(), 111); !ok {
			t.Fatal("telegram id 111 not linked")
		}
		if _, _, ok, _ := tb.users.LookupByTelegramID(context.Background(), 222); !ok {
			t.Fatal("telegram id 222 not linked")
		}
	})

	t.Run("duplicate username rejected", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.users.addUser("alice", "existing")
		tb.bot.cmdAddUser(context.Background(), textMessage(1, ""), "alice alice-pw")
		if got := tb.api.lastText(1); !strings.Contains(got, "already taken") {
			t.Errorf("reply = %q", got)
		}
	})

	t.Run("linked telegram id rejected", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.users.addUser("bob", "bob-pw", 111)
		tb.bot.cmdAddUser(context.Background(), textMessage(1, ""), "alice alice-pw 111")
		if got := tb.api.lastText(1); !strings.Contains(got, "already linked") {
			t.Errorf("reply = %q", got)
		}
		if _, ok, _ := tb.users.VerifyPassword(context.Background(), "alice", "alice-pw"); ok {
			t.Fatal("user should not have been created")
		}
	})
}

// Covers 5.4: linking a Telegram ID to an existing user; unknown user and an
// already-linked ID are rejected.
func TestCmdAddTelegramID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.users.addUser("alice", "alice-pw")
		tb.bot.cmdAddTelegramID(context.Background(), textMessage(1, ""), "alice 999")
		if _, _, ok, _ := tb.users.LookupByTelegramID(context.Background(), 999); !ok {
			t.Fatal("telegram id not linked")
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.bot.cmdAddTelegramID(context.Background(), textMessage(1, ""), "nobody 999")
		if got := tb.api.lastText(1); !strings.Contains(got, "not found") {
			t.Errorf("reply = %q", got)
		}
	})

	t.Run("already linked", func(t *testing.T) {
		tb := newTestBot()
		adminChat(tb, 1)
		tb.users.addUser("alice", "alice-pw", 999)
		tb.users.addUser("bob", "bob-pw")
		tb.bot.cmdAddTelegramID(context.Background(), textMessage(1, ""), "bob 999")
		if got := tb.api.lastText(1); !strings.Contains(got, "already linked") {
			t.Errorf("reply = %q", got)
		}
	})
}

// Covers 5.5: listing users, paged to fit Telegram message limits.
func TestCmdListUsersPages(t *testing.T) {
	tb := newTestBot()
	adminChat(tb, 1)
	for i := 0; i < 500; i++ {
		tb.users.addUser("user-"+strconv.Itoa(i)+"-"+strings.Repeat("x", 10), "pw")
	}
	tb.bot.cmdListUsers(context.Background(), textMessage(1, ""), "")
	if len(tb.api.allText(1)) < 2 {
		t.Fatalf("expected multiple pages, got %d messages", len(tb.api.allText(1)))
	}
}
