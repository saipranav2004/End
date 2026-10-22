# syntax=docker/dockerfile:1

# ---------- build stage ----------
FROM golang:1.26-alpine AS builder
WORKDIR /src

# Dependencies first so the module cache survives source-only changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOFLAGS=-mod=readonly \
    go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server

# ---------- runtime stage ----------
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S nhi \
    && adduser -u 10001 -S -G nhi -h /app -s /sbin/nologin nhi
WORKDIR /app

# No secrets are baked in: every credential comes from environment variables
# (see .env.example and docker-compose.yml).
COPY --from=builder /out/api-server /app/api-server
COPY configs/config.yaml /app/configs/config.yaml
# Migrations are applied by hand (docker compose --profile tools run --rm migrate).
COPY migrations/ /app/migrations/

ENV NHI_CONFIG_FILE=/app/configs/config.yaml
USER nhi
EXPOSE 8080

# /health needs no database, so a slow Postgres never restarts the container.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1

# Direct exec so SIGTERM reaches the server and its 20 s graceful drain runs.
ENTRYPOINT ["/app/api-server"]
