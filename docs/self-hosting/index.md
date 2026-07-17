# Self-Hosting RadioLedger

> Run RadioLedger on your own hardware with Docker Compose.

RadioLedger is designed to be self-hosted. A single `docker compose up` gets you a fully functional instance with no external dependencies. No Redis, no managed services — just Docker and a server.

## Why Self-Host?

- **Privacy**: Your QSO data stays on your hardware
- **Control**: No account with an external RadioLedger operator required
- **Customization**: Configure retention, backup policies, and integrations your way
- **AGPLv3**: The source is yours — modify, extend, run how you like

## Quick Start

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger/docker
./init.sh
# Edit .env with your settings
docker compose up -d
```

Web UI: `http://localhost:3000`
API: `http://localhost:8080`

See [Docker Setup](docker-setup.md) for the full guide.

## In This Section

| Guide | What it covers |
|-------|---------------|
| [requirements.md](requirements.md) | Hardware and software requirements |
| [docker-setup.md](docker-setup.md) | Complete Docker Compose installation guide |
| [configuration.md](configuration.md) | Environment variables and configuration options |
| [updating.md](updating.md) | Upgrading to new versions |
| [backup-restore.md](backup-restore.md) | Backup and restore procedures |
| [reverse-proxy.md](reverse-proxy.md) | HTTPS with Nginx, Caddy, or Traefik |
| [security.md](security.md) | Security hardening for self-hosters |

## What's in the Stack

| Service | Purpose |
|---------|---------|
| `radioledger/api` | Go API server |
| `radioledger/web` | Web UI (static files served by the API) |
| `postgis/postgis:17-3.5` | PostgreSQL with PostGIS extension |
| `zitadel/zitadel` | Authentication (OIDC/OAuth2) |

No Redis, no RabbitMQ, no external cache — River (the job queue) runs on PostgreSQL.

## Data Locations

| Data | Location |
|------|----------|
| PostgreSQL data | Docker volume `pgdata` |
| Uploaded ADIF files | Docker volume `uploads` (or S3-compatible) |
| Config | `.env` file and Docker Compose file |

**Back up the `pgdata` volume and your `RADIOLEDGER_MASTER_KEY` separately.** If you lose the master key, encrypted credentials (QRZ API key, eQSL password, etc.) cannot be recovered.

## Getting Help

- GitHub Issues: [github.com/FtlC-ian/radioledger/issues](https://github.com/FtlC-ian/radioledger/issues)
- Check the [Troubleshooting section in docker-setup.md](docker-setup.md#troubleshooting)

## Related

- [Getting Started: Installation](../getting-started/installation.md)
- [Security Hardening](security.md)
- [Configuration Reference](configuration.md)
