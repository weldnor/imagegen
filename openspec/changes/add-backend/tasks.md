## 1. Project scaffolding & tooling

- [x] 1.1 Add `.editorconfig` at repo root (LF, final newline, trim trailing whitespace; Go = tab; js/css/html/yml/yaml/json/sql = 2-space; Markdown keeps trailing whitespace) and verify `editorconfig-checker` (or manual review) reports no conflicts on existing files
- [x] 1.2 Run `go mod init` for the repo module and add deps (`chi/v5`, `pgx/v5`, `golang.org/x/crypto/bcrypt`, migration lib); verify `go build ./...` succeeds on an empty `cmd/server/main.go`
- [x] 1.3 Create the package layout from design decision 2 (`cmd/server`, `internal/{config,auth,openrouter,gallery,httpapi}`, `migrations`) with placeholder files; verify `go vet ./...` passes

## 2. Configuration (capability: configuration)

- [x] 2.1 Implement `internal/config` reading every variable in design decision 3 from the environment only (no file): required `OPENROUTER_API_KEY`, `AUTH_USERS`, `DATABASE_URL`; optional `IMAGE_STORAGE_DIR`, `LISTEN_ADDR`, `SESSION_TTL`, `SESSION_COOKIE_SECURE`, `SESSION_COOKIE_NAME`, `MAX_UPLOAD_BYTES`, `OPENROUTER_CONCURRENCY`, `OPENROUTER_TIMEOUT`, `OPENROUTER_BASE_URL`, `STATIC_DIR` — verify a unit test asserts every documented default and parses durations/bools/ints
- [x] 2.2 Fail fast on startup with a message naming the offending variable: missing/empty required var, zero parsed users, malformed `AUTH_USERS` entry, non-writable `IMAGE_STORAGE_DIR`; verify a test asserts a descriptive non-nil error for each case
- [x] 2.3 Confirm the backend reads no configuration file (only env); verify by a test/review that startup does not open a config path
- [x] 2.4 Add a `-gen-hash` subcommand that prints a bcrypt hash for a password; verify it produces a hash that `bcrypt.CompareHashAndPassword` accepts
- [x] 2.5 Keep `.env.example` in lockstep with `internal/config` (every var, required flag, default); verify a test cross-checks the var names in `.env.example` against the config loader

## 3. Database & migrations

- [x] 3.1 Write `migrations/0001_init.sql` creating `pgcrypto`, `users`, `sessions` (with the `sessions_expires_idx` index), `images`, and the `images(user_id, created_at desc)` index per design decisions 4–5; verify it applies cleanly to a fresh Postgres
- [x] 3.2 Implement embedded-migration runner invoked on startup plus a `-migrate-only` flag; verify running twice against the same DB is a no-op (idempotent) via a test
- [x] 3.3 Implement `pgxpool` connection with startup ping; verify the server exits with a clear error when `DATABASE_URL` points nowhere

## 4. Authentication (capability: authentication)

- [x] 4.1 Parse `AUTH_USERS` (`user:bcrypt-hash` comma list) into an in-memory map; verify a test rejects malformed entries and accepts valid ones
- [x] 4.2 On startup, upsert a `users` row per configured username; verify a test confirms rows exist and re-running does not duplicate
- [x] 4.3 Implement the session store in `internal/auth`: `Create(userID) -> id` (32 random bytes base64url, `expires_at = now + SESSION_TTL`), `Lookup(id) -> (user, ok)` rejecting missing/expired, `Delete(id)`, and opportunistic purge of expired rows; verify pgx integration tests for create/lookup/expiry/delete
- [x] 4.4 Implement `POST /api/login`: look up username, `bcrypt.CompareHashAndPassword` (dummy compare on unknown user for flat timing), generic `401` for unknown-user and wrong-password, `400` on missing fields; on success create a session and set the cookie (`HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` per `SESSION_COOKIE_SECURE`, `Max-Age = SESSION_TTL`, name `SESSION_COOKIE_NAME`) — verify handler tests for success, wrong password, unknown user, malformed body
- [x] 4.5 Implement a small per-IP+username in-memory failed-login limiter (e.g. 10 / 5 min → `429`); verify a test trips and resets it
- [x] 4.6 Implement session middleware: read the cookie, `Lookup`, `401` on missing/unknown/expired, else put `user_id`/`username` on request context and refresh `last_seen_at`; verify table-driven tests for each outcome
- [x] 4.7 Implement `GET /api/session` (`200 {username}` or `401`) and `POST /api/logout` (delete row, clear cookie, success even with no session); verify handler tests including logout-then-reuse-old-cookie → `401`
- [x] 4.8 Ensure request logging never logs passwords or full cookie values; verify a test inspects captured log output
- [x] 4.9 Wire middleware: public = static assets, `POST /api/login`, `GET /api/session`, `GET /healthz`; session-protected = everything else under `/api`; verify an integration test that protected paths return `401` without a cookie and succeed with one, and public paths work without

## 5. OpenRouter client (capability: image-generation-proxy)

- [x] 5.1 Port `MODEL_CONFIGS` (model id → name, `supportsImageSize`, `supportsAspectRatio`, `supportsImageInput`, `maxReferences`) into `internal/openrouter`; verify a test asserts the list matches `src/app.js`
- [x] 5.2 Implement request-body builder mirroring `generateSingleImage()` (reference image parts only when `supportsImageInput`, text part, Gemini `image_config`, non-Gemini top-level `aspect_ratio`); verify unit tests for a Gemini and a non-Gemini model produce the expected JSON
- [x] 5.3 Implement the HTTP call with `Bearer` key, `HTTP-Referer`, `X-Title`, per-request context timeout (~120s), against `OPENROUTER_BASE_URL`; verify a test using an `httptest` server asserts headers and body
- [x] 5.4 Implement the response image extractor using the same fallback chain as `app.js` (`message.images[].image_url.url`, content parts, inline data, raw data URI); verify unit tests cover each shape and a "no image" error
- [x] 5.5 Map upstream failures: OpenRouter 4xx/5xx → same-class status with key-stripped message; connection error/timeout → `502`/`504`; verify tests assert status and that no response contains the API key

## 6. Gallery store (capability: image-gallery)

- [x] 6.1 Implement metadata CRUD in `internal/gallery` (insert, list-by-user newest-first, get-by-id-scoped-to-user, delete-by-id-scoped-to-user, delete-all-by-user); verify pgx integration tests including cross-user isolation (user A cannot get/delete user B's row)
- [x] 6.2 Implement byte storage: write to `IMAGE_STORAGE_DIR/<user_id>/<image_id>.<ext>` (ext from content type), read, delete file, delete user dir; verify tests for round-trip and missing-file handling
- [x] 6.3 Implement a combined `Save(userID, bytes, meta)` that writes file then inserts row and rolls back the file if the insert fails; verify a test forces an insert error and asserts no orphan file remains
- [x] 6.4 Implement `Delete` / `ClearForUser` removing both row and file(s); verify a test asserts both are gone and other users are untouched

## 7. HTTP API (capabilities: image-generation-proxy, image-gallery)

- [x] 7.1 `POST /api/generate`: validate prompt non-empty, model known, `1 ≤ count ≤ 8`, body size ≤ `MAX_UPLOAD_BYTES` (`413` past it); verify handler tests for each `400`/`413` case
- [x] 7.2 `POST /api/generate` happy path: run up to `OPENROUTER_CONCURRENCY` concurrent OpenRouter calls, persist each success via the gallery store for the session's user, return `{images:[{id,url,...meta}], failed}` with `url = /api/images/{id}`; verify an integration test with a fake OpenRouter returns N images and a gallery list then shows them
- [x] 7.3 `POST /api/generate` partial/total failure: return successes + `failed` count on partial; `502` with first upstream message on total failure; verify tests for both
- [x] 7.4 `GET /api/images` returns the caller's metadata newest-first (empty list when none); verify handler test
- [x] 7.5 `GET /api/images/{id}` streams bytes with correct `Content-Type`; `404` when missing or not owned; verify handler tests including cross-user `404`
- [x] 7.6 `DELETE /api/images/{id}` and `DELETE /api/images` (clear) remove row+bytes, scoped to caller, `404` on non-owned single delete; verify handler tests
- [x] 7.7 `GET /api/models` (session) returns the model list + capability flags; `GET /healthz` (public) returns `200` with no data; verify tests
- [x] 7.8 Serve the frontend: embedded `embed.FS` by default, disk when `STATIC_DIR` set, for `/`, `/src/*`, `/assets/*`, `/favicon.svg`, with no session required (login form must load); verify an integration test fetches `/` without a cookie and gets `index.html`
- [x] 7.9 Assemble the chi router, middleware order (recovery → logging → per-route session middleware), and `cmd/server/main.go` wiring (config → DB → migrate → stores → server; `-migrate-only` and `-gen-hash` subcommands); verify `go build ./...` and a smoke test that boots the server against a test DB

## 8. Frontend adaptation (capability: web-ui)

- [x] 8.1 In `index.html`: remove the "OpenRouter API Key" section; add a login overlay (`<form id="loginForm">` with username + password, submit button, error slot); wrap the app markup in an `#appRoot` container that starts `hidden`; add a "Log out" button in the sidebar header — verify the page shows only the login form on first load
- [x] 8.2 Add `src/styles.css` rules for the login overlay/form and logout button using existing CSS variables; verify the overlay is centered and themed consistently
- [x] 8.3 Add an auth bootstrap in `src/app.js`: on `DOMContentLoaded` call `GET /api/session`; `200` → hide overlay, show `#appRoot`, run `init()`; `401` → show overlay, do not run `init()` — verify both paths by toggling a session
- [x] 8.4 Wire the login form → `POST /api/login` (`200` → enter app, `401` → show error, keep form) and the logout button → `POST /api/logout` → show overlay and reset in-memory state; verify correct/incorrect credentials and logout
- [x] 8.5 Add an `apiFetch` helper: JSON by default, throws on non-2xx, and on any `/api/*` `401` while in the app tears down to the login overlay; after a successful re-login it re-runs `init()` — verify by expiring a session mid-use
- [x] 8.6 In `src/app.js` remove `state.apiKey`, the `saveApiKey` handler, and all `imagen_api_key` storage access; verify `grep -i api.key src/app.js` finds nothing and no `localStorage` key for it is written
- [x] 8.7 Delete the `ImagenDB` object and every `ImagenDB.*` call; verify `grep -i indexeddb src/app.js` finds nothing
- [x] 8.8 Rewrite `generateImages()` to `POST /api/generate` with prompt/model/imageSize/aspectRatio/count/references and render returned images (using `/api/images/{id}` URLs); on error show a toast and clear placeholders — verify generating 2 images shows 2 cards and a forced error shows a toast
- [x] 8.9 Load the gallery in `init()` from `GET /api/images`; wire delete → `DELETE /api/images/{id}` and clear-gallery → `DELETE /api/images`; verify generate → reload shows the images persisted, delete removes one, clear empties it
- [x] 8.10 Confirm model dropdown, size/aspect/count controls, reference upload/paste/drag, recreate, and the modal still work against server data; verify by exercising each in the browser

## 9. Docker & compose

- [x] 9.1 Write a multi-stage `Dockerfile` (`golang:1.23` build with embedded frontend → distroless/alpine runtime, `EXPOSE 8080`, default `IMAGE_STORAGE_DIR=/data/images`); verify `docker build .` succeeds and the image runs `--help`/`-migrate-only`
- [x] 9.2 Write `docker-compose.yml` with `db` (`postgres:16`, `pgdata` volume, healthcheck) and `app` (build, `depends_on: db healthy`, `images` volume at `/data/images`, `env_file: .env`, `8080:8080`); verify `docker compose up` yields a reachable app
- [x] 9.3 Add `.env.example` listing every variable from design decision 3 (required marked, defaults shown), a sample `AUTH_USERS` with a bcrypt hash, a placeholder `OPENROUTER_API_KEY`, and `SESSION_COOKIE_SECURE=false` for local http; verify copying it to `.env` and setting a real key lets `docker compose up` reach a working login + generate flow
- [x] 9.4 Add `.dockerignore` (`.git`, `node_modules`, local `*.env`, build artifacts); verify build context size is reduced

## 10. Documentation & end-to-end verification

- [x] 10.1 Rewrite `README.md`: new architecture, the full env-var reference (all from environment, no config file), `-gen-hash` usage, `docker compose up`, the "must sit behind TLS in production" note, and that browser IndexedDB galleries are not imported; verify a new reader can start the stack from the README alone
- [x] 10.2 End-to-end: fresh `docker compose up`; open the app → login form shown; log in as user A, generate a batch, reload (images persist), delete one, clear gallery; log out → login form returns; log in as user B → sees none of user A's images; confirm no request goes to `openrouter.ai` from the browser and no API key appears in any response — verify all hold
