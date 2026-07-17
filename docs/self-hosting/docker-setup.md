# Docker Compose Setup

> Complete guide to installing RadioLedger with Docker Compose.

## Prerequisites

- Docker Engine 24+ and Docker Compose v2 installed
- See [System Requirements](requirements.md)

## Installation

### Step 1: Create a Directory

```bash
mkdir ~/radioledger && cd ~/radioledger
```

### Step 2: Download the Compose File

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger/docker
./init.sh
```

### Step 3: Edit Your Configuration

Open `.env` and set required values:

```dotenv
# Database password — use a strong random value
DB_PASSWORD=change_me_to_something_strong

# Master encryption key — auto-generated if empty, but you should set one
# BACK THIS UP. If lost, encrypted credentials are unrecoverable.
# Generate: openssl rand -base64 32
RADIOLEDGER_MASTER_KEY=

# Base URL (for external links and OAuth redirects)
BASE_URL=http://localhost:3000
```

See [Configuration Reference](configuration.md) for all options.

### Step 4: Start the Stack

```bash
docker compose up -d
```

This starts all services. On first run, this takes 1-2 minutes as Docker pulls images and runs database migrations.

### Step 5: Verify

```bash
docker compose ps
```

All services should show `Up` (not `Exited` or `Restarting`).

```bash
# Check API health
curl http://localhost:8080/health
# Expected: {"status":"ok"}

curl http://localhost:8080/ready
# Expected: {"status":"ok"} (or 503 if migrations still running)
```

### Step 6: Create Your Account

Open `http://localhost:3000` and create your account.

## Docker Compose File

The Compose file includes these services:

```yaml
services:
  api:           # Go API server
  web:           # Web UI
  db:            # PostgreSQL + PostGIS
  zitadel:       # Authentication
```

The database port is NOT exposed to the host — it's only accessible within the Docker network.

## Running Migrations

Migrations run automatically on container startup. If you need to check migration status:

```bash
docker compose run --rm api radioledger migrate status
```

## Adding HTTPS

For external access, use a reverse proxy. An example with Caddy:

```bash
docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d
```

See [Reverse Proxy Setup](reverse-proxy.md) for Caddy, Nginx, and Traefik examples.

## Troubleshooting

### Container Not Starting

```bash
docker compose logs api
```

Common causes:
- **"cannot connect to database"**: Database container is still starting. Wait 30s and retry.
- **"migration failed"**: A migration error. Check logs for the specific error.
- **"master key required"**: Set `RADIOLEDGER_MASTER_KEY` in your `.env` file.

### Database Issues

```bash
docker compose logs db
```

### Zitadel (Auth) Issues

```bash
docker compose logs zitadel
```

### Reset Everything

```bash
# WARNING: This deletes all your data
docker compose down -v
docker compose up -d
```

## Systemd Service (Auto-Start on Boot)

To start RadioLedger automatically when the server boots:

```ini
# /etc/systemd/system/radioledger.service
[Unit]
Description=RadioLedger
After=docker.service
Requires=docker.service

[Service]
WorkingDirectory=/home/youruser/radioledger
ExecStart=/usr/bin/docker compose up
ExecStop=/usr/bin/docker compose down
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable radioledger
sudo systemctl start radioledger
```

## Related

- [Configuration Reference](configuration.md)
- [Updating RadioLedger](updating.md)
- [Backup and Restore](backup-restore.md)
- [Reverse Proxy Setup](reverse-proxy.md)
- [Security Hardening](security.md)
