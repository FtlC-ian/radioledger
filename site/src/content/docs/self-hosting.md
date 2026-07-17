---
title: Self-Hosting
description: Deploy RadioLedger with Docker Compose and keep full control of your station data.
sidebar:
  order: 2
---

This guide is adapted from the project Docker setup documentation and tuned for quick, repeatable installs.

## Prerequisites

- Docker Engine 24+
- Docker Compose v2
- A server, mini PC, or NAS you control

## Install with Docker Compose

### 1) Create a deploy directory

```bash
mkdir -p ~/radioledger && cd ~/radioledger
```

### 2) Fetch compose + env template

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger/docker
./init.sh
```

### 3) Configure secrets

```dotenv
DB_PASSWORD=change-me-to-a-strong-random-value
RADIOLEDGER_MASTER_KEY=openssl-rand-base64-32-output
BASE_URL=http://localhost:3000
```

> Back up `RADIOLEDGER_MASTER_KEY`. If you lose it, encrypted credentials cannot be recovered.

### 4) Start services

```bash
docker compose up -d
```

### 5) Verify service health

```bash
docker compose ps
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

## What starts

- `api` — Go REST API
- `web` — Web UI
- `db` — PostgreSQL + PostGIS
- `zitadel` — OIDC / OAuth authentication

## Common operations

### View logs

```bash
docker compose logs -f api
```

### Update to latest images

```bash
docker compose pull
docker compose up -d
```

### Full reset (destructive)

```bash
docker compose down -v
docker compose up -d
```

## Reverse proxy and HTTPS

For internet-facing deployments, put the stack behind Caddy, Traefik, or Nginx and terminate TLS there.

## Backups

At minimum, back up:

- PostgreSQL data volume
- `.env` file
- `RADIOLEDGER_MASTER_KEY`

## Troubleshooting

- **API can't connect to DB:** wait for database startup, then re-check logs.
- **Auth issues:** verify auth service is healthy and callback URLs match `BASE_URL`.
- **Migration errors:** inspect API logs and rerun once root cause is fixed.
