# ==============================================================================
# Multi-Stage Dockerfile for Backup Platform (Modular Monolith)
# ==============================================================================

# Stage 1: Build binary using official Go Alpine builder
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install required build utilities
RUN apk add --no-cache git ca-certificates tzdata

# Cache module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy application source code
COPY . .

# Compile self-contained, statically-linked Go binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /build/bin/backup-platform \
    ./cmd/server

# Stage 2: Minimal non-root runtime image
FROM alpine:3.21

# Install CA certificates for outgoing TLS connections (e.g. cPanel APIs) and timezone data
RUN apk add --no-cache ca-certificates tzdata wget

# Create dedicated non-root service account (UID:GID 10001:10001)
RUN addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app

# Copy compiled binary from builder
COPY --from=builder /build/bin/backup-platform /app/backup-platform

# Restrict binary ownership and permissions
RUN chown -R appuser:appgroup /app && \
    chmod 0555 /app/backup-platform

# Run as non-root user
USER 10001:10001

# Document internal listening port
EXPOSE 8080

# Configure container health probe
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/api/v1/health | grep -q '"status":"ok"' || exit 1

ENTRYPOINT ["/app/backup-platform"]
