## Purpose

Describe the observable behavior of the browser application after it is adapted to run against the backend: a login form gates the app, no API-key input, all generation and gallery actions routed through the backend API, and gallery state loaded from the server.

## ADDED Requirements

### Requirement: A login form gates the application

On load the UI SHALL ask the backend whether the current session is valid (`GET /api/session`). If it is not, the UI SHALL show a login form (username and password) instead of the app. On submit it SHALL call `POST /api/login`; on success it SHALL show the app, on failure it SHALL show an error and keep the form. The app SHALL provide a visible logout control that calls `POST /api/logout` and returns to the login form.

#### Scenario: No session on load

- **WHEN** a user opens the app without a valid session
- **THEN** a username/password login form is shown
- **AND** the generation controls and gallery are not shown

#### Scenario: Successful login

- **WHEN** the user submits correct credentials in the login form
- **THEN** the app view replaces the login form
- **AND** the gallery loads for that user

#### Scenario: Failed login

- **WHEN** the user submits incorrect credentials
- **THEN** an error message is shown and the login form stays visible
- **AND** no app content is revealed

#### Scenario: Logout

- **WHEN** the user activates the logout control
- **THEN** the app calls the logout endpoint and returns to the login form

#### Scenario: Valid session on load

- **WHEN** a user opens the app with a still-valid session
- **THEN** the app is shown directly without the login form

### Requirement: No API-key input in the UI

The UI SHALL NOT present any field for entering, saving, or displaying an OpenRouter API key, and SHALL NOT read or write an API key in `localStorage` or any other browser storage.

#### Scenario: Sidebar has no key field

- **WHEN** a user loads the application
- **THEN** there is no "OpenRouter API Key" input, no "Save Key" button, and no related help text

#### Scenario: No key in browser storage

- **WHEN** the application runs
- **THEN** it never sets an `imagen_api_key` (or equivalent) value in browser storage

### Requirement: Generation goes through the backend

When the user clicks Generate, the UI SHALL send the prompt, selected model, options, reference images, and requested count to the backend generation endpoint and render the returned images. It SHALL NOT call `openrouter.ai` directly.

#### Scenario: Generate uses the backend

- **WHEN** a user enters a prompt and clicks Generate
- **THEN** the browser issues a request to the backend generation endpoint
- **AND** issues no request to `openrouter.ai`

#### Scenario: Generation error shown to user

- **WHEN** the backend returns an error for a generation request
- **THEN** the UI shows a visible error message (e.g. a toast) with the reason
- **AND** removes the corresponding loading placeholders

#### Scenario: Existing generation options preserved

- **WHEN** a user selects a model, image size, aspect ratio, batch count, or adds reference images
- **THEN** those controls behave as they do today and their values are included in the backend request

### Requirement: Gallery is loaded from the server

The UI SHALL populate the gallery from the backend gallery-list endpoint on load, and SHALL use backend endpoints for viewing image bytes, deleting an image, and clearing the gallery. It SHALL NOT use IndexedDB for image storage.

#### Scenario: Gallery loads on startup

- **WHEN** a user opens the application
- **THEN** the gallery is populated from the backend list endpoint
- **AND** shows the empty state only when the backend returns no images

#### Scenario: Gallery persists across refresh

- **WHEN** a user generates images and then reloads the page
- **THEN** the previously generated images are still shown, fetched from the server

#### Scenario: Delete and clear use the backend

- **WHEN** a user deletes an image or clears the gallery
- **THEN** the UI calls the corresponding backend endpoint
- **AND** updates the displayed gallery to match the server state

#### Scenario: No IndexedDB usage

- **WHEN** the application manages gallery images
- **THEN** it does not open or write to an IndexedDB database

### Requirement: Recreate and reference reuse still work

The UI SHALL keep the "recreate" action (restore a past image's prompt, model, and options into the controls) and the ability to add a generated image as a reference for a new generation, using data from the server-provided gallery entries.

The backend stores only the number of reference images used for a generation, not their bytes (see the `image-gallery` capability and design decision 5). Consequently "recreate" SHALL NOT restore the original reference images; it restores the prompt, model, image size, and aspect ratio, and clears the reference list. When the recreated image was generated with one or more references, the UI SHOULD indicate that the references must be re-added manually.

#### Scenario: Recreate from a gallery image

- **WHEN** a user triggers "recreate" on a gallery image
- **THEN** the prompt, model, image size, and aspect ratio are restored into the controls
- **AND** the reference list is cleared

#### Scenario: Recreate an image that used references

- **WHEN** a user triggers "recreate" on a gallery image whose metadata reports a non-zero reference count
- **THEN** the controls are restored as above
- **AND** the UI indicates that the reference images were not stored and must be re-added

#### Scenario: Use a generated image as reference

- **WHEN** a user chooses "use as reference" on a gallery image
- **THEN** that image's bytes are fetched from the backend and added to the reference list for the next generation request

### Requirement: Session expiry during use returns to the login form

If any backend API call returns `401 Unauthorized` while the user is in the app (for example the session expired), the UI SHALL return to the login form rather than failing silently, and SHALL let the user log back in and resume.

#### Scenario: Session expires mid-use

- **WHEN** a backend API call returns `401 Unauthorized` after the user was already in the app
- **THEN** the UI shows the login form again
- **AND** after a successful re-login the app view and gallery are restored
