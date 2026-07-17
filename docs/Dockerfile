FROM node:22-alpine AS builder
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY desktop/package.json ./desktop/
COPY docs/package.json ./docs/
COPY e2e/package.json ./e2e/
COPY packages/ui/package.json ./packages/ui/
COPY site/package.json ./site/
COPY web/package.json ./web/
RUN corepack enable \
    && corepack prepare pnpm@11.3.0 --activate \
    && pnpm install --frozen-lockfile --filter radioledger-docs...
COPY docs/ ./docs/
RUN pnpm --filter radioledger-docs docs:build

FROM nginx:1.27-alpine
# VitePress base is /docs/, so we place the files under /docs/ to match.
COPY --from=builder /src/docs/.vitepress/dist /usr/share/nginx/html/docs
EXPOSE 80
