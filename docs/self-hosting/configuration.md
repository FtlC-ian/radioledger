# Configuration Reference

> All environment variables and configuration options for self-hosted RadioLedger.

Configuration is managed via environment variables in your `.env` file (loaded by Docker Compose).

## Required Variables

| Variable | Description | Example |
|---------|-------------|---------|
| `DB_PASSWORD` | PostgreSQL database password | `strong_random_password` |
| `BASE_URL` | Public URL of the RadioLedger instance | `https://radio.yourdomain.com` |

## Important: Master Key

| Variable | Description |
|---------|-------------|
| `RADIOLEDGER_MASTER_KEY` | AES-256 master key for credential encryption |

If not set, a random key is generated on first run and stored in the database. **This is convenient but dangerous** — if the database is lost, your encrypted credentials are unrecoverable.

**Best practice:** Generate a key and set it explicitly:
```bash
openssl rand -base64 32
# Set this value in your .env as RADIOLEDGER_MASTER_KEY=<output>
```

**Back up this key separately from your database.** Store it in a password manager or secure note.

## Database

| Variable | Default | Description |
|---------|---------|-------------|
| `DB_HOST` | `db` | PostgreSQL host (Docker service name) |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_NAME` | `radioledger` | Database name |
| `DB_USER` | `radioledger` | Database user |
| `DB_PASSWORD` | (required) | Database password |
| `DB_SSLMODE` | `require` | SSL mode (`require`, `disable`, `verify-full`) |

Full connection URL (alternative to individual variables):
```
RADIOLEDGER_DB_URL=postgres://radioledger:password@db:5432/radioledger?sslmode=require
```

## Authentication (Zitadel)

| Variable | Default | Description |
|---------|---------|-------------|
| `ZITADEL_URL` | `http://zitadel:8080` | Zitadel service URL |
| `ZITADEL_DOMAIN` | derived from `BASE_URL` | Zitadel external domain |

TODO: Document Zitadel configuration in detail (API key, organization setup).

## API Server

| Variable | Default | Description |
|---------|---------|-------------|
| `RADIOLEDGER_API_PORT` | `8080` | API server listen port |
| `RADIOLEDGER_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `RADIOLEDGER_CORS_ORIGINS` | derived from `BASE_URL` | Allowed CORS origins |
| `RADIOLEDGER_DATA_DIR` | `/data` | Base directory for local data storage |
| `RADIOLEDGER_ADMIN_EMAILS` | (none) | Comma-separated list of admin email addresses |

## Storage

| Variable | Default | Description |
|---------|---------|-------------|
| `RADIOLEDGER_STORAGE` | `local` | Storage backend: `local` or `s3` |
| `RADIOLEDGER_UPLOAD_DIR` | `/data/uploads` | Local upload directory (when storage=local) |
| `S3_BUCKET` | (none) | S3 bucket name (when storage=s3) |
| `S3_ENDPOINT` | (none) | S3-compatible endpoint URL |
| `S3_ACCESS_KEY` | (none) | S3 access key |
| `S3_SECRET_KEY` | (none) | S3 secret key |

## Job Queue (River)

| Variable | Default | Description |
|---------|---------|-------------|
| `RADIOLEDGER_WORKER_CONCURRENCY` | `10` | Number of parallel job workers |
| `RADIOLEDGER_QUEUE_MAX_RETRIES` | `5` | Max retry attempts for failed jobs |

## Email (Optional)

Email is used for notifications (import complete, certificate expiry, sync errors).

| Variable | Default | Description |
|---------|---------|-------------|
| `SMTP_HOST` | (none) | SMTP server hostname |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USER` | (none) | SMTP username |
| `SMTP_PASSWORD` | (none) | SMTP password |
| `SMTP_FROM` | `noreply@yourdomain.com` | From address |

If SMTP is not configured, email notifications are disabled.

## Observability

| Variable | Default | Description |
|---------|---------|-------------|
| `RADIOLEDGER_METRICS_ENABLED` | `true` | Enable Prometheus `/metrics` endpoint |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (none) | OpenTelemetry collector endpoint |

## Security

| Variable | Default | Description |
|---------|---------|-------------|
| `RADIOLEDGER_RATE_LIMIT_IP` | `100/min` | Rate limit per IP (unauthenticated) |
| `RADIOLEDGER_RATE_LIMIT_USER` | `1000/min` | Rate limit per authenticated user |
| `RADIOLEDGER_SESSION_DURATION` | `15m` | Access token TTL |

## Example .env File

```dotenv
# Required
DB_PASSWORD=change_this_to_a_strong_password
BASE_URL=https://radio.yourdomain.com
RADIOLEDGER_MASTER_KEY=<output_of_openssl_rand_base64_32>
RADIOLEDGER_ADMIN_EMAILS=ian@example.com

# Optional: email notifications
SMTP_HOST=smtp.yourdomain.com
SMTP_PORT=587
SMTP_USER=radioledger@yourdomain.com
SMTP_PASSWORD=email_password
SMTP_FROM=radioledger@yourdomain.com
```

## Related

- [Docker Setup](docker-setup.md)
- [Security Hardening](security.md)
- [Reverse Proxy Setup](reverse-proxy.md)
