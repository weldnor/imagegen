## Why

Imagen is currently only reachable through its web UI, and its user list is a
static `AUTH_USERS` environment variable that requires a redeploy to change.
A Telegram bot front-end, backed by the same server, lets users generate
images, pick a model, and browse their gallery directly from a chat they
already have open, and lets an operator manage who can log in — including
linking a user's Telegram account for automatic authentication — without
touching the deployment.

## What Changes

- Add a Telegram bot that talks to Telegram's Bot API (long polling) and
  drives the existing backend's internal collaborators directly (in-process,
  see design.md) instead of re-implementing generation or storage. It runs
  inside the existing server process.
- App users move from the `AUTH_USERS` environment variable to a Postgres
  `users` table. **BREAKING**: `AUTH_USERS` and the `-gen-hash` CLI
  subcommand are removed; both the web UI's `POST /api/login` and the bot's
  `/login` authenticate against the database. Provisioning the first users
  after a deploy is done through the bot's admin commands (below), so
  `TELEGRAM_BOT_TOKEN` becomes **required**, not optional.
- New admin capability, gated by a shared `ADMIN_PASSWORD`: `/admin
  <password>` authorizes a chat as admin (for that chat, temporarily);
  `/adduser <username> <password> [telegram_id...]` creates a user and
  optionally links one or more Telegram IDs to it; `/addtelegramid
  <username> <telegram_id>` links an additional Telegram ID to an existing
  user; `/listusers` lists users and their linked Telegram IDs.
- A chat whose Telegram ID has been linked to a user by an admin is
  automatically authenticated as that user on every message — no `/login`
  needed. A chat that isn't linked still authenticates with `/login
  <username> <password>` (checked against the `users` table); `/logout`
  clears that temporary binding.
- `/generate <prompt>` (and a plain text message once authenticated)
  generates an image with the chat's currently selected model, aspect
  ratio, and image size, mirroring `POST /api/generate` — including sending
  multiple images when a count > 1 is set, and attaching a replied-to/sent
  photo as a reference image for models that support image input.
- `/models` lists the same model catalog as the web UI (`GET /api/models`)
  and `/model <id>` selects one for the chat; `/aspect <ratio>`, `/size
  <size>`, and `/count <n>` set the remaining generation options a model
  supports, mirroring the web UI's controls.
- `/gallery` browses the chat's previously generated images (paged), each
  with a delete action; `/delete <id>` and `/clear` mirror
  `DELETE /api/images/{id}` and `DELETE /api/images`.
- New configuration: `TELEGRAM_BOT_TOKEN` and `ADMIN_PASSWORD` (both
  required); `AUTH_USERS` removed; reuses `DATABASE_URL`,
  `IMAGE_STORAGE_DIR`, `OPENROUTER_*` from the existing backend config.
- Internal wiring is restructured around `uber-fx`: each `internal/*`
  package (existing ones and the new bot package) gets a `module.go`
  declaring its `fx.Module`, and `cmd/server` assembles the app by composing
  those modules instead of constructing everything by hand in `run.go`.

## Capabilities

### New Capabilities
- `telegram-bot`: chat-based access to image generation, model/option
  selection, and gallery browsing over the Telegram Bot API; admin-gated
  user management (create user, link Telegram IDs, list users); and
  automatic per-chat authentication for a linked Telegram ID.

### Modified Capabilities
None. There is no archived main spec yet for authentication or user
storage to diff against (the credential-source change from `AUTH_USERS` to
a database table, and the removal of `-gen-hash`, are captured as Impact
below and in design.md rather than as a capability delta).

## Impact

- New `internal/telegrambot` package (Telegram update handling, command
  routing, per-chat state) plus a `module.go` per existing `internal/*`
  package for `uber-fx` wiring; `cmd/server` composes the app via `fx.New`.
- `internal/config`: remove `AUTH_USERS`; add required `TELEGRAM_BOT_TOKEN`
  and `ADMIN_PASSWORD`; `.env.example` and README updated to match.
- `internal/auth`: credential lookup moves from an in-memory map built from
  config to queries against a new `users` table; `cmd/server -gen-hash` is
  removed (the bot hashes a new user's password itself).
- New migration: `users` and `user_telegram_ids` tables.
- New dependencies: a Telegram Bot API client library, `go.uber.org/fx`.
- Reuses `internal/openrouter` (generation, model catalog) and
  `internal/gallery` (persistence) unchanged.
- Per-chat ephemeral state (admin authorization, selected
  model/aspect/size/count, temporary `/login` binding for an unlinked chat)
  needs a home — addressed in design.md.
