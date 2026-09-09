## Purpose

Let authenticated users generate images through the backend without ever handling an OpenRouter API key, by accepting a generation request, attaching the server-held key, forwarding it to OpenRouter, and returning the resulting images.

## ADDED Requirements

### Requirement: Backend proxies generation to OpenRouter

The backend SHALL expose an authenticated endpoint that accepts a single image-generation request and forwards it to OpenRouter's chat/completions API using an API key held only by the backend. The browser SHALL never receive the key and SHALL never call OpenRouter directly.

#### Scenario: Successful single-image generation

- **WHEN** an authenticated user submits a request with a non-empty prompt and a supported model
- **THEN** the backend calls OpenRouter with the configured `OPENROUTER_API_KEY`
- **AND** returns the generated image to the client as image bytes or a data URI plus its metadata

#### Scenario: API key never leaves the backend

- **WHEN** any generation response or error is returned to the client
- **THEN** the response body and headers contain no OpenRouter API key

#### Scenario: Missing prompt

- **WHEN** a request has an empty or whitespace-only prompt
- **THEN** the backend responds `400 Bad Request` with a descriptive error
- **AND** does not call OpenRouter

#### Scenario: OpenRouter key not configured

- **WHEN** the backend starts without `OPENROUTER_API_KEY` set
- **THEN** it fails to start with a clear error message

### Requirement: Supported generation options are honored

The endpoint SHALL accept and forward the same generation options the current tool supports: model selection from the known model list, image size / quality (for models that support it), aspect ratio, and one or more reference images. Options not supported by the chosen model SHALL be omitted from the OpenRouter call rather than causing a failure.

#### Scenario: Model-specific options

- **WHEN** a Gemini model is selected with an image size and aspect ratio
- **THEN** the backend includes the image-config (size + aspect ratio) in the OpenRouter request

#### Scenario: Reference images forwarded

- **WHEN** a request for a model that supports image input includes reference images
- **THEN** the backend forwards them as image content parts in the OpenRouter message

#### Scenario: Unknown model rejected

- **WHEN** a request names a model that is not in the backend's known model list
- **THEN** the backend responds `400 Bad Request` and does not call OpenRouter

#### Scenario: Unsupported option ignored

- **WHEN** a request selects a model that does not support reference images but includes them
- **THEN** the backend drops the reference images and proceeds with generation

### Requirement: Batch generation

The endpoint SHALL support requesting between 1 and 8 images for a single prompt. Each image in a batch SHALL be generated independently so that a partial failure still returns the images that succeeded.

#### Scenario: Full batch succeeds

- **WHEN** a user requests 4 images
- **THEN** the client receives 4 generated images

#### Scenario: Partial batch failure

- **WHEN** a user requests 4 images and one generation call to OpenRouter fails
- **THEN** the client receives the 3 successful images
- **AND** an indication that 1 failed

#### Scenario: Count out of range

- **WHEN** a user requests fewer than 1 or more than 8 images
- **THEN** the backend responds `400 Bad Request`

### Requirement: Upstream errors are surfaced safely

When OpenRouter returns an error or is unreachable, the backend SHALL return a non-2xx status with a human-readable message describing the failure, without leaking credentials or internal stack traces.

#### Scenario: OpenRouter returns an error

- **WHEN** OpenRouter responds with a 4xx/5xx error
- **THEN** the backend responds with a 4xx/5xx status and a message derived from OpenRouter's error
- **AND** the message contains no API key

#### Scenario: OpenRouter unreachable or times out

- **WHEN** the request to OpenRouter times out or the connection fails
- **THEN** the backend responds `502 Bad Gateway` (or `504 Gateway Timeout`) with a descriptive message

### Requirement: Successful generations are stored

Every image successfully generated through the endpoint SHALL be persisted to the authenticated user's gallery (see the `image-gallery` capability) before or as part of returning the response, so the image survives a browser refresh.

#### Scenario: Generated image appears in gallery

- **WHEN** a user generates an image successfully
- **THEN** a subsequent gallery-list request for that user includes the new image with its prompt, model, and options
