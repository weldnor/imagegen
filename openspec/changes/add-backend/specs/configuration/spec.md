## Purpose

Make the backend fully configurable through environment variables alone — no config file — so it runs identically from a shell, a `.env` file, Docker Compose, or an orchestrator, and fails fast with a clear message when a required value is missing.

## ADDED Requirements

### Requirement: All configuration comes from environment variables

Every runtime setting the backend needs SHALL be readable from an environment variable. The backend SHALL NOT require or read a configuration file. At minimum the following are configurable: OpenRouter API key, user credentials, database connection string, image storage directory, listen address, session time-to-live, session-cookie `Secure` flag, and the maximum request body size.

#### Scenario: Runs from environment only

- **WHEN** the backend is started with only environment variables set and no config file present
- **THEN** it starts and serves requests normally

#### Scenario: No config file is consulted

- **WHEN** the backend starts
- **THEN** it does not read any configuration file from disk

### Requirement: Required settings are validated at startup

The backend SHALL validate configuration before it begins listening. If a required variable (OpenRouter API key, user credentials, database URL) is missing or empty, or a provided value is unusable (credential list yields zero users, image storage directory not writable, database unreachable), the backend SHALL exit with a non-zero status and a message identifying the offending variable.

#### Scenario: Missing required variable

- **WHEN** the backend starts without the OpenRouter API key set
- **THEN** it exits non-zero with a message naming that variable
- **AND** does not begin listening

#### Scenario: Unusable value

- **WHEN** the image storage directory is set to a path that is not writable
- **THEN** the backend exits non-zero with a message identifying the directory

#### Scenario: All required values present

- **WHEN** every required variable is set to a valid value
- **THEN** the backend completes startup and listens on the configured address

### Requirement: Optional settings have documented defaults

Optional settings SHALL have defaults that let the app run locally without setting them. The defaults SHALL be documented alongside every variable in a single reference (`.env.example` and the README).

#### Scenario: Optional variables omitted

- **WHEN** the backend starts with only the required variables set
- **THEN** each optional setting takes its documented default (e.g. listen address, storage directory, session TTL, upload limit)

#### Scenario: Reference lists every variable

- **WHEN** a reader consults `.env.example`
- **THEN** it lists every environment variable the backend reads, marks which are required, and shows the default for each optional one
