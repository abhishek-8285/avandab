# syntax=docker/dockerfile:1

# ── Docker build context ──────────────────────────────────────────────
# No .dockerignore by design: the build COPYs only allow-listed paths
# (go.mod/go.sum, then source) and the runtime stage keeps only the binary
# + db/migrations + templates/static + .env.example. Never add a COPY of
# .env / *.db / server.log / node_modules / .git — secrets arrive via
# environment at run time, not in image layers.

# Build stage
FROM golang:1.26-bookworm AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build (VERSION arg stamps ?v= cache-busting; defaults to deploys via scripts)
ARG VERSION=docker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION}" -o bin/mvtms ./cmd/server

# Runtime stage
FROM gcr.io/distroless/static-debian12

COPY --from=builder /app/bin/mvtms /mvtms
COPY --from=builder /app/db/migrations /db/migrations
COPY --from=builder /app/internal/templates /internal/templates
COPY --from=builder /app/internal/static /internal/static
COPY --from=builder /app/.env.example /.env.example

WORKDIR /
EXPOSE 8080

ENTRYPOINT ["/mvtms"]