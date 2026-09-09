## Why

The current tool (`yusufipk/imagen-openrouter`, vendored here as the starting point) is 100% client-side: every user must paste their own OpenRouter API key into the browser, the browser calls `openrouter.ai` directly, and generated images live only in that browser's IndexedDB. That means the secret key is exposed in the client, there is no access control, and galleries cannot be shared or recovered across devices. Adding a small backend fixes all three: the key stays on the server, every request is proxied and authenticated, and images persist per user.

## What Changes

- **BREAKING** Remove the "OpenRouter API Key" field and its `localStorage` handling from the UI. The key is now configured only on the backend via an environment variable.
- **BREAKING** The browser no longer calls `https://openrouter.ai/...` directly. All generation requests go to the new backend, which injects the key and forwards to OpenRouter.
- Add a Go backend service that: serves the static frontend, exposes a JSON API, proxies image generation to OpenRouter, and stores generated images.
- Add **per-user authentication** with a login form: each user has a username and password (configured on the backend), signs in through an in-app form, and gets a server-side **session** (opaque HttpOnly session cookie). No route-level HTTP Basic Auth.
- **BREAKING** Move the image gallery from client-side IndexedDB to the server: image bytes on a mounted file volume, metadata in PostgreSQL, scoped per authenticated user. A user cannot see another user's images. The UI loads, displays, and deletes gallery images through the API.
- **All backend configuration is supplied through environment variables** — API key, user credentials, database URL, storage path, listen address, session settings, limits. No config file is read.
- Add project tooling: `.editorconfig`, a multi-stage `Dockerfile` (build Go + bundle static assets), and a `docker-compose.yml` running the app plus PostgreSQL.
- Keep the existing vanilla HTML/CSS/JS frontend, model list, generation options (model, image size, aspect ratio, batch count), reference-image upload, and "recreate" behavior. Only the transport, auth, and storage layers change.

## Capabilities

### New Capabilities

- `authentication`: Per-user login (username + password) that establishes a server-side session; a login/logout/session API; session-cookie enforcement on all data endpoints; user identity scopes user-owned data.
- `configuration`: All runtime configuration is read from environment variables only, with documented defaults and fail-fast validation of required values at startup.
- `image-generation-proxy`: A backend endpoint that accepts a generation request (prompt, model, options, reference images), attaches the server-held OpenRouter key, forwards it to OpenRouter's chat/completions API, and returns the resulting image(s).
- `image-gallery`: Server-side persistence and retrieval of generated images, scoped to the authenticated user — list, fetch bytes, and delete; one user cannot access another's; metadata in PostgreSQL, bytes on a file volume.
- `web-ui`: The browser application's observable behavior after adaptation — a login form gates the app, no API-key input, generation and gallery actions go through the backend API, gallery state comes from the server.

### Modified Capabilities

<!-- None: there are no existing specs under openspec/specs/. -->

## Impact

- **New code**: Go backend (repo root `cmd/`, `internal/`), SQL migrations, Dockerfile, `docker-compose.yml`, `.editorconfig`, `.env.example`.
- **Modified code**: `index.html` (remove key field, add login form), `src/app.js` (replace direct OpenRouter `fetch` + IndexedDB with backend API calls; add login/logout/session handling), `README.md` (setup/run instructions).
- **Removed behavior**: client-side API-key storage, direct browser-to-OpenRouter calls, IndexedDB gallery.
- **New dependencies**: Go toolchain, PostgreSQL, a Postgres driver / migration tool, bcrypt; Docker + Docker Compose for local run.
- **New configuration (environment variables only)**: `OPENROUTER_API_KEY`, `AUTH_USERS` (username + bcrypt-hash pairs), `DATABASE_URL`, `IMAGE_STORAGE_DIR`, `LISTEN_ADDR`, `SESSION_TTL`, `SESSION_COOKIE_SECURE`, `MAX_UPLOAD_BYTES`, plus optional OpenRouter tuning vars.
- **New endpoints**: `POST /api/login`, `POST /api/logout`, `GET /api/session`.
- **Deployment**: the app is now a stateful service (DB + file volume) rather than a static site.
