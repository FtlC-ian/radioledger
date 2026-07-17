# RadioLedger Web UI — Multi-stage Docker build
#
# Build context: repo root (radioledger/)

# Stage 1: Build the Quasar SPA from the root workspace lockfile
FROM node:22-alpine AS builder

WORKDIR /src

# Copy root + workspace manifests first for layer caching.
# quasar prepare runs as postinstall and needs the web workspace config files.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY desktop/package.json ./desktop/
COPY docs/package.json ./docs/
COPY e2e/package.json ./e2e/
COPY packages/ui/package.json ./packages/ui/
COPY site/package.json ./site/
COPY web/package.json web/quasar.config.ts web/tsconfig.json web/index.html ./web/

# Install only the web workspace using the authoritative root lockfile.
RUN corepack enable \
    && corepack prepare pnpm@11.3.0 --activate \
    && pnpm install --frozen-lockfile --filter radioledger-web...

# Copy the workspace sources needed for the web build.
COPY packages/ ./packages/
COPY web/ ./web/

# Build the SPA (outputs to web/dist/spa/).
# Set both env names for backward compatibility with existing config fallbacks.
RUN RADIOLEDGER_API_BASE_URL="" API_BASE_URL="" pnpm --filter radioledger-web build

# ──────────────────────────────────────────────────────────────────────────────
# Stage 2: nginx runtime (serve the compiled SPA + proxy API)
FROM nginx:1.27-alpine AS runtime

# Remove the default nginx config
RUN rm /etc/nginx/conf.d/default.conf

# Copy nginx configuration (SPA serving + /v1/* proxy to api service)
COPY web/docker/nginx.conf /etc/nginx/conf.d/radioledger.conf

# Copy the compiled SPA from builder stage
COPY --from=builder /src/web/dist/spa/ /usr/share/nginx/html/

EXPOSE 80

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:80/ || exit 1
