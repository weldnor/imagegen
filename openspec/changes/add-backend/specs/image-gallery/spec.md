## Purpose

Persist generated images on the server, scoped to the user who created them, so galleries survive browser refreshes and are available from any device — replacing the previous browser-only IndexedDB storage.

## ADDED Requirements

### Requirement: Generated images are persisted server-side

The backend SHALL store each generated image's bytes on a configured storage location and its metadata in PostgreSQL. Metadata SHALL include at least: owning user, prompt, model, image size / quality, aspect ratio, number of reference images used, and creation timestamp.

#### Scenario: Image persisted on generation

- **WHEN** an image is generated successfully for a user
- **THEN** its bytes are written to the storage location
- **AND** a metadata row is created in the database linked to that user

#### Scenario: Storage location not writable

- **WHEN** the backend starts and the configured image storage location does not exist or is not writable
- **THEN** it fails to start with a clear error message

#### Scenario: Persistence failure after generation

- **WHEN** an image is generated but writing its bytes or metadata fails
- **THEN** the backend returns an error for that image rather than reporting success

### Requirement: Users can list their gallery

The backend SHALL expose an authenticated endpoint that returns the calling user's images as a list of metadata entries ordered newest-first, each with an identifier usable to fetch the image bytes.

#### Scenario: List returns own images newest-first

- **WHEN** an authenticated user requests their gallery list
- **THEN** the response contains that user's image metadata entries ordered by creation time descending

#### Scenario: Empty gallery

- **WHEN** a user who has generated no images requests their gallery list
- **THEN** the response is an empty list with a success status

### Requirement: Users can fetch an image's bytes

The backend SHALL expose an authenticated endpoint that returns the bytes of a single image the calling user owns, with a correct image content type.

#### Scenario: Fetch own image

- **WHEN** a user requests the bytes for an image identifier they own
- **THEN** the backend responds `200 OK` with the image bytes and an `image/*` content type

#### Scenario: Fetch missing or non-owned image

- **WHEN** a user requests an image identifier that does not exist or belongs to another user
- **THEN** the backend responds `404 Not Found`

### Requirement: Users can delete images

The backend SHALL expose an authenticated endpoint to delete a single image the calling user owns, and an endpoint (or parameter) to clear the calling user's entire gallery. Deletion SHALL remove both the metadata row and the stored bytes.

#### Scenario: Delete one image

- **WHEN** a user deletes an image identifier they own
- **THEN** the metadata row and the stored bytes are removed
- **AND** a subsequent gallery list for that user no longer includes it

#### Scenario: Clear entire gallery

- **WHEN** a user clears their gallery
- **THEN** all of that user's image rows and stored bytes are removed
- **AND** no other user's images are affected

#### Scenario: Delete non-owned image

- **WHEN** a user attempts to delete an image identifier that belongs to another user
- **THEN** the backend responds `404 Not Found` and removes nothing

### Requirement: Database schema is created automatically

The backend SHALL create or migrate its PostgreSQL schema on startup (or via an explicit migration step run before startup), so a fresh database becomes usable without manual SQL.

#### Scenario: Fresh database

- **WHEN** the backend starts against an empty PostgreSQL database
- **THEN** the required tables are created
- **AND** the service becomes ready to handle requests

#### Scenario: Database unreachable

- **WHEN** the backend cannot connect to PostgreSQL at startup
- **THEN** it fails to start with a clear error message
