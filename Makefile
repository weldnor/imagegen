.PHONY: build run migrate test fmt vet tidy clean docker-up docker-down

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

migrate:
	go run ./cmd/server -migrate-only

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

docker-up:
	docker compose up --build

docker-down:
	docker compose down
