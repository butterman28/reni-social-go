# ══════════════════════════════════════════════════════
# Stage 1 — build
# ══════════════════════════════════════════════════════
FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -tags=docker → compiler picks config-docker.go + redis_client_docker.go
RUN CGO_ENABLED=0 go build -tags=docker -ldflags="-s -w" -o bin/api    ./cmd/api \
 && CGO_ENABLED=0 go build -tags=docker -ldflags="-s -w" -o bin/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -tags=docker -ldflags="-s -w" -o bin/schema ./cmd/schema

# ══════════════════════════════════════════════════════
# Stage 2 — runtime
# ══════════════════════════════════════════════════════
FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates tzdata
WORKDIR /app

COPY --from=builder /app/bin/api    ./api
COPY --from=builder /app/bin/worker ./worker
COPY --from=builder /app/bin/schema ./schema

# schema binary globs ./schemas/*.yaml at runtime
COPY --from=builder /app/schemas ./schemas

# bake the docker env file — config-docker.go loads it on startup
COPY --from=builder /app/.env_docker ./.env_docker

EXPOSE 1040
CMD ["./api"]
