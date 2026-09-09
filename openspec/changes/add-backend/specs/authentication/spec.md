## Purpose

Give each user their own username and password, gate the application behind an in-app login form, and maintain a server-side session so that user identity can scope galleries and generation history and one user can never reach another user's data.

## ADDED Requirements

### Requirement: User credentials are configured via environment

User credentials SHALL be provided to the backend through an environment variable at startup, as a list of `username` + password-hash pairs (bcrypt). At least one user MUST be configurable. The backend SHALL NOT store or log passwords in plaintext, and credential verification SHALL be constant-time (bcrypt comparison).

#### Scenario: No users configured

- **WHEN** the backend starts with no users configured
- **THEN** it exits with a non-zero status and a clear error message
- **AND** does not begin listening for requests

#### Scenario: Multiple users configured

- **WHEN** two or more users are configured
- **THEN** each can log in with their own username and password
- **AND** each is treated as a distinct identity

#### Scenario: Malformed credential entry

- **WHEN** the credential list contains an entry that is not a `username`/hash pair
- **THEN** the backend exits at startup with an error naming the problem

#### Scenario: Credentials absent from logs

- **WHEN** the backend logs a request, a login attempt, or an authentication failure
- **THEN** the log entry contains neither the password nor the password hash

### Requirement: Login establishes a session

The backend SHALL expose `POST /api/login` accepting a username and password. On correct credentials it SHALL create a server-side session and return it to the browser as an HttpOnly session cookie (`Secure` by default, `SameSite=Lax`, `Path=/`). On incorrect credentials it SHALL respond `401 Unauthorized` with a generic message that does not reveal whether the username or the password was wrong.

#### Scenario: Successful login

- **WHEN** a client posts valid credentials to `/api/login`
- **THEN** the response is `200 OK` with the username in the body
- **AND** a `Set-Cookie` header creates an HttpOnly session cookie

#### Scenario: Wrong password

- **WHEN** a client posts a known username with an incorrect password
- **THEN** the response is `401 Unauthorized` with a generic error
- **AND** no session cookie is set

#### Scenario: Unknown username

- **WHEN** a client posts a username that is not configured
- **THEN** the response is `401 Unauthorized` with the same generic error as a wrong password

#### Scenario: Malformed login request

- **WHEN** a client posts to `/api/login` with a missing or empty username or password
- **THEN** the response is `400 Bad Request`

### Requirement: Session cookie gates all data endpoints

Every API route that reads or writes user data (generation, gallery list/fetch/delete) SHALL require a valid, unexpired session cookie. Requests without a cookie, with an unknown cookie, or with an expired session SHALL receive `401 Unauthorized` and no side effects. `POST /api/login` and an optional health check SHALL be the only unauthenticated endpoints. Static frontend assets MAY be served without a session (the login form must load).

#### Scenario: Data request without a session

- **WHEN** a client calls a generation or gallery endpoint with no session cookie
- **THEN** the response is `401 Unauthorized`
- **AND** nothing is generated, stored, or deleted

#### Scenario: Data request with an expired session

- **WHEN** a client calls a data endpoint with a session cookie whose session has passed its expiry
- **THEN** the response is `401 Unauthorized`
- **AND** the session is treated as invalid from then on

#### Scenario: Data request with a valid session

- **WHEN** a client calls a data endpoint with a valid session cookie
- **THEN** the request is processed as the session's user

#### Scenario: Health check needs no session

- **WHEN** an operator requests the health-check route (if enabled) with no cookie
- **THEN** the response is `200 OK` and exposes no user data or configuration

### Requirement: Current session can be queried

The backend SHALL expose `GET /api/session` that returns the authenticated user's identity when the session cookie is valid, and `401 Unauthorized` otherwise, so the frontend can decide whether to show the login form or the app.

#### Scenario: Query with a valid session

- **WHEN** a client calls `/api/session` with a valid cookie
- **THEN** the response is `200 OK` with the username

#### Scenario: Query without a session

- **WHEN** a client calls `/api/session` with no cookie or an invalid one
- **THEN** the response is `401 Unauthorized`

### Requirement: Logout ends the session

The backend SHALL expose `POST /api/logout` that invalidates the current server-side session and clears the session cookie. After logout, the previously held cookie SHALL NOT grant access.

#### Scenario: Logout then reuse old cookie

- **WHEN** a user logs out and then repeats a request with the same (now-cleared) cookie value
- **THEN** the response is `401 Unauthorized`

#### Scenario: Logout without a session

- **WHEN** a client calls `/api/logout` with no valid session
- **THEN** the response is still a success status and no error is raised

### Requirement: Sessions expire

Each session SHALL have an expiry derived from a configurable time-to-live. Expired sessions SHALL be rejected and MAY be purged by the backend. Removing a user from the configured credential list SHALL prevent new logins for that user; existing sessions for that user SHALL stop working no later than their expiry.

#### Scenario: TTL is configurable

- **WHEN** the session time-to-live is set via configuration
- **THEN** newly created sessions expire after that duration

#### Scenario: Removed user cannot log in

- **WHEN** a username is removed from the configured credential list and the backend restarts
- **THEN** that username can no longer authenticate at `/api/login`

### Requirement: Authenticated user scopes user-owned data

The session's user identity SHALL be the key that scopes gallery data and generation history. One user MUST NOT be able to read, modify, or delete another user's stored images through any endpoint.

#### Scenario: User sees only their own gallery

- **WHEN** user A and user B have each generated images
- **THEN** a gallery-list request in user A's session returns only user A's images

#### Scenario: Cross-user access is denied

- **WHEN** user A requests or deletes an image that belongs to user B by its identifier
- **THEN** the backend responds `404 Not Found` and performs no change
