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
| `/models` | List the known model catalog (same set as `GET /api/models`). |
| `/model <id>` | Select the active model for this chat. |
| `/aspect <ratio>` | Set the aspect ratio, if the active model supports it. |
| `/size <size>` | Set the image size, if the active model supports it. |
| `/count <n>` | Set how many images to generate per request (1-8). |
| `/generate <prompt>` | Generate with the chat's active model/options; also triggered by a plain-text message. A photo (or a reply to one) is used as a reference image when the active model supports image input, up to its reference limit. |

**Gallery (require authentication)**
| Command | Description |
|---|---|
| `/gallery` | List this chat's images, newest first (paged). |
| `/delete <id>` | Delete one owned image. |
| `/clear` | Delete every image owned by this chat's user. |

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
