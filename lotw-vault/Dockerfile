# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# Install build tools
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy dependency files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build a fully static binary.
# modernc.org/sqlite is pure Go — no CGo needed, scratch base works fine.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -extldflags '-static'" \
    -o /lotw-vault ./cmd/lotw-vault/

# Create the /data directory and a minimal passwd file so we can run as uid 65534.
# We do this in the build stage since scratch has no shell or useradd.
RUN mkdir -p /data && \
    echo "nobody:x:65534:65534:nobody:/:/sbin/nologin" > /etc/passwd.minimal && \
    chown 65534:65534 /data

# ── Stage 2: Minimal runtime image ──────────────────────────────────────────
# scratch = no shell, no package manager, smallest possible attack surface.
FROM scratch

# passwd file so the process has a valid user identity.
COPY --from=builder /etc/passwd.minimal /etc/passwd

# /data directory for the SQLite database (owned by nobody).
COPY --from=builder --chown=65534:65534 /data /data

# The vault binary.
COPY --from=builder /lotw-vault /lotw-vault

# Run as nobody (uid 65534) — no root access.
USER 65534:65534

# Persistent SQLite data directory — mount a named volume here in production.
VOLUME ["/data"]

# No EXPOSE: the vault has no externally reachable ports.
# The main RadioLedger API contacts it via Docker internal network.

# Default: run the server on :8080 (plain HTTP, TLS terminated at API gateway).
ENTRYPOINT ["/lotw-vault"]
CMD ["-addr", ":8080", "-db", "/data/certs.db"]

# Health check via the /health endpoint (called from the API network, not externally).
# Note: wget/curl are not available in scratch. Use an HTTP probe from the API side,
# or add a sidecar health prober if native Docker HEALTHCHECK is required.
