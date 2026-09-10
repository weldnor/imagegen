## Purpose

Gives users chat-based access to Imagen's image generation and gallery from
Telegram, authenticated against the same database-backed user accounts as
the web UI (with automatic authentication for a Telegram ID an admin has
linked), so a user can generate and browse images, and an admin can manage
who has access, without opening a browser.

## ADDED Requirements

### Requirement: Chat authentication
The bot SHALL authenticate a chat as an app user in one of two ways: (1)
automatically, when the chat's Telegram user ID is linked to a user (see
Automatic authentication via linked Telegram ID), or (2) temporarily, via
`/login <username> <password>` checked against the `users` table, for a
chat that isn't linked. The bot SHALL NOT accept any generation or gallery
command from a chat that is neither linked nor logged in.

#### Scenario: Successful login
- **WHEN** an unlinked chat sends `/login alice correct-password` and
  `alice` is a user in the database with a matching password
- **THEN** the bot creates a temporary binding for the chat to `alice`,
  confirms success, and does not echo the password back
- **AND** the bot deletes the `/login` message from the chat if the
  Telegram API permits it, so the password does not linger in chat history

#### Scenario: Failed login
- **WHEN** a chat sends `/login` with an unknown username or wrong password
- **THEN** the bot replies with a generic authentication failure (no
  indication of whether the username exists) and creates no binding

#### Scenario: Command requires authentication
- **WHEN** a chat that is neither linked nor logged in sends `/generate`,
  `/gallery`, `/models`, `/model`, `/aspect`, `/size`, `/count`, `/delete`,
  or `/clear`
- **THEN** the bot replies asking the chat to `/login` first and performs
  no other action

#### Scenario: Logout clears temporary binding
- **WHEN** a chat with a temporary `/login` binding sends `/logout`
- **THEN** the bot clears that binding and any selected generation options,
  and confirms logout

#### Scenario: Logout on a linked chat
- **WHEN** a chat whose Telegram ID is linked to a user sends `/logout`
- **THEN** the bot replies that the chat is linked and stays authenticated;
  only an admin removing the link can end that chat's access

#### Scenario: Login rate limiting
- **WHEN** a chat submits failed `/login` attempts repeatedly
- **THEN** the bot applies the same login rate limiting as the web API
  (`internal/auth` rate limiter) keyed per chat, rejecting further attempts
  once the limit is hit

### Requirement: Automatic authentication via linked Telegram ID
The bot SHALL treat every message from a chat whose Telegram user ID has
been linked to an app user (via admin user management) as already
authenticated as that user, with no `/login` step and no expiry, for as
long as the link exists.

#### Scenario: Linked chat sends a command without logging in
- **WHEN** a chat whose Telegram user ID is linked to `alice` sends
  `/generate <prompt>` without ever sending `/login`
- **THEN** the bot processes the command as `alice`

#### Scenario: Linked authentication takes precedence
- **WHEN** a linked chat also holds a temporary `/login` binding to a
  different user
- **THEN** the bot authenticates the chat as the user linked to its
  Telegram ID, not the temporarily logged-in user

### Requirement: Admin authentication
The bot SHALL require a chat to present the configured admin password via
`/admin <password>` before it accepts any user-management command from
that chat. Admin authorization SHALL be scoped to the chat, bounded in
time, and SHALL NOT need to survive a process restart.

#### Scenario: Successful admin authentication
- **WHEN** a chat sends `/admin <password>` with the correct configured
  admin password
- **THEN** the bot authorizes the chat as admin, confirms success, and does
  not echo the password back
- **AND** the bot deletes the `/admin` message from the chat if the
  Telegram API permits it

#### Scenario: Failed admin authentication
- **WHEN** a chat sends `/admin` with an incorrect password
- **THEN** the bot replies with a generic authentication failure and does
  not authorize the chat as admin

#### Scenario: Admin command requires admin authentication
- **WHEN** a chat that has not authenticated as admin sends `/adduser`,
  `/addtelegramid`, or `/listusers`
- **THEN** the bot replies asking the chat to `/admin` first and performs
  no other action

#### Scenario: Admin authentication rate limiting
- **WHEN** a chat submits failed `/admin` attempts repeatedly
- **THEN** the bot rejects further attempts once the same class of limit
  used for `/login` is hit

#### Scenario: Admin authentication expires
- **WHEN** an admin-authorized chat's authorization window elapses
- **THEN** the bot requires `/admin <password>` again before accepting a
  further user-management command from that chat

### Requirement: User management
An admin-authorized chat SHALL be able to create app users, link
additional Telegram IDs to existing users, and list configured users - the
only way to provision an app user, since there is no other account-creation
path.

#### Scenario: Creating a user
- **WHEN** an admin-authorized chat sends `/adduser <username> <password>`
  with a username not already in use
- **THEN** the bot creates the user with that password (hashed before
  storage) and confirms creation

#### Scenario: Creating a user with linked Telegram IDs
- **WHEN** an admin-authorized chat sends `/adduser <username> <password>
  <telegram_id> [telegram_id...]` and none of the given Telegram IDs are
  already linked to another user
- **THEN** the bot creates the user and links every given Telegram ID to it
  in the same operation

#### Scenario: Duplicate username rejected
- **WHEN** an admin-authorized chat sends `/adduser <username> ...` for a
  username that already exists
- **THEN** the bot rejects the command, stating the username is taken, and
  creates nothing

#### Scenario: Telegram ID already linked elsewhere rejected
- **WHEN** an admin-authorized chat sends `/adduser` or `/addtelegramid`
  naming a Telegram ID already linked to a different user
- **THEN** the bot rejects the command, stating that Telegram ID is already
  linked, and makes no change

#### Scenario: Linking a Telegram ID to an existing user
- **WHEN** an admin-authorized chat sends `/addtelegramid <username>
  <telegram_id>` for an existing username and an unlinked Telegram ID
- **THEN** the bot links that Telegram ID to the user and confirms the
  link

#### Scenario: Linking a Telegram ID to an unknown user
- **WHEN** an admin-authorized chat sends `/addtelegramid <username>
  <telegram_id>` for a username that does not exist
- **THEN** the bot rejects the command, stating the username was not
  found, and links nothing

#### Scenario: Listing users
- **WHEN** an admin-authorized chat sends `/listusers`
- **THEN** the bot replies with every configured username and its linked
  Telegram IDs (if any), paged so a single reply stays within Telegram's
  message limits

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
- **WHEN** an authenticated chat sends `/generate <prompt>` or a plain text
  message
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
- **THEN** the bot deletes every image owned by that chat's authenticated
  user and confirms how many were removed
