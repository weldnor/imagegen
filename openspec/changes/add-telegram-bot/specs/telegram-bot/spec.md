## Purpose

Gives users chat-based access to Imagen's image generation and gallery from
Telegram, authenticated against the same per-user credentials as the web UI,
so a user can generate and browse images without opening a browser.

## ADDED Requirements

### Requirement: Chat authentication
The bot SHALL require a Telegram chat to authenticate with an existing
`AUTH_USERS` username and password, via `/login <username> <password>`,
before it accepts any generation or gallery command from that chat. The bot
SHALL bind an authenticated chat to that app user for the duration of the
binding, and SHALL clear the binding on `/logout` or after the same
inactivity period as web sessions (`SESSION_TTL`).

#### Scenario: Successful login
- **WHEN** a chat sends `/login alice correct-password` and `alice` is a
  configured user with a matching password
- **THEN** the bot binds the chat to `alice`, confirms success, and does not
  echo the password back
- **AND** the bot deletes the `/login` message from the chat if the Telegram
  API permits it, so the password does not linger in chat history

#### Scenario: Failed login
- **WHEN** a chat sends `/login` with an unknown username or wrong password
- **THEN** the bot replies with a generic authentication failure (no
  indication of whether the username exists) and does not bind the chat

#### Scenario: Command requires authentication
- **WHEN** an unauthenticated chat sends `/generate`, `/gallery`, `/models`,
  `/model`, `/aspect`, `/size`, `/count`, `/delete`, or `/clear`
- **THEN** the bot replies asking the chat to `/login` first and performs no
  other action

#### Scenario: Logout clears binding
- **WHEN** an authenticated chat sends `/logout`
- **THEN** the bot clears the chat's user binding and any selected
  generation options, and confirms logout

#### Scenario: Login rate limiting
- **WHEN** a chat submits failed `/login` attempts repeatedly
- **THEN** the bot applies the same login rate limiting as the web API
  (`internal/auth` rate limiter) keyed per chat, rejecting further attempts
  once the limit is hit

### Requirement: Model catalog and selection
The bot SHALL expose the same model catalog the web UI and `GET /api/models`
expose, and SHALL let an authenticated chat select one of those models as
its active model for subsequent generations.

#### Scenario: Listing models
- **WHEN** an authenticated chat sends `/models`
- **THEN** the bot replies with the list of known model IDs and display
  names, matching the set `GET /api/models` returns

#### Scenario: Selecting a model
- **WHEN** an authenticated chat sends `/model <id>` with a known model ID
- **THEN** the bot sets that model as the chat's active model and confirms
  the selection

#### Scenario: Selecting an unknown model
- **WHEN** an authenticated chat sends `/model <id>` with an ID not in the
  catalog
- **THEN** the bot rejects the command with an error naming the unknown
  model and leaves the previous active model unchanged

#### Scenario: Default active model
- **WHEN** an authenticated chat has not yet selected a model
- **THEN** the bot uses a fixed default model for `/generate` and states
  which model is active when asked

### Requirement: Generation options per chat
The bot SHALL let an authenticated chat set image size, aspect ratio, and
image count for its active model, mirroring the options `POST
/api/generate` accepts, and SHALL reject an option unsupported by the
currently active model the same way the generation proxy does.

#### Scenario: Setting aspect ratio
- **WHEN** an authenticated chat sends `/aspect <ratio>` and the active
  model supports aspect ratio selection
- **THEN** the bot stores that aspect ratio for subsequent generations by
  this chat and confirms it

#### Scenario: Aspect ratio unsupported by model
- **WHEN** an authenticated chat sends `/aspect <ratio>` and the active
  model does not support aspect ratio selection
- **THEN** the bot rejects the command, stating the active model does not
  support this option

#### Scenario: Setting image size
- **WHEN** an authenticated chat sends `/size <size>` and the active model
  supports image size selection
- **THEN** the bot stores that image size for subsequent generations by this
  chat and confirms it

#### Scenario: Setting image count
- **WHEN** an authenticated chat sends `/count <n>` with `n` between 1 and 8
  inclusive
- **THEN** the bot stores that count for subsequent generations by this chat
  and confirms it

#### Scenario: Image count out of range
- **WHEN** an authenticated chat sends `/count <n>` with `n` less than 1 or
  greater than 8
- **THEN** the bot rejects the command stating the valid range and leaves
  the previous count unchanged

#### Scenario: Switching model resets unsupported options
- **WHEN** an authenticated chat switches to a model that does not support
  an option currently set (image size or aspect ratio)
- **THEN** the bot clears that unsupported option for the chat rather than
  silently sending it to the generation proxy

### Requirement: Image generation from chat
The bot SHALL generate images for an authenticated chat's prompt using that
chat's active model and generation options, matching the behavior of `POST
/api/generate`, and SHALL persist every successfully generated image to the
same per-user gallery the web UI uses.

#### Scenario: Generating from a text prompt
- **WHEN** an authenticated chat sends `/generate <prompt>` or, once
  authenticated, a plain text message
- **THEN** the bot requests generation with that prompt and the chat's
  active model, image size, aspect ratio, and count, and sends back the
  resulting image(s) as photo messages

#### Scenario: Generating with a reference image
- **WHEN** an authenticated chat sends a photo (optionally with a caption as
  the prompt) or replies to a photo with `/generate <prompt>`, and the
  active model supports image input
- **THEN** the bot includes that photo as a reference image in the
  generation request, up to the active model's maximum reference count

#### Scenario: Reference image with a model that does not support it
- **WHEN** an authenticated chat sends a photo and the active model does not
  support image input
- **THEN** the bot generates from the prompt/caption alone and tells the
  chat the photo was ignored because the active model does not accept
  reference images

#### Scenario: Empty prompt
- **WHEN** an authenticated chat sends `/generate` with no prompt text and
  no caption
- **THEN** the bot rejects the command asking for a prompt and makes no
  generation request

#### Scenario: Partial batch failure
- **WHEN** a multi-image generation request (count > 1) succeeds for some
  images and fails for others
- **THEN** the bot sends the successful images and reports how many failed,
  matching the partial-success behavior of `POST /api/generate`

#### Scenario: All generations fail
- **WHEN** every image in a generation request fails upstream
- **THEN** the bot sends no photos and reports the failure reason to the
  chat

### Requirement: Gallery browsing from chat
The bot SHALL let an authenticated chat list, view, and delete its
previously generated images, scoped to that user exactly as `GET/DELETE
/api/images[/{id}]` are.

#### Scenario: Listing the gallery
- **WHEN** an authenticated chat sends `/gallery`
- **THEN** the bot replies with the chat's images newest-first, paged so a
  single reply stays within Telegram's message limits, each entry showing
  its prompt, model, and creation time

#### Scenario: Deleting one image
- **WHEN** an authenticated chat sends `/delete <id>` for an image ID it
  owns
- **THEN** the bot deletes that image from the gallery and confirms removal

#### Scenario: Deleting an image not owned by the chat
- **WHEN** an authenticated chat sends `/delete <id>` for an image ID it
  does not own or that does not exist
- **THEN** the bot reports the image was not found and deletes nothing

#### Scenario: Clearing the gallery
- **WHEN** an authenticated chat sends `/clear`
- **THEN** the bot deletes every image owned by that chat's bound user and
  confirms how many were removed
