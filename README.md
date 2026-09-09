# Imagen

Internal AI image-generation tool. A small Go backend serves a vanilla
HTML/CSS/JS frontend, proxies generation requests to
[OpenRouter](https://openrouter.ai), and stores every generated image per user.

This repository started as the fully client-side
`yusufipk/imagen-openrouter` tool. It now has a backend so that:

- the OpenRouter API key lives only on the server — never in the browser;
- access is gated by a per-user login (username + password) and a server-side
  session;
- generated images persist server-side (Postgres metadata + a file volume),
  scoped per user, instead of the browser's IndexedDB.

## Architecture

```
browser ──HTTP──▶ Go service ──HTTPS──▶ OpenRouter
                    │
                    ├─ serves the embedded frontend (/, /src/*, /favicon.svg)
                    ├─ POST /api/login · GET /api/session · POST /api/logout
                    ├─ POST /api/generate           (proxy + persist)
                    ├─ GET/DELETE /api/images[/{id}] (per-user gallery)
                    ├─ GET /api/models · GET /healthz
                    │
                    ├─ PostgreSQL   image + user + session metadata
                    └─ file volume  image bytes at IMAGE_STORAGE_DIR/<user>/<id>.<ext>
```

- One binary. The frontend is embedded via `go:embed` (override with
  `STATIC_DIR` for local frontend edits).
- Auth is an opaque, DB-backed session cookie (`HttpOnly`, `SameSite=Lax`).
  There is no route-level HTTP Basic Auth and no API-key field in the UI.
- Database migrations are embedded and run automatically on startup.

## Quick start (Docker Compose)

```sh
cp .env.example .env
# edit .env:
#   OPENROUTER_API_KEY  – your real key
#   AUTH_USERS          – username:bcrypt-hash pairs (see below)
docker compose up --build
```

Then open <http://localhost:8080>, and log in with a username from `AUTH_USERS`.

Compose runs two services: `db` (`postgres:16`, data in the `pgdata` volume) and
`app` (built from the `Dockerfile`, image bytes in the `images` volume, published
on `8080:8080`). `app` waits for `db` to be healthy, runs migrations, then
listens.

### Generating a password hash

`AUTH_USERS` stores bcrypt hashes, not plaintext. Generate one with the
`-gen-hash` subcommand:

```sh
# with the image built by compose
docker compose run --rm app -gen-hash -password 'correct horse battery staple'

# or locally with the Go toolchain
go run ./cmd/server -gen-hash -password 'correct horse battery staple'
```

Paste the printed hash into `AUTH_USERS`. **Keep the single quotes** — bcrypt
hashes contain `$`, which Docker Compose would otherwise try to expand:

```
AUTH_USERS='alice:$2a$10$...,bob:$2a$10$...'
```

Removing a user from `AUTH_USERS` disables new logins for them; their existing
sessions expire at their TTL.

## Running without Docker

Requires Go 1.26+ and a reachable PostgreSQL.

```sh
export OPENROUTER_API_KEY=sk-or-...
export AUTH_USERS='alice:$2a$10$...'
export DATABASE_URL='postgres://user:pass@localhost:5432/imagen?sslmode=disable'
export IMAGE_STORAGE_DIR=./data/images
export SESSION_COOKIE_SECURE=false   # local http only
mkdir -p "$IMAGE_STORAGE_DIR"

go run ./cmd/server                  # migrate + serve
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
| `AUTH_USERS` | yes | — | credentials, `user1:bcrypt-hash,user2:bcrypt-hash` |
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
missing/empty, if `AUTH_USERS` yields zero users or a malformed entry, if
`IMAGE_STORAGE_DIR` is not writable, or if the database is unreachable.

## Production notes

- **Put the service behind a TLS-terminating proxy.** The session cookie is
  `Secure` by default, so it will not be sent over plain http. Never run a
  non-localhost deployment with `SESSION_COOKIE_SECURE=false` — credentials and
  the session cookie would travel in the clear.
- Single instance only: image bytes live on a local volume and the login rate
  limiter is in-process. There is no horizontal-scaling story.
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
