## Context

See proposal.md - Why. The backend already exposes generation
(`internal/openrouter`), per-user gallery storage (`internal/gallery`), and
credential/session auth (`internal/auth`) behind an HTTP API
(`internal/httpapi`). All configuration is env-var driven and validated at
startup (`internal/config`); there is one Postgres database and one image
volume, single instance, no horizontal-scaling story (see README §Production
notes). The bot must fit that same operational model rather than introduce a
second source of truth for users or images.

## Goals / Non-Goals

**Goals:**
- Reuse the existing `Generator`, `GalleryStore`, and credential-check logic
  directly (in-process Go calls), not by having the bot call the HTTP API
  over the network as a client.
- Keep the bot's per-chat state (bound user, active model/options) simple
  enough to survive a process restart without a new migration if possible.
- Keep `TELEGRAM_BOT_TOKEN` optional: omitting it disables the bot with no
  effect on the HTTP server.

**Non-Goals:**
- No Telegram-native account linking (OAuth, deep links) - login stays
  username/password via `/login`, matching the web UI's only auth method.
- No group-chat support beyond what Telegram's Bot API gives for free; the
  spec's scenarios assume one user per authenticated chat (DMs).
- No changes to `internal/httpapi`'s routes or the web UI.

## Decisions

**In-process integration, not an HTTP client.** The bot binds directly to
the same `Generator`, `GalleryStore`, and `*auth.Users` types the HTTP
handlers use (`internal/httpapi.API` already models this via the
`Generator`/`GalleryStore` interfaces). Alternative considered: have the bot
call `POST /api/generate` etc. over HTTP like any other client. Rejected
because it would require the bot to also manage an HTTP session cookie and a
second network hop for no benefit - both processes already run against the
same DB and file volume, and being in-process avoids serializing image bytes
through JSON/base64 twice.

**Single binary, opt-in via config, not a separate deployment.** The bot
starts inside `cmd/server` (same process as the HTTP server) when
`TELEGRAM_BOT_TOKEN` is set, using the same `internal/config.Config` and the
same `pgxpool.Pool`/`gallery.Store` instances constructed at startup.
Alternative considered: a separate `cmd/telegrambot` binary/container.
Rejected for now - it would duplicate startup wiring (DB pool, migrations,
gallery store) and add a second container to docker-compose.yml for a
capability that shares 100% of its backend state with the existing service;
revisit only if the bot's load or lifecycle needs to scale independently.

**Per-chat auth binding stored in Postgres, TTL like sessions.** A new table
`telegram_bindings (chat_id PK, user_id, created_at, expires_at)` records
which app user a chat is bound to, with the same `SESSION_TTL` used for web
sessions. Alternative considered: reuse `internal/auth`'s existing session
table by minting a real session per chat. Rejected because sessions are
modeled around cookies (`internal/auth/session.go`); a distinct small table
keyed by Telegram chat ID is simpler and makes the binding's meaning
explicit at the schema level. The chat's active model/options
(model/aspect/size/count) are kept in-memory only (map keyed by chat ID,
guarded by a mutex) - they are cheap to re-select and losing them on a
process restart is an acceptable trade-off (Non-Goal-adjacent) versus a
second migration + table for ephemeral preferences.

**Long polling, not a webhook.** `getUpdates` long polling avoids requiring
a public HTTPS endpoint/TLS cert wiring beyond what Caddy already terminates
for the web UI, and matches the "single instance" deployment model. A
webhook would need a dedicated route in `internal/httpapi` and Telegram-side
setup; long polling needs neither. Revisit if multi-instance deployment ever
happens (long polling requires exactly one poller).

**Library: a maintained Go Telegram Bot API wrapper** (e.g.
`go-telegram-bot-api/telegram-bot-api` or `go-telegram/bot`) rather than
hand-rolling HTTP calls to `api.telegram.org`. Final pick happens in
tasks.md/implementation; either gives typed updates, message/photo sending,
and update polling, and avoids re-implementing multipart photo upload and
Telegram's update-offset bookkeeping.

## Risks / Trade-offs

- **Password sent in plaintext chat text** → `/login` messages carry a
  password in cleartext Telegram message. Telegram's client-server
  transport is TLS, and the bot deletes the login message where the bot API
  permits (spec scenario), matching the risk profile of the web login form
  auto-filled from a password manager. Users on shared/group chats should
  not `/login` there; documentation will call this out.
- **In-memory chat option state lost on restart** → a chat's selected
  model/aspect/size/count reverts to defaults after a deploy. Mitigation:
  cheap to reselect via `/model`, `/aspect`, `/size`, `/count`; the bound
  user (in Postgres) is not lost, so `/generate` still works with defaults.
- **Shared `OPENROUTER_API_KEY` spend, no per-user quota** → already true
  for the web UI (README §Production notes); the bot does not make this
  worse, but it does add a second surface generating spend against the same
  key.
- **Long-running polling process** → `getUpdates` runs in a goroutine for
  the process lifetime; a panic in the bot's update loop must not take down
  the HTTP server. Mitigation: run the poller in its own goroutine with a
  recover, matching the `recoverer` middleware pattern already used in
  `internal/httpapi`.

## Migration Plan

- Add a new migration for `telegram_bindings` (Postgres, embedded and
  auto-applied on startup like `migrations/0001_init.sql`).
- New env var `TELEGRAM_BOT_TOKEN` (optional) documented in `.env.example`
  and README; startup does not fail when it is unset, the bot simply does
  not start.
- No changes to existing tables, routes, or the web UI; rollback is deleting
  the new table and unsetting the token.
