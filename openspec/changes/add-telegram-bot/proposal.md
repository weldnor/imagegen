## Why

Imagen is currently only reachable through its web UI. Users who live in
Telegram have to switch context to a browser tab to generate or browse
images. A Telegram bot front-end, backed by the same server, lets users
generate images, pick a model, and browse their gallery directly from a chat
they already have open — without duplicating the OpenRouter integration,
storage, or per-user access model.

## What Changes

- Add a Telegram bot process (`cmd/telegrambot`) that talks to Telegram's Bot
  API (long polling) and drives the existing backend's internal APIs instead
  of re-implementing generation or storage.
- `/login <username> <password>` authenticates a chat against the existing
  `AUTH_USERS` credentials and binds the Telegram chat to that app user for
  subsequent commands; `/logout` clears the binding.
- `/generate <prompt>` (and a plain text message once logged in) generates an
  image with the chat's currently selected model, aspect ratio, and image
  size, mirroring `POST /api/generate` — including sending multiple images
  when a count > 1 is set, and attaching a replied-to/sent photo as a
  reference image for models that support image input.
- `/models` lists the same model catalog as the web UI (`GET /api/models`)
  and `/model <id>` selects one for the chat; `/aspect <ratio>`, `/size
  <size>`, and `/count <n>` set the remaining generation options a model
  supports, mirroring the web UI's controls.
- `/gallery` browses the chat's previously generated images (paged), each
  with a delete action; `/delete <id>` and `/clear` mirror
  `DELETE /api/images/{id}` and `DELETE /api/images`.
- New configuration: `TELEGRAM_BOT_TOKEN` (required to enable the bot) and
  reuses `AUTH_USERS`, `DATABASE_URL`, `IMAGE_STORAGE_DIR`,
  `OPENROUTER_*` from the existing backend config.

## Capabilities

### New Capabilities
- `telegram-bot`: chat-based access to image generation, model/option
  selection, and gallery browsing over the Telegram Bot API, authenticated
  against the existing per-user credentials.

### Modified Capabilities
None. The bot is a new client of the existing authentication, image
generation, and gallery capabilities; it does not change their requirements.

## Impact

- New `cmd/telegrambot` binary/entrypoint and a new `internal/telegrambot`
  package (Telegram update handling, per-chat state, command routing).
- `internal/config`: new `TELEGRAM_BOT_TOKEN` variable (optional — the bot is
  disabled when unset) plus a `TELEGRAM_BOT_ENABLED`-style flag if run inside
  the same process as the HTTP server; `.env.example` and README updated to
  match.
- New dependency: a Telegram Bot API client library.
- Reuses `internal/auth` (credential check, no new session storage
  necessarily — see design.md), `internal/openrouter` (generation, model
  catalog), and `internal/gallery` (persistence) unchanged.
- Per-chat state (bound user, selected model/aspect/size/count) needs a home —
  addressed in design.md.
