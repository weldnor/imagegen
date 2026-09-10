## 1. Config and migration

- [ ] 1.1 Add `TELEGRAM_BOT_TOKEN` (optional) to `internal/config` (`EnvVarNames`, `Config` struct, loader), update `.env.example` and README config table, and verify `TestEnvExampleMatchesLoader` still passes
- [ ] 1.2 Add a migration creating `telegram_bindings (chat_id PK, user_id, created_at, expires_at)` and verify it applies cleanly via `go run ./cmd/server -migrate-only` against a test database
- [ ] 1.3 Choose and add the Telegram Bot API client dependency to `go.mod`/`go.sum` and verify `go build ./...` succeeds

## 2. Bot scaffolding and lifecycle

- [ ] 2.1 Create `internal/telegrambot` package with a `Bot` type wired to `Generator`, `GalleryStore`, `*auth.Users`, and a bindings store, and verify it compiles with unit tests for construction
- [ ] 2.2 Implement long-polling update loop (`getUpdates`) started as a goroutine from `cmd/server` only when `TELEGRAM_BOT_TOKEN` is set, with panic recovery so a bot crash cannot take down the HTTP server, and verify with a test that a simulated panic in a handler does not stop the process
- [ ] 2.3 Implement command routing (`/login`, `/logout`, `/models`, `/model`, `/aspect`, `/size`, `/count`, `/generate`, `/gallery`, `/delete`, `/clear`, plain text, and photo messages) dispatching to per-command handlers, and verify with a table-driven test mapping sample updates to the expected handler

## 3. Authentication and per-chat binding

- [ ] 3.1 Implement a bindings store (`telegram_bindings` CRUD: bind, lookup by chat ID, clear) reusing `internal/auth` credential verification, and verify with unit tests covering bind/lookup/expire/clear
- [ ] 3.2 Implement `/login <username> <password>`: verify credentials, create/refresh the binding with `SESSION_TTL`, best-effort delete the login message, and reply without echoing the password; verify with a test asserting no password appears in any reply and the binding is created on success
- [ ] 3.3 Implement generic-failure responses and per-chat login rate limiting reusing `internal/auth`'s rate limiter, and verify with a test that repeated failed logins from one chat get rejected past the limit
- [ ] 3.4 Implement `/logout` (clear binding + in-memory chat options) and an auth-gate middleware that rejects all protected commands with a "please /login" reply when unauthenticated, and verify with tests for both an authenticated and unauthenticated chat hitting a protected command
- [ ] 3.5 Implement binding expiry (treat an expired binding as unauthenticated) and verify with a test using a pre-expired binding row

## 4. Model and generation options per chat

- [ ] 4.1 Implement in-memory per-chat option state (active model, aspect ratio, image size, count) guarded by a mutex, with a fixed default model, and verify with concurrent-access unit tests
- [ ] 4.2 Implement `/models` and `/model <id>` against `openrouter.KnownModels`/`openrouter.Model`, rejecting unknown IDs, and verify with tests for a known and an unknown model ID
- [ ] 4.3 Implement `/aspect`, `/size`, `/count`, validating each against the active model's `ModelConfig` (`SupportsAspectRatio`, `SupportsImageSize`) and the count range 1-8, and verify with tests covering supported, unsupported, and out-of-range inputs
- [ ] 4.4 Implement clearing unsupported options when `/model` switches to a model that doesn't support a currently-set option, and verify with a test that sets aspect ratio then switches to a model without `SupportsAspectRatio` and asserts the option is cleared

## 5. Generation and photo handling

- [ ] 5.1 Implement `/generate <prompt>` and authenticated plain-text messages, calling the same `Generator.Generate` used by `internal/httpapi.API.Generate` with the chat's active model/options, and verify with a test asserting the same params are built as the HTTP handler would for equivalent input
- [ ] 5.2 Implement sending back one or more generated photos, persisting each via `GalleryStore.Save` with metadata matching `POST /api/generate`'s behavior, and verify with a test that saved metadata (model, prompt, aspect ratio, image size, reference count) matches the request
- [ ] 5.3 Implement partial-failure and all-failed reporting (count of failures, upstream error message) matching `internal/httpapi`'s `generateResponse` semantics, and verify with tests for partial and total failure
- [ ] 5.4 Implement reference-image handling: extract a photo from the message or the replied-to message, pass it to `Generate` when the active model's `SupportsImageInput` is true (capped at `MaxReferences`), and reply that the photo was ignored otherwise; verify with tests for a supporting model, a non-supporting model, and exceeding `MaxReferences`
- [ ] 5.5 Implement the empty-prompt rejection (no `/generate` argument, no caption) and verify with a test asserting no generation call is made

## 6. Gallery browsing

- [ ] 6.1 Implement `/gallery` listing via `GalleryStore.List`, paged to fit Telegram message limits, showing prompt/model/created time, and verify with a test using a gallery large enough to require paging
- [ ] 6.2 Implement `/delete <id>` via `GalleryStore.Delete`/`Get`, reporting not-found for another user's or nonexistent image, and verify with tests for owned, not-owned, and nonexistent IDs
- [ ] 6.3 Implement `/clear` via `GalleryStore.ClearForUser`, reporting the count removed, and verify with a test asserting all of a user's images are gone and the count matches

## 7. Wiring, docs, and end-to-end verification

- [ ] 7.1 Wire bot startup into `cmd/server`/`run.go` behind `TELEGRAM_BOT_TOKEN`, sharing the existing `pgxpool.Pool`, `gallery.Store`, `auth.Users`, and OpenRouter client instances, and verify the server starts unchanged with the token unset and starts the bot goroutine when it is set (via a log line or test hook)
- [ ] 7.2 Update README (architecture diagram, configuration table, a "Telegram bot" section describing `/login` through `/clear`) and verify docs mention every command in the spec
- [ ] 7.3 Run `go test ./...` and `go vet ./...` and verify both pass
