## Context

See `proposal.md` — Why. Starting point is the vendored client-only app: `index.html`, `src/app.js` (~1400 lines), `src/styles.css`. `app.js` today owns: state in `localStorage`, an `ImagenDB` IndexedDB wrapper, `generateSingleImage()` which `fetch`es `https://openrouter.ai/api/v1/chat/completions` with a `Bearer` key, and gallery rendering. The model list and per-model capability flags (`MODEL_CONFIGS`) already exist in `app.js` and must be mirrored on the backend.

Constraints:
- Keep the vanilla frontend (no framework, no build step for JS/CSS).
- Backend is Go + PostgreSQL; run locally via Docker Compose.
- Per-user auth: each user has a username + password; sign-in is an in-app login form backed by a server-side session. Galleries are isolated per user.
- All backend configuration comes from environment variables only — no config file.
- One shared `OPENROUTER_API_KEY` for all users (the proxy's key, not a per-user credential).

## Goals / Non-Goals

**Goals:**
- Single deployable service: Go binary serves the static frontend and the JSON API on one port.
- The OpenRouter key exists only in backend process env; never sent to the browser.
- Per-user gallery persistence that survives refresh and is reachable from any device.
- Minimal dependency footprint: standard library HTTP, one Postgres driver, one migration mechanism.
- `.editorconfig`, `Dockerfile`, `docker-compose.yml` so `docker compose up` gives a working app + DB.

**Non-Goals:**
- No user self-registration, password reset, email verification, or roles — users are provisioned by configuration.
- No "remember me", refresh tokens, or multi-device session management beyond a single cookie with a TTL.
- No per-user OpenRouter keys, billing, or quota accounting.
- No horizontal scaling design; single instance with a local volume is acceptable.
- No CDN / object storage; image bytes go on a mounted filesystem volume.
- No rework of the frontend's visual design or model list semantics.

## Decisions

### 1. Language stack: Go standard library `net/http` + `chi` router

`net/http` for the server; `github.com/go-chi/chi/v5` for routing and middleware (session auth, request logging, recovery) because stdlib `ServeMux` pattern matching is awkward for `/api/images/{id}`. Alternative: Gin/Echo — rejected as heavier than needed. DB access via `github.com/jackc/pgx/v5` (`pgxpool`); no ORM — a handful of hand-written queries is clearer than a dependency. Password hashing via `golang.org/x/crypto/bcrypt`.

### 2. Project layout (backend added alongside the existing static site)

```
/                       existing frontend (index.html, src/, favicon.svg, assets/)
/cmd/server/main.go      entrypoint: load config, connect DB, run migrations, serve
/internal/config         env parsing + validation (fail fast)
/internal/auth           user lookup + bcrypt verify, session store, session middleware
/internal/openrouter     client: build request, call OpenRouter, parse image out of response
/internal/gallery        store: metadata (pgx) + bytes (filesystem)
/internal/httpapi        handlers + routing, static file serving
/migrations              *.sql (embedded via embed.FS)
Dockerfile
docker-compose.yml
.editorconfig
.env.example
go.mod / go.sum
```

The Go module lives at repo root. Frontend files are embedded into the binary with `embed.FS` (so the image is self-contained) OR served from disk when a `STATIC_DIR` env var is set (convenient for local frontend edits). Default: embed.

### 3. Configuration — environment variables only, validated at startup

No config file is read. `internal/config` reads every setting from the environment (a `.env` file is loaded by Docker Compose / the shell, not by the app). `.env.example` is the single source of truth listing every variable.

| Var | Required | Default | Purpose |
|---|---|---|---|
| `OPENROUTER_API_KEY` | yes | — | key attached to every OpenRouter call |
| `AUTH_USERS` | yes | — | credentials, format `user1:bcrypt-hash,user2:bcrypt-hash` (see decision 4) |
| `DATABASE_URL` | yes | — | `postgres://…` connection string |
| `IMAGE_STORAGE_DIR` | no | `/data/images` | directory for image bytes; must be writable |
| `LISTEN_ADDR` | no | `:8080` | bind address |
| `SESSION_TTL` | no | `720h` | how long a session stays valid (Go duration) |
| `SESSION_COOKIE_SECURE` | no | `true` | set `false` to allow the cookie over plain http for local dev |
| `SESSION_COOKIE_NAME` | no | `imagen_session` | session cookie name |
| `MAX_UPLOAD_BYTES` | no | `33554432` (32 MiB) | request body cap for `/api/generate`; `413` past it |
| `OPENROUTER_CONCURRENCY` | no | `4` | max in-flight OpenRouter calls per batch |
| `OPENROUTER_TIMEOUT` | no | `120s` | per-image upstream timeout |
| `OPENROUTER_BASE_URL` | no | OpenRouter prod | override for tests |
| `STATIC_DIR` | no | (embedded) | serve frontend from disk instead of embedded |

Startup fails with a non-zero exit and a clear message if any required var is missing/empty, if `AUTH_USERS` yields zero users or a malformed entry, if `IMAGE_STORAGE_DIR` is not writable, or if the DB is unreachable. Unknown/omitted optional vars take the defaults above.

### 4. Auth: login form + opaque DB-backed session cookie

**Credentials.** `AUTH_USERS` holds `username:bcrypt-hash` pairs. On startup the backend parses them into an in-memory map and upserts a `users (id uuid pk, username text unique, created_at)` row per username, so `images.user_id` is a real FK. A small `-gen-hash` subcommand prints a bcrypt hash for a password. Removing a user from `AUTH_USERS` disables new logins for them (existing rows are kept; existing sessions die at their TTL). Hashes-not-plaintext satisfies "passwords SHALL NOT be stored in plaintext".

**Login.** `POST /api/login {username,password}` → look up the username in the map, `bcrypt.CompareHashAndPassword`. On success, create a session and set the cookie; on failure return `401` with one generic message for both unknown-user and wrong-password (and do a dummy bcrypt compare on unknown-user to keep timing flat). A minimal in-memory per-IP+username rate limiter (e.g. 10 failures / 5 min) slows brute force.

**Sessions.** Server-side, stored in Postgres:

```sql
sessions(
  id          text primary key,          -- 32 random bytes, base64url; the cookie value
  user_id     uuid not null references users(id) on delete cascade,
  created_at  timestamptz not null default now(),
  expires_at  timestamptz not null,
  last_seen_at timestamptz not null default now()
)
create index sessions_expires_idx on sessions(expires_at);
```

The cookie is `Set-Cookie: <SESSION_COOKIE_NAME>=<id>; HttpOnly; SameSite=Lax; Path=/; Secure` (`Secure` toggled by `SESSION_COOKIE_SECURE`), `Max-Age` = `SESSION_TTL`. The session middleware reads the cookie, looks up the row, rejects when missing or `expires_at` past, else puts `user_id`/`username` on the request context and refreshes `last_seen_at`. `POST /api/logout` deletes the row and clears the cookie. Expired rows are purged opportunistically (a cheap `DELETE WHERE expires_at < now()` on a ticker).

Why opaque DB sessions over a stateless signed cookie (JWT/HMAC): they are revocable (logout, user removal), need no separate signing-secret var, and are trivial to reason about. Cost is one indexed primary-key lookup per request — negligible here. Alternative — HTTP Basic Auth (the previous plan) — dropped: it has no real logout, no in-app login UI, and forces auth onto every static asset and `<img>` request.

**What's public:** static assets (`/`, `/src/*`, `/assets/*`, `/favicon.svg`), `POST /api/login`, `GET /healthz`. Everything else under `/api` requires a valid session. Serving the app shell unauthenticated is fine — it holds no secrets and all data is behind `/api`.

### 5. Data model

```sql
users(
  id          uuid primary key default gen_random_uuid(),
  username    text not null unique,
  created_at  timestamptz not null default now()
)

-- sessions: see decision 4

images(
  id            uuid primary key default gen_random_uuid(),
  user_id       uuid not null references users(id) on delete cascade,
  prompt        text not null,
  model         text not null,
  model_name    text not null,
  image_size    text,            -- e.g. "1K" / "1024x1024", nullable
  aspect_ratio  text not null,
  reference_count int not null default 0,
  content_type  text not null,   -- e.g. "image/png"
  file_path     text not null,   -- path under IMAGE_STORAGE_DIR
  created_at    timestamptz not null default now()
)
create index images_user_created_idx on images(user_id, created_at desc);
```

Image **bytes** live on the filesystem (`IMAGE_STORAGE_DIR/<user_id>/<image_id>.<ext>`), not in Postgres `bytea`: keeps the DB small, makes large 4K PNGs cheap to stream, and the compose volume is the natural persistence unit. Reference images sent for a generation are **not** stored (matches today's behavior where refs are transient inputs); only the count is recorded.

### 6. Migrations: embedded `.sql`, applied on startup

`migrations/*.sql` embedded via `embed.FS`, applied by `github.com/jackc/tern/v2` migrator (or a ~40-line hand-rolled runner tracking a `schema_migrations` table). Runs automatically before the server starts listening; a `-migrate-only` flag allows running them as a separate step. `gen_random_uuid()` requires `pgcrypto` (Postgres 13+ has it built-in as `gen_random_uuid`; enable `pgcrypto` extension in the first migration to be safe).

### 7. API surface

Request/response JSON unless noted. Session-protected unless marked public.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/api/login` | public | body `{username,password}` → `200 {username}` + session cookie, or `401` |
| `POST` | `/api/logout` | session (soft) | invalidate session, clear cookie; success even without a session |
| `GET` | `/api/session` | public | `200 {username}` if the cookie is valid, else `401` |
| `POST` | `/api/generate` | session | body `{prompt, model, imageSize, aspectRatio, count, references[] (data URIs)}` → `{images:[{id, url, ...meta}], failed:int}`. `url` is `/api/images/{id}`. |
| `GET` | `/api/images` | session | list current user's images, newest-first, as metadata array |
| `GET` | `/api/images/{id}` | session | raw image bytes with `Content-Type`; `404` if missing or not owned |
| `DELETE` | `/api/images/{id}` | session | delete one image (row + file); `404` if not owned |
| `DELETE` | `/api/images` | session | clear the current user's gallery |
| `GET` | `/api/models` | session | the known model list + capability flags (frontend can stop hard-coding it, optional) |
| `GET` | `/healthz` | public | liveness check, no data |

Everything else (`/`, `/src/*`, `/assets/*`, `/favicon.svg`) is the static frontend, served without a session so the login form can load. Same-origin `fetch` and `<img src>` send the session cookie automatically.

### 8. Generation flow (backend)

1. Validate: prompt non-empty, `model` in known list, `1 ≤ count ≤ 8`.
2. Build the OpenRouter chat/completions body — port `generateSingleImage()` from `app.js`: message `content` parts for reference images (only if the model `supportsImageInput`), text prompt, `image_config` (size + aspect ratio) for Gemini models, top-level `aspect_ratio` for others.
3. Run `count` calls to OpenRouter concurrently, bounded by `OPENROUTER_CONCURRENCY`, each with a per-request `context` timeout of `OPENROUTER_TIMEOUT`. Attach `Authorization: Bearer $OPENROUTER_API_KEY`, `HTTP-Referer`, `X-Title`.
4. Parse the image out of each response using the same fallback chain `app.js` uses today (`message.images[].image_url.url`, content parts, inline data, raw data URI).
5. For each success: decode to bytes, infer extension from content type, write file, insert row.
6. Respond with the successes and a `failed` count. If all fail, return `502` with the first upstream error message (key stripped).

### 9. Frontend changes (`index.html`, `src/app.js`, `src/styles.css`)

- `index.html`:
  - Delete the "OpenRouter API Key" `config-section` (lines ~106–111).
  - Add a login overlay: a centered `<form id="loginForm">` with username + password inputs, a submit button, and an error slot. Add a "Log out" button to the sidebar header.
  - Wrap the existing app markup so it can be hidden until authenticated (e.g. an `#appRoot` container with `hidden`).
- `app.js`:
  - Add an auth bootstrap: on `DOMContentLoaded`, call `GET /api/session`. If `200`, hide the login overlay, show `#appRoot`, run the existing `init()`. If `401`, show the login overlay and do not run `init()`.
  - Login form submit → `POST /api/login`; on `200` proceed to the app, on `401` show the error and keep the form.
  - Logout button → `POST /api/logout`, then show the login overlay and reset in-memory state.
  - Add a `fetchJSON` / `apiFetch` helper: on any `401` from an `/api/*` call while in the app, tear down to the login overlay so the user can re-auth; after a successful re-login, re-run `init()`.
  - Remove `state.apiKey`, the `saveApiKey` handler, and the `imagen_api_key` `localStorage` reads/writes.
  - Delete the `ImagenDB` object and all `ImagenDB.*` calls.
  - `generateImages()` → `POST /api/generate` with the current options; render returned images; on non-2xx show a toast and clear placeholders.
  - `init()` → load gallery from `GET /api/images` instead of `ImagenDB.getAllImages()`.
  - Delete/clear buttons → `DELETE /api/images/{id}` and `DELETE /api/images`.
  - Image `src` uses the returned `/api/images/{id}` URL (the browser sends the session cookie automatically for same-origin requests).
  - Keep `MODEL_CONFIGS`, model dropdown, size/aspect/count controls, reference upload, recreate, modal — unchanged except data source. `references` are still collected as data URIs and now sent in the request body. Because the backend records only the reference *count* (decision 5), "recreate" restores prompt/model/size/aspect and clears the reference list; it cannot repopulate the original reference images. "Use as reference" on a gallery image fetches that image's bytes from `/api/images/{id}` and converts them to a data URI before adding it to `references`.
- `src/styles.css`: styles for the login overlay/form and the logout button, reusing existing CSS variables.
- Other browser persistence (`localStorage` for last-used model/size/aspect/count) can stay — those are preferences, not secrets or images.

### 10. Docker

- **`Dockerfile`**: multi-stage. Stage 1 `golang:1.23` builds a static binary with the frontend embedded. Stage 2 `gcr.io/distroless/static` (or `alpine`) with the binary and a default `IMAGE_STORAGE_DIR` of `/data/images`. `EXPOSE 8080`.
- **`docker-compose.yml`**: services `db` (`postgres:16`, named volume `pgdata`, healthcheck) and `app` (built from `Dockerfile`, `depends_on: db healthy`, named volume `images` at `/data/images`, `env_file: .env`). Publish `8080:8080`. `.env.example` lists every variable from decision 3 (required marked, defaults shown), a sample `AUTH_USERS` with a bcrypt hash, a placeholder `OPENROUTER_API_KEY`, and `SESSION_COOKIE_SECURE=false` for local http.
- **`.editorconfig`**: root; LF, final newline, trim trailing whitespace; Go `indent_style=tab`; `*.{js,css,html,yml,yaml,json,md,sql}` `indent_style=space,indent_size=2` (Markdown keeps trailing-whitespace trimming off).

## Risks / Trade-offs

- **Session cookie over plain HTTP is interceptable** → `Secure` is on by default; document that the service MUST sit behind TLS in any non-local deployment. `SESSION_COOKIE_SECURE=false` is a local-dev-only escape hatch.
- **Login brute force** → generic `401`, bcrypt cost, and a small per-IP+username in-memory failure limiter. Not distributed (single instance assumption).
- **`SameSite=Lax` + no CSRF token** → acceptable because every state-changing endpoint is `POST`/`DELETE` with a JSON body and no cross-site form can send those with the cookie under `Lax`; revisit if a `GET` ever mutates.
- **Shared OpenRouter key = shared spend, no per-user attribution** → out of scope by decision; `images.user_id` records who generated what.
- **Reference images posted as data URIs inflate request bodies** (multiple MB) → set a generous but explicit request body size limit (e.g. 32 MB) and return `413` past it.
- **Filesystem storage isn't shared across replicas** → single-instance assumption stated in Non-Goals; migrating to object storage later only touches `internal/gallery`.
- **Embedding the frontend means a frontend edit needs a rebuild** → `STATIC_DIR` env var escape hatch for local dev.
- **Concurrent batch calls to OpenRouter can hit rate limits** → bounded concurrency (max ~4) and per-image failure isolation already in the spec; surface `429` text to the user.

## Migration Plan

Greenfield for the backend; no existing server data to migrate. Deployment:

1. `git` pull; `cp .env.example .env` and fill `OPENROUTER_API_KEY`, `AUTH_USERS` (use `-gen-hash` for each password), `DATABASE_URL`; adjust `SESSION_*` as needed.
2. `docker compose up -d` — Postgres starts, `app` runs migrations then listens on `:8080`.
3. Put a TLS-terminating proxy in front for anything but localhost (required so the `Secure` session cookie works and credentials aren't sent in the clear).

Rollback: `docker compose down` and redeploy the previous static-only version; the old client-side app is unaffected by the DB (users would revert to pasting their own keys, and lose the server gallery). No destructive DB migration is involved.

Users currently relying on browser IndexedDB galleries: those images stay in their browsers but are not imported. Call this out in the README; a one-off import script is out of scope.

## Open Questions

- Exact migration library (`tern` vs a tiny hand-rolled runner) — deferrable; does not affect specs, API, or task breakdown.
- Whether `/api/models` replaces the hard-coded `MODEL_CONFIGS` in `app.js` now or later — cosmetic; the hard-coded list can stay for the first cut.
