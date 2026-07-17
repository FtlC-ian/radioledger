# RadioLedger API — Multi-stage Docker build
#
# Build context: repo root (radioledger/)
# This is required because the api/ module has a replace directive for pkg/adif.
#
# Stage 1: Build the Go binaries
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Create output directory for compiled binaries
RUN mkdir /out

# Install goose for SQL migrations (baked into the image)
RUN GOBIN=/usr/local/bin go install github.com/pressly/goose/v3/cmd/goose@v3.24.3

# Copy go module files first for dependency caching
COPY api/go.mod api/go.sum ./api/
COPY pkg/adif/go.mod pkg/adif/go.sum ./pkg/adif/
COPY pkg/plan/go.mod pkg/plan/go.sum ./pkg/plan/

# Download dependencies (cached layer — only re-runs when go.mod/go.sum change)
WORKDIR /src/api
RUN go mod download

WORKDIR /src

# Copy source code
COPY api/      ./api/
COPY pkg/      ./pkg/
COPY database/ ./database/

# Build the API server binary (static, CGO disabled for Alpine runtime)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -C api \
    -ldflags="-s -w -extldflags=-static" \
    -o /out/radioledger-api \
    ./cmd/server

# Build the River queue migration binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -C api \
    -ldflags="-s -w -extldflags=-static" \
    -o /out/migrate-river \
    ./cmd/migrate-river

# ──────────────────────────────────────────────────────────────────────────────
# Stage 2: Minimal runtime image
FROM alpine:3.19 AS runtime

# Install CA certificates (for outbound HTTPS to LoTW, QRZ, etc.), wget for
# health-check probes, and psql for the fail-closed legacy schema baseline.
# No package cache retained.
RUN apk add --no-cache ca-certificates tzdata wget poppler-utils postgresql-client

# Create non-root user/group to run the server
RUN addgroup -S radioledger && adduser -S -G radioledger radioledger

# Copy compiled binaries from builder stage
COPY --from=builder /out/radioledger-api   /usr/local/bin/radioledger-api
COPY --from=builder /out/migrate-river     /usr/local/bin/migrate-river
COPY --from=builder /usr/local/bin/goose   /usr/local/bin/goose

# Copy SQL migration files (goose runs these on startup)
COPY --from=builder /src/database/migrations/ /app/migrations/
COPY --from=builder /src/database/bootstrap_legacy_goose.sql /app/bootstrap_legacy_goose.sql
RUN chmod -R a+rX /app/migrations \
    && chmod 0644 /app/bootstrap_legacy_goose.sql

# Copy entrypoint script (sourced from the build context, not from builder stage)
COPY api/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh

# Temp dir for ADIF import staging (mounted as tmpfs in compose for read-only FS)
RUN mkdir -p /tmp/radioledger && chown radioledger:radioledger /tmp/radioledger

WORKDIR /app
USER radioledger

EXPOSE 8080

# Liveness probe — /health returns 200 if the process is alive
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
