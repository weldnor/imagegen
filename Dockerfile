# syntax=docker/dockerfile:1

# ---- Build stage: compile a static binary with the frontend embedded ----
# The module requires Go 1.26 (see go.mod), so the build image is pinned to 1.26
# rather than the 1.23 mentioned in the original design note.
FROM golang:1.26 AS build

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build. index.html, favicon.svg, src/ and migrations/*.sql are pulled into the
# binary via //go:embed, so the whole repo is copied in.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Runtime stage: small, non-root ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app -u 65532 app \
    && mkdir -p /data/images && chown -R app:app /data

COPY --from=build /out/server /usr/local/bin/server

USER app

ENV IMAGE_STORAGE_DIR=/data/images \
    LISTEN_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/server"]
