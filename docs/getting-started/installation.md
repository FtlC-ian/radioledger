# Installation (Self-Hosting)

> Run RadioLedger on your own hardware using Docker Compose.

This guide covers self-hosted installation.

## Prerequisites

- Docker Engine 24+ and Docker Compose v2
- A Linux, macOS, or Windows machine with at least 1 GB RAM and 5 GB disk
- A domain name (optional but recommended for HTTPS)

## Quick Start

```bash
# 1. Clone the public source
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger/docker

# 2. Generate secrets and start
./init.sh
docker compose up -d

# Web UI:  http://localhost:3000
# API:     http://localhost:8080
```

RadioLedger automatically generates a `RADIOLEDGER_MASTER_KEY` on first run if you don't provide one. **Back this key up separately from your database — without it, encrypted credentials cannot be recovered.**

## Step-by-Step Setup

### Step 1: Download the Compose file

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger/docker
./init.sh
```

### Step 2: Configure environment variables

Open `.env` and set:

```dotenv
# Required
DB_PASSWORD=<strong-random-password>
RADIOLEDGER_MASTER_KEY=<generated-by-first-run-or-set-manually>

# Optional: your Zitadel auth instance (or use the bundled one)
ZITADEL_URL=https://auth.yourdomain.com
```

TODO: Document every supported environment variable. See [configuration.md](../self-hosting/configuration.md) for the full reference.

### Step 3: Start the stack

```bash
docker compose up -d
```

### Step 4: Create your account

Open `http://localhost:3000` in your browser and create your account.

### Step 5: (Optional) Enable HTTPS

Use the included Caddy reverse proxy:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d
```

TODO: Full TLS/reverse-proxy setup — see [reverse-proxy.md](../self-hosting/reverse-proxy.md).

## What's Running

| Service | Port | Purpose |
|---------|------|---------|
| `api` | 8080 | REST API server |
| `web` | 3000 | Web UI |
| `db` | (internal only) | PostgreSQL + PostGIS |
| `zitadel` | (internal only) | Authentication |

The database port is **not** exposed to the host by design.

## Upgrading

See [Updating RadioLedger](../self-hosting/updating.md).

## Backup and Restore

See [Backup and Restore](../self-hosting/backup-restore.md). Back up your `pgdata` volume and `RADIOLEDGER_MASTER_KEY`.

## Troubleshooting

TODO: Common Docker startup issues — see [self-hosting troubleshooting](../self-hosting/index.md).

**Container won't start?** Run `docker compose logs api` for details.

**Migration failed?** Run `docker compose run api radioledger migrate status`.

## Related

- [Configuration Reference](../self-hosting/configuration.md)
- [Reverse Proxy Setup](../self-hosting/reverse-proxy.md)
- [Security Hardening](../self-hosting/security.md)
- [Backup and Restore](../self-hosting/backup-restore.md)
- [Log Your First QSO](first-qso.md)
