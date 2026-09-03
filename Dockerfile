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

# Stage 2: Verified Restic 0.19.1 Supply Chain
FROM alpine:3.21 AS restic-builder

WORKDIR /restic-verify

# Install verification and extraction tools
RUN apk add --no-cache gnupg bzip2 curl ca-certificates

# Copy vendored official release signing key
COPY build/restic/restic_release_key.asc ./

ENV RESTIC_VERSION=0.19.1
ENV EXPECTED_FINGERPRINT="CF8F18F2844575973F79D4E191A6868BD3F7A907"

# 1. Import key and verify exact fingerprint match (fail-closed if mismatch)
RUN gpg --batch --import restic_release_key.asc && \
    ACTUAL_FP=$(gpg --batch --with-colons --fingerprint 0x91A6868BD3F7A907 | grep -E '^fpr:' | head -n 1 | cut -d: -f10) && \
    echo "Verified key fingerprint: ${ACTUAL_FP}" && \
    if [ "${ACTUAL_FP}" != "${EXPECTED_FINGERPRINT}" ]; then \
        echo "FATAL: Fingerprint mismatch! Expected ${EXPECTED_FINGERPRINT}, got ${ACTUAL_FP}" && exit 1; \
    fi

# 2. Download official release signatures and checksums
RUN curl -sSL -o SHA256SUMS "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/SHA256SUMS" && \
    curl -sSL -o SHA256SUMS.asc "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/SHA256SUMS.asc" && \
    gpg --batch --verify SHA256SUMS.asc SHA256SUMS

# 3. Download Linux amd64 release archive
RUN curl -sSL -o "restic_${RESTIC_VERSION}_linux_amd64.bz2" "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_amd64.bz2"

# 4. Verify SHA256 against signature-verified SHA256SUMS
RUN grep "restic_${RESTIC_VERSION}_linux_amd64.bz2" SHA256SUMS | sha256sum -c -s

# 5. Decompress and verify version
RUN bzip2 -d "restic_${RESTIC_VERSION}_linux_amd64.bz2" && \
    mv "restic_${RESTIC_VERSION}_linux_amd64" /usr/local/bin/restic && \
    chmod 0555 /usr/local/bin/restic && \
    /usr/local/bin/restic version | grep -q "restic ${RESTIC_VERSION}"

# Stage 3: Minimal non-root runtime image
FROM alpine:3.21

# Install CA certificates for outgoing TLS connections and timezone data
RUN apk add --no-cache ca-certificates tzdata wget

# Create dedicated non-root service account (UID:GID 10001:10001)
RUN addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app

# Copy compiled binary from builder
COPY --from=builder /build/bin/backup-platform /app/backup-platform

# Copy verified Restic 0.19.1 binary from restic-builder (owned by root, executable, non-writable by appuser)
COPY --from=restic-builder /usr/local/bin/restic /usr/local/bin/restic

# Restrict binary ownership and permissions
RUN chown -R appuser:appgroup /app && \
    chmod 0555 /app/backup-platform && \
    chown root:root /usr/local/bin/restic && \
    chmod 0555 /usr/local/bin/restic

# Run as non-root user
USER 10001:10001

# Document internal listening port
EXPOSE 8080

# Configure container health probe
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/api/v1/health | grep -q '"status":"ok"' || exit 1

ENTRYPOINT ["/app/backup-platform"]
