## Context

See proposal.md - Why. The backend already exposes generation
(`internal/openrouter`), per-user gallery storage (`internal/gallery`), and
credential/session auth (`internal/auth`) behind an HTTP API
(`internal/httpapi`). All configuration is env-var driven and validated at
startup (`internal/config`); there is one Postgres database and one image
volume, single instance, no horizontal-scaling story (see README §Production
notes). The bot must fit that same operational model rather than introduce a
second source of truth for users or images. Today, app users come entirely
from the static `AUTH_USERS` env var; this change moves them into Postgres
and adds the bot as the only interface for provisioning them.

## Goals / Non-Goals

**Goals:**
- Reuse the existing `Generator`, `GalleryStore`, and credential-check logic
  directly (in-process Go calls), not by having the bot call the HTTP API
  over the network as a client.
- Keep the bot's ephemeral per-chat state (admin authorization, active
  model/options, a temporary `/login` binding) simple enough to survive a
  process restart without a new migration where possible.
- Store app users in Postgres so both the web UI and the bot authenticate
  against the same table, and so a user can be provisioned without a
  redeploy.
- Let a chat whose Telegram ID an admin has linked to a user skip `/login`
  entirely on every message.
- Structure the codebase around `uber-fx`: every `internal/*` package
  (existing and new) exposes a `module.go` with an `fx.Module`, and
  `cmd/server` composes the app from those modules.

**Non-Goals:**
- No Telegram-native account linking (OAuth, deep links) - an admin links a
  Telegram ID explicitly via `/addtelegramid` or at `/adduser` time.
- No web-based admin UI for user management - the bot's admin commands are
  the only interface; the web UI keeps its existing login form only.
- No group-chat support beyond what Telegram's Bot API gives for free; the
  spec's scenarios assume one user per authenticated chat (DMs).
- No changes to `internal/httpapi`'s routes.
- No behavior change to existing endpoints from the `uber-fx` wiring
  refactor - it is a mechanical restructuring of how the app is
  constructed, not a change to what it does.

## Decisions

**In-process integration, not an HTTP client.** The bot binds directly to
the same `Generator`, `GalleryStore`, and user-store types the HTTP
handlers use (`internal/httpapi.API` already models this via the
`Generator`/`GalleryStore` interfaces). Alternative considered: have the bot
call `POST /api/generate` etc. over HTTP like any other client. Rejected
because it would require the bot to also manage an HTTP session cookie and a
second network hop for no benefit - both processes already run against the
same DB and file volume, and being in-process avoids serializing image bytes
through JSON/base64 twice.

**Single binary, opt-in via config, not a separate deployment.** The bot
starts inside `cmd/server` (same process as the HTTP server). Alternative
considered: a separate `cmd/telegrambot` binary/container. Rejected - it
would duplicate startup wiring (DB pool, migrations, gallery store) and add
a second container to docker-compose.yml for a capability that shares 100%
of its backend state with the existing service; revisit only if the bot's
load or lifecycle needs to scale independently.

**Dependency injection with `uber-fx`, one `fx.Module` per domain
package.** Every `internal/*` package (`config`, `db`, `auth`, `gallery`,
`openrouter`, `httpapi`, and the new `telegrambot`) gets a `module.go`
declaring an `fx.Module(...)` with that package's providers; `cmd/server`
builds the app with `fx.New(config.Module, db.Module, auth.Module, ...)`
plus `fx.Invoke` calls to start the HTTP server and the bot's polling loop.
Alternative considered: keep the current hand-written wiring in
`cmd/server/run.go`. Rejected - this change already adds a second
long-running component with its own start/stop semantics (the bot's
polling loop); `run.go` would otherwise grow a second bespoke lifecycle
block next to the HTTP server's. `fx.Lifecycle` (`OnStart`/`OnStop`) gives
both components the same, tested startup/shutdown mechanism instead of two
different hand-rolled ones. The bot's module is only included in the graph
when `TELEGRAM_BOT_TOKEN` is configured (see below - now always required,
so this reduces to "always included," but the module boundary stays clean
either way).

**DB-backed users, `AUTH_USERS` removed.** A new `users` table
(`id`, `username` unique, `password_hash`, `created_at`) replaces the
`AUTH_USERS` env var as the credential source for both `POST /api/login`
and the bot's `/login`. `internal/auth`'s user lookup moves from an
in-memory map built at startup from config to a query against this table.
The `-gen-hash` CLI subcommand is removed - it existed only to prepare an
`AUTH_USERS` entry, and passwords are now hashed by the bot itself when an
admin runs `/adduser`. Alternative considered: keep `AUTH_USERS` as a
fallback/seed source. Rejected - a single source of truth avoids drift
between two credential stores, and the admin bot commands are a strictly
more flexible replacement (no redeploy to add a user).

**Admin authorization is a shared password, not a per-user flag.** A
required `ADMIN_PASSWORD` env var gates `/admin`. A chat that presents it
correctly is authorized as admin for that chat only, held in-memory with
the same TTL pattern as the chat's ephemeral option state (not persisted -
lost on restart, cheap to re-enter). Alternative considered: an `is_admin`
column on `users`. Rejected - that introduces a bootstrapping problem (who
creates the first admin-flagged user, and how, once `AUTH_USERS` is gone?)
that a single shared secret configured alongside `TELEGRAM_BOT_TOKEN`
avoids entirely.

**Automatic authentication via linked Telegram ID; `/login` stays for
unlinked chats.** A new `user_telegram_ids` table (`telegram_id` unique,
`user_id`, `created_at`) is checked on every incoming update before
anything else: a match authenticates the chat as that user with no
expiry, since it's a durable admin-created link rather than a session. A
chat with no match falls back to the existing temporary `/login` binding
(kept in Postgres - `telegram_bindings (chat_id PK, user_id, created_at,
expires_at)` - with `SESSION_TTL`, exactly as originally designed for
per-chat login) so a not-yet-linked user still has a way in. Alternative
considered: require `/login` even for linked chats, treating the link as
metadata only. Rejected - defeats the purpose stated in the proposal; the
whole point of linking is to skip that step.

**`TELEGRAM_BOT_TOKEN` and `ADMIN_PASSWORD` become required.** This
reverses the earlier design's "token is optional" stance: once
`AUTH_USERS` is gone, `/adduser` is the *only* way to provision an account
(web UI included), so a deployment without the bot enabled would have no
way to create any user at all. Both are added to
`config.RequiredEnvVarNames()`; startup fails fast if either is missing,
consistent with how `OPENROUTER_API_KEY`/`DATABASE_URL` are already
treated.

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

- **Bot is now on the critical path for provisioning** → if
  `TELEGRAM_BOT_TOKEN` is misconfigured or the token is revoked after
  deploy, no new user can be created (existing logins/links keep working
  since they don't depend on the bot being reachable at that moment).
  Mitigation: fail fast at startup if the token or `ADMIN_PASSWORD` looks
  missing/empty, same as other required config.
- **Shared admin password compromise** → whoever holds `ADMIN_PASSWORD` can
  create users and link Telegram IDs. Mitigation: same risk class as
  `AUTH_USERS` today (a shared secret in env); rate-limit `/admin` attempts
  like `/login`; document that rotating it is a config change + restart.
- **Password sent in plaintext chat text** → `/login` and `/admin` messages
  carry a password in cleartext Telegram message text; `/adduser` carries a
  new user's password the same way. Telegram's client-server transport is
  TLS, and the bot deletes these messages where the Bot API permits (spec
  scenarios). Users should not run these commands in a shared/group chat;
  documentation will call this out.
- **In-memory admin authorization and chat option state lost on restart** →
  reverts to defaults after a deploy; cheap to redo (`/admin`, `/model`,
  `/aspect`, `/size`, `/count`). Linked-Telegram-ID authentication and the
  `users` table are unaffected, since those live in Postgres.
- **Shared `OPENROUTER_API_KEY` spend, no per-user quota** → already true
  for the web UI (README §Production notes); the bot does not make this
  worse, but it does add a second surface generating spend against the same
  key.
- **Long-running polling process** → `getUpdates` runs in a goroutine for
  the process lifetime; a panic in the bot's update loop must not take down
  the HTTP server. Mitigation: run the poller under its own `fx.Lifecycle`
  hook with panic recovery, matching the `recoverer` middleware pattern
  already used in `internal/httpapi`.
- **`uber-fx` refactor touches already-working wiring code** → converting
  every existing package to a `module.go` is a broad diff with no behavior
  change intended, so a mistake there risks regressing the working HTTP
  server. Mitigation: convert one package at a time, run `go test ./...`
  after each, and keep `cmd/server`'s externally observable behavior
  (routes, config validation, exit codes) identical throughout.

## Migration Plan

- Add a migration creating `users`, `user_telegram_ids`, and
  `telegram_bindings` (Postgres, embedded and auto-applied on startup like
  `migrations/0001_init.sql`).
- Remove `AUTH_USERS` from `internal/config`; add required
  `TELEGRAM_BOT_TOKEN` and `ADMIN_PASSWORD`. Update `.env.example` and
  README to match, replacing the `-gen-hash`/`AUTH_USERS` bootstrap
  instructions with: set `ADMIN_PASSWORD` and `TELEGRAM_BOT_TOKEN`, deploy,
  then message the bot `/admin <password>` followed by `/adduser <username>
  <password> [telegram_id...]` to create the first account.
- Remove the `cmd/server -gen-hash` subcommand.
- Convert `internal/config`, `internal/db`, `internal/auth`,
  `internal/gallery`, `internal/openrouter`, and `internal/httpapi` to
  `fx.Module`s one at a time, keeping `go test ./...` green throughout;
  then add `internal/telegrambot`'s module and wire both the HTTP server
  and the bot's polling loop through `fx.Lifecycle` in `cmd/server`.
- Existing deployments must set `ADMIN_PASSWORD` and `TELEGRAM_BOT_TOKEN`
  and re-provision their users via `/adduser` after upgrading - this is a
  breaking change with no automatic `AUTH_USERS` import, since the whole
  point is to move off static, redeploy-only credentials. Rollback is
  reverting the deploy and restoring the previous `AUTH_USERS` value (the
  new tables are additive and harmless to leave in place).
