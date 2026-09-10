# Imagen

Internal AI image-generation tool. A small Go backend serves a vanilla
HTML/CSS/JS frontend and a Telegram bot, proxies generation requests to
[OpenRouter](https://openrouter.ai), and stores every generated image per user.

This repository started as the fully client-side
`yusufipk/imagen-openrouter` tool. It now has a backend so that:

- the OpenRouter API key lives only on the server — never in the browser;
- access is gated by a per-user login (web: username + password and a
  server-side session; Telegram: `/login` or a linked Telegram account) backed
  by the same `users` table;
- generated images persist server-side (Postgres metadata + a file volume),
  scoped per user, instead of the browser's IndexedDB;
- users are provisioned through the Telegram bot's admin commands — there is
  no other account-creation path.

## Architecture

```
browser ──HTTP──▶ Go service ──HTTPS──▶ OpenRouter
                    │            ▲
Telegram ──long-poll┘            │
                    │
                    ├─ serves the embedded frontend (/, /src/*, /favicon.svg)
                    ├─ POST /api/login · GET /api/session · POST /api/logout
                    ├─ POST /api/generate           (proxy + persist)
                    ├─ GET/DELETE /api/images[/{id}] (per-user gallery)
                    ├─ GET /api/models · GET /healthz
                    ├─ Telegram bot: same generation/gallery/auth logic,
                    │   driven in-process (see "Telegram bot" below)
                    │
                    ├─ PostgreSQL   image + user + session + telegram-link metadata
                    └─ file volume  image bytes at IMAGE_STORAGE_DIR/<user>/<id>.<ext>
```

- One binary, one process: the HTTP server and the Telegram bot's polling
  loop run side by side, wired together with `uber-fx` (each `internal/*`
  package exposes an `fx.Module`; `cmd/server` composes them in `run.go`).
  The frontend is embedded via `go:embed` (override with `STATIC_DIR` for
  local frontend edits).
- Auth is an opaque, DB-backed session cookie for the web UI (`HttpOnly`,
  `SameSite=Lax`), and a linked-Telegram-ID or temporary `/login` binding for
  the bot. There is no route-level HTTP Basic Auth and no API-key field in
  the UI.
- Database migrations are embedded and run automatically on startup.

## Quick start (Docker Compose)

```sh
cp .env.example .env
# edit .env:
#   OPENROUTER_API_KEY  – your real key
#   TELEGRAM_BOT_TOKEN  – a bot token from @BotFather
#   ADMIN_PASSWORD      – a password only you know
docker compose up --build
```

Compose runs two services: `db` (`postgres:16`, data in the `pgdata` volume) and
`app` (built from the `Dockerfile`, image bytes in the `images` volume, published
on `8080:8080`). `app` waits for `db` to be healthy, runs migrations, then
listens and starts polling Telegram.

### Creating the first user

There is no `AUTH_USERS` env var and no `-gen-hash` subcommand: the bot is the
only way to provision an account. Once the service is up, message your bot:

```
/admin <ADMIN_PASSWORD>
/adduser alice alice-password
```

`alice` can now log in on the web UI with that username/password, or use the
bot directly. To let a Telegram chat skip `/login` entirely, link its Telegram
ID (from @userinfobot or similar) either at creation time or after:

```
/adduser bob bob-password 123456789
/addtelegramid alice 987654321
```

## Running without Docker

Requires Go 1.26+ and a reachable PostgreSQL.

```sh
export OPENROUTER_API_KEY=sk-or-...
export TELEGRAM_BOT_TOKEN=123456:...
export ADMIN_PASSWORD=let-me-in
export DATABASE_URL='postgres://user:pass@localhost:5432/imagen?sslmode=disable'
export IMAGE_STORAGE_DIR=./data/images
export SESSION_COOKIE_SECURE=false   # local http only
mkdir -p "$IMAGE_STORAGE_DIR"

go run ./cmd/server                  # migrate + serve + poll Telegram
go run ./cmd/server -migrate-only    # apply migrations and exit
```

## Configuration

Every setting is read from the **environment only** — the backend reads no
config file. `.env.example` is the single source of truth and is kept in
lockstep with the loader by a test. Docker Compose loads `.env`; outside Docker,
export the variables yourself.

| Variable | Required | Default | Purpose |
|---|:---:|---|---|
| `OPENROUTER_API_KEY` | yes | — | key attached to every OpenRouter call; never sent to the browser |
| `TELEGRAM_BOT_TOKEN` | yes | — | bot token from @BotFather; the bot is the only way to provision users |
| `ADMIN_PASSWORD` | yes | — | shared password gating the bot's `/admin` (user-management) commands |
| `DATABASE_URL` | yes | — | PostgreSQL connection string |
| `IMAGE_STORAGE_DIR` | no | `/data/images` | directory for image bytes; must exist and be writable |
| `LISTEN_ADDR` | no | `:8080` | bind address |
| `SESSION_TTL` | no | `720h` | how long a session stays valid (Go duration) |
| `SESSION_COOKIE_SECURE` | no | `true` | set `false` to allow the cookie over plain http (local dev only) |
| `SESSION_COOKIE_NAME` | no | `imagen_session` | session cookie name |
| `MAX_UPLOAD_BYTES` | no | `33554432` (32 MiB) | request body cap for `POST /api/generate`; `413` past it |
| `OPENROUTER_CONCURRENCY` | no | `4` | max in-flight OpenRouter calls per batch |
| `OPENROUTER_TIMEOUT` | no | `120s` | per-image upstream timeout (Go duration) |
| `OPENROUTER_BASE_URL` | no | `https://openrouter.ai/api/v1` | override for testing |
| `STATIC_DIR` | no | (embedded) | serve the frontend from disk instead of the embedded copy |

Startup fails fast, naming the offending variable, if a required var is
missing/empty, if `IMAGE_STORAGE_DIR` is not writable, or if the database is
unreachable.

## Telegram bot

The bot runs in the same process as the HTTP server (long polling; see
`internal/telegrambot`) and mirrors the web UI's generation, model/option
selection, and gallery, plus admin-only user management.

**It is button-driven: nothing has to be memorized.** Send a prompt as an
ordinary message, and use the persistent keyboard Telegram draws under the
input field for everything else:

| Button | What it opens |
|---|---|
| 🎨 New image | How to write a prompt, and what it will generate with. |
| 🖼 Gallery | The listing, the one-at-a-time browser, and delete-all. |
| ⚙️ Settings | Model, aspect ratio, image size, images per prompt, reset. |
| ❓ Help | The command reference below. |

The keyboard is installed on `/start` and on a successful `/login`, and stays
until it is replaced. Every command below still works when typed, but only a
short list — `/start`, `/menu`, `/help`, `/login`, `/settings`, `/gallery` —
is published to Telegram's "/" menu: a "/" list nobody can scan is exactly
what the buttons exist to avoid.

**General**
| Command | Description |
|---|---|
| `/start` | What the bot does; installs the keyboard and opens the menu. |
| `/help` | Every command, grouped, plus the buttons and shortcuts. |
| `/menu` | Open the inline menu (gallery, settings, help). |
| `/settings` | Open the settings submenu. |

**Authentication**
| Command | Description |
|---|---|
| `/login <username> <password>` | Temporarily authenticate this chat (rate-limited, same class of limiter as the web login). The bot deletes the message and never echoes the password. A chat whose Telegram ID an admin has linked (see `/addtelegramid`) is authenticated automatically, with no expiry, and `/login` is not needed. |
| `/logout` | Clear a temporary `/login` binding and any selected options. On a linked chat this replies that only an admin removing the link can end its access. |

**Admin (requires `/admin <password>` first; expires like a session)**
| Command | Description |
|---|---|
| `/admin <password>` | Authorize this chat as admin against `ADMIN_PASSWORD` (rate-limited; message deleted). |
| `/adduser <username> <password> [telegram_id...]` | Create a user, hashing the password server-side, optionally linking one or more Telegram IDs in the same operation. |
| `/addtelegramid <username> <telegram_id>` | Link an additional Telegram ID to an existing user. |
| `/listusers` | List every username and its linked Telegram IDs (paged). |

**Generation and options (require authentication)**
| Command | Description |
|---|---|
| `/generate <prompt>` | Generate with the chat's active model/options; also triggered by a plain-text message. A photo (or a reply to one) is used as a reference image when the active model supports image input, up to its reference limit. |
| `/models` | List the known model catalog (same set as `GET /api/models`), with a picker attached. |
| `/model [id]` | Select the active model for this chat; with no id, show the picker. |
| `/aspect [ratio]` | Set the aspect ratio, if the active model supports it; with no ratio, offer the presets. |
| `/size [size]` | Set the image size, if the active model supports it; with no size, offer the presets. |
| `/count [n]` | Set how many images to generate per request (1-8); with no n, offer the buttons. |

**Gallery (require authentication)**
| Command | Description |
|---|---|
| `/gallery` | List this chat's images, newest first (paged), with Browse and Delete-all buttons. |
| `/browse` | Page through the images one at a time, newest first. |
| `/delete <id>` | Delete one owned image (the id is in the `/gallery` listing). |
| `/clear` | Delete every image owned by this chat's user, without asking (the Delete-all button asks first). |

**Inline menus**

Inline keyboards carry the same authorization as the commands they stand for:
every press re-checks the chat's authentication, and a press on a stale button
(an option the newly selected model does not support, an image that is already
deleted) is refused with an explanation rather than acted on.

| Where | What it does |
|---|---|
| Main menu (`/menu`, `/start`, `/help`) | Three destinations — 🖼 Gallery, ⚙️ Settings, ❓ Help — and Close. Settings are one level down, so the root stays readable. |
| ⚙️ Settings | One row per setting, each labelled with the value in force (`🎨 Model · Gemini 2.5 Flash Image`, `📐 Aspect ratio · auto`), plus ♻️ Reset to defaults. Options the active model does not support are not offered; the pickers lead back here. Aspect ratios are ordered by how often they are wanted, and both the ratio and size pickers have an `auto` button that hands the choice back to the model. |
| 🖼 Gallery | The listing, with 🖼 Browse images, 🗑 Delete all (which asks first), and Back. An empty gallery says so and offers no destructive button. |
| Under every generated image | **Again** regenerates from the stored request (prompt, model, options; a reference image is not stored, so a request that used one is regenerated from the prompt alone), **Original** resends the image as an uncompressed file (Telegram re-encodes anything sent as a photo), **Delete** removes the image and its message — plus a row back to the gallery and the settings. |
| Browser | Arrows walk the list (wrapping at either end) by replacing the browser's own message, so arrow presses do not fill the chat. |

Selected model/options and admin authorization are in-memory per chat and are
lost on restart (cheap to redo); linked Telegram IDs and the `users` table
live in Postgres and are unaffected.

## Production notes

- **Put the service behind a TLS-terminating proxy.** The session cookie is
  `Secure` by default, so it will not be sent over plain http. Never run a
  non-localhost deployment with `SESSION_COOKIE_SECURE=false` — credentials and
  the session cookie would travel in the clear.
- Single instance only: image bytes live on a local volume, the login rate
  limiter is in-process, and the bot's Telegram long polling requires exactly
  one poller. There is no horizontal-scaling story.
- The bot is on the critical path for provisioning: if `TELEGRAM_BOT_TOKEN`
  is wrong or revoked, no new user can be created (existing logins/links are
  unaffected). `/login`, `/admin`, and `/adduser` carry a password in
  cleartext chat text — Telegram's transport is TLS and the bot deletes those
  messages when permitted, but avoid running them in a shared/group chat.
- The shared `OPENROUTER_API_KEY` means shared spend; `images.user_id` records
  who generated what, but there is no per-user quota.

## Migrating from the old client-only version

There is no server-side data to migrate. Images that previous users generated
with the old tool live in **their browser's IndexedDB** and are **not** imported
— that data stays only in the browser it was created in. A one-off import script
is out of scope.

## Development

```sh
go test ./...        # unit + handler tests
go vet ./...
```

Some tests (pgx integration, the server smoke test) need a PostgreSQL and are
skipped unless `IMAGEN_TEST_DATABASE_URL` is set.
