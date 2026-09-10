## 1. Dependency injection and domain modules

- [ ] 1.1 Add `go.uber.org/fx` to `go.mod`/`go.sum` and verify `go build ./...` succeeds
- [ ] 1.2 Add `module.go` (`fx.Module`) to `internal/config`, `internal/db`, `internal/auth`, `internal/gallery`, `internal/openrouter`, and `internal/httpapi`, each providing what `cmd/server/run.go` currently constructs by hand, and verify `go test ./...` still passes with no behavior change
- [ ] 1.3 Rewire `cmd/server` to build the app via `fx.New(...)` over those modules plus `fx.Invoke` to start the HTTP server, and verify the server starts, serves `/healthz`, and shuts down cleanly (existing smoke test still passes)

## 2. User storage and config

- [ ] 2.1 Add a migration creating `users (id, username unique, password_hash, created_at)`, `user_telegram_ids (telegram_id unique, user_id, created_at)`, and `telegram_bindings (chat_id PK, user_id, created_at, expires_at)`, and verify it applies cleanly via `go run ./cmd/server -migrate-only` against a test database
- [ ] 2.2 Replace `internal/auth`'s in-memory, config-built user map with DB-backed lookups (by username, and a new by-Telegram-ID lookup) against `users`/`user_telegram_ids`, and verify with unit tests covering found/not-found for both lookups
- [ ] 2.3 Remove `AUTH_USERS` from `internal/config` (`EnvVarNames`, `RequiredEnvVarNames`, `Config`, loader) and remove the `cmd/server -gen-hash` subcommand, and verify `TestEnvExampleMatchesLoader` (updated) and `go build ./...` pass
- [ ] 2.4 Add required `TELEGRAM_BOT_TOKEN` and `ADMIN_PASSWORD` to `internal/config` (`EnvVarNames`, `RequiredEnvVarNames`, `Config`, loader), update `.env.example` and README's config table, and verify startup fails fast with a clear error when either is missing
- [ ] 2.5 Choose and add the Telegram Bot API client dependency to `go.mod`/`go.sum` and verify `go build ./...` succeeds

## 3. Bot scaffolding and lifecycle

- [ ] 3.1 Create `internal/telegrambot` with a `module.go` (`fx.Module`) providing a `Bot` type wired to `Generator`, `GalleryStore`, the DB-backed user store, and a bindings store, and verify it compiles with unit tests for construction
- [ ] 3.2 Implement the long-polling update loop (`getUpdates`) started via `fx.Lifecycle.OnStart`/stopped via `OnStop`, with panic recovery so a bot crash cannot take down the HTTP server, and verify with a test that a simulated panic in a handler does not stop the process
- [ ] 3.3 Implement command routing (`/admin`, `/adduser`, `/addtelegramid`, `/listusers`, `/login`, `/logout`, `/models`, `/model`, `/aspect`, `/size`, `/count`, `/generate`, `/gallery`, `/delete`, `/clear`, plain text, and photo messages) dispatching to per-command handlers, and verify with a table-driven test mapping sample updates to the expected handler

## 4. Chat authentication

- [ ] 4.1 Implement the authentication resolution order for an incoming chat - linked Telegram ID first (no expiry), then a live `telegram_bindings` row (`SESSION_TTL`), then unauthenticated - and verify with tests for each of the three outcomes
- [ ] 4.2 Implement `/login <username> <password>`: verify against the `users` table, create/refresh the chat's `telegram_bindings` row with `SESSION_TTL`, best-effort delete the login message, and reply without echoing the password; verify with a test asserting no password appears in any reply and the binding is created on success
- [ ] 4.3 Implement generic-failure responses and per-chat login rate limiting reusing `internal/auth`'s rate limiter, and verify with a test that repeated failed logins from one chat get rejected past the limit
- [ ] 4.4 Implement `/logout` (clears the temporary binding + in-memory chat options on an unlinked chat; replies that the chat is linked and stays authenticated when it has a Telegram-ID link) and an auth-gate that rejects all protected commands with a "please /login" reply when neither linked nor logged in, and verify with tests covering a linked chat, a logged-in chat, and an unauthenticated chat hitting a protected command
- [ ] 4.5 Implement binding expiry for the temporary `/login` path (an expired `telegram_bindings` row is treated as unauthenticated) and verify with a test using a pre-expired row

## 5. Admin authentication and user management

- [ ] 5.1 Implement in-memory, per-chat admin authorization (mutex-guarded map, `SESSION_TTL`-style expiry) and `/admin <password>` checked against `ADMIN_PASSWORD`, with generic-failure replies, best-effort message deletion, and the same rate limiting as `/login`, and verify with tests for success, failure, expiry, and rate limiting
- [ ] 5.2 Implement an admin-gate that rejects `/adduser`, `/addtelegramid`, and `/listusers` with a "please /admin" reply when the chat is not admin-authorized, and verify with a test hitting each command unauthorized
- [ ] 5.3 Implement `/adduser <username> <password> [telegram_id...]`: reject a duplicate username or a Telegram ID already linked to another user, otherwise hash the password and create the user (plus any linked Telegram IDs) in one operation, and verify with tests for success with and without Telegram IDs, duplicate username, and an already-linked Telegram ID
- [ ] 5.4 Implement `/addtelegramid <username> <telegram_id>`: reject an unknown username or an already-linked Telegram ID, otherwise link it, and verify with tests for success, unknown username, and already-linked ID
- [ ] 5.5 Implement `/listusers` listing every username with its linked Telegram IDs, paged to fit Telegram message limits, and verify with a test using enough users to require paging

## 6. Model and generation options per chat

- [ ] 6.1 Implement in-memory per-chat option state (active model, aspect ratio, image size, count) guarded by a mutex, with a fixed default model, and verify with concurrent-access unit tests
- [ ] 6.2 Implement `/models` and `/model <id>` against `openrouter.KnownModels`/`openrouter.Model`, rejecting unknown IDs, and verify with tests for a known and an unknown model ID
- [ ] 6.3 Implement `/aspect`, `/size`, `/count`, validating each against the active model's `ModelConfig` (`SupportsAspectRatio`, `SupportsImageSize`) and the count range 1-8, and verify with tests covering supported, unsupported, and out-of-range inputs
- [ ] 6.4 Implement clearing unsupported options when `/model` switches to a model that doesn't support a currently-set option, and verify with a test that sets aspect ratio then switches to a model without `SupportsAspectRatio` and asserts the option is cleared

## 7. Generation and photo handling

- [ ] 7.1 Implement `/generate <prompt>` and authenticated plain-text messages, calling the same `Generator.Generate` used by `internal/httpapi.API.Generate` with the chat's active model/options, and verify with a test asserting the same params are built as the HTTP handler would for equivalent input
- [ ] 7.2 Implement sending back one or more generated photos, persisting each via `GalleryStore.Save` with metadata matching `POST /api/generate`'s behavior, and verify with a test that saved metadata (model, prompt, aspect ratio, image size, reference count) matches the request
- [ ] 7.3 Implement partial-failure and all-failed reporting (count of failures, upstream error message) matching `internal/httpapi`'s `generateResponse` semantics, and verify with tests for partial and total failure
- [ ] 7.4 Implement reference-image handling: extract a photo from the message or the replied-to message, pass it to `Generate` when the active model's `SupportsImageInput` is true (capped at `MaxReferences`), and reply that the photo was ignored otherwise; verify with tests for a supporting model, a non-supporting model, and exceeding `MaxReferences`
- [ ] 7.5 Implement the empty-prompt rejection (no `/generate` argument, no caption) and verify with a test asserting no generation call is made

## 8. Gallery browsing

- [ ] 8.1 Implement `/gallery` listing via `GalleryStore.List`, paged to fit Telegram message limits, showing prompt/model/created time, and verify with a test using a gallery large enough to require paging
- [ ] 8.2 Implement `/delete <id>` via `GalleryStore.Delete`/`Get`, reporting not-found for another user's or nonexistent image, and verify with tests for owned, not-owned, and nonexistent IDs
- [ ] 8.3 Implement `/clear` via `GalleryStore.ClearForUser`, reporting the count removed, and verify with a test asserting all of a user's images are gone and the count matches

## 9. Wiring, docs, and end-to-end verification

- [ ] 9.1 Wire the bot's `fx.Module` into `cmd/server`'s `fx.New(...)` graph and its polling loop into `fx.Lifecycle`, sharing the existing DB pool, gallery store, user store, and OpenRouter client, and verify the server starts, runs both the HTTP server and the bot, and shuts down cleanly on signal
- [ ] 9.2 Update README (architecture diagram, configuration table, the new bootstrap flow replacing `AUTH_USERS`/`-gen-hash`, and a "Telegram bot" section describing every command from `/admin` through `/clear`) and verify docs mention every command in the spec
- [ ] 9.3 Run `go test ./...` and `go vet ./...` and verify both pass
