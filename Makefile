.PHONY: build run test test-race lint fmt vet generate migrate-up migrate-down clean docker dev build-css check check-fast check-fmt staticcheck check-security build-rag

## Build CSS from Tailwind source
build-css:
	npx @tailwindcss/cli -i src/input.css -o internal/static/css/tailwind.css --minify

## Build the server binary
build:
	go build -o bin/mvtms ./cmd/server/

## Build the RAG CLI
build-rag:
	go build -o bin/rag ./cmd/rag/

## Run the server
run:
	go run ./cmd/server/

## Run tests
test:
	go test -v ./...

## Run tests with race detector
test-race:
	go test -race -v ./...

## Run linter
lint:
	golangci-lint run

## Format code
fmt:
	go fmt ./...

## Vet code
vet:
	go vet ./...

## Generate sqlc code
generate:
	sqlc generate

## Run migrations
migrate-up:
	goose sqlite $(DATABASE_URL) up

migrate-down:
	goose sqlite $(DATABASE_URL) down

## Clean build artifacts
clean:
	rm -rf bin/ coverage.out *.db *.db-wal *.db-shm

## Run tests and build
ci: check-fmt vet test build

## Fast fail: fmt check, vet, staticcheck, quick test (fails immediately on first error)
check-fast: check-fmt vet staticcheck test

## Full check: fmt check, vet, build, staticcheck, race tests, security scans
check: check-fmt vet build staticcheck test-race check-security

check-fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; fi

staticcheck:
	@bin=$$(command -v staticcheck || echo "$$(go env GOPATH)/bin/staticcheck"); \
	if [ ! -x "$$bin" ]; then echo "staticcheck not installed: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; fi; \
	"$$bin" ./...

check-security:
	./scripts/security-check.sh

check-security-strict:
	SECURITY_GATE_STRICT=1 ./scripts/security-check.sh

## Run development server
dev:
	air

## Build Docker image
docker:
	docker build -t mvtms:latest .