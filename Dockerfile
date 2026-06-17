# ──────────────────────────────────────────────────────────────────────────────
# Stage 1: Build
# ──────────────────────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Download dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build both binaries
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags=prod -ldflags="-s -w" -o bin/api ./cmd/api

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags=prod -ldflags="-s -w" -o bin/worker ./cmd/worker

# ──────────────────────────────────────────────────────────────────────────────
# Stage 2: Runtime
# ──────────────────────────────────────────────────────────────────────────────
FROM alpine:3.19

# ffmpeg for media processing, ca-certificates for HTTPS to social APIs
RUN apk add --no-cache ffmpeg ca-certificates tzdata

WORKDIR /app

# Copy compiled binaries from builder
COPY --from=builder /app/bin/api    ./api
COPY --from=builder /app/bin/worker ./worker

# Expose API port (matches config-prod.go Port)
EXPOSE 1041

# Default: run the API server.
# Override CMD to run the worker: docker run ... ./worker
CMD ["./api"]
