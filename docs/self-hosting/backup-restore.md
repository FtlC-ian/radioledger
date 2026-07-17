# Backup and Restore

> Protect your RadioLedger data with regular backups.

## What to Back Up

Two things are critical to back up:

| What | Why |
|------|-----|
| **PostgreSQL database** (`pgdata` volume) | All your QSOs, settings, sync state |
| **`RADIOLEDGER_MASTER_KEY`** | Without this key, encrypted credentials are unrecoverable |

These two items together allow a complete restore. Without the master key, you'd need to re-enter all external service credentials (QRZ API key, eQSL password, etc.) after a restore.

## Database Backup

### pg_dump (Recommended)

```bash
# Create a SQL dump
docker compose exec db pg_dump -U radioledger radioledger > backup_$(date +%Y%m%d_%H%M%S).sql

# Compressed dump (smaller file)
docker compose exec db pg_dump -U radioledger radioledger | gzip > backup_$(date +%Y%m%d).sql.gz
```

### Automated Daily Backup Script

```bash
#!/bin/bash
# /usr/local/bin/radioledger-backup.sh
BACKUP_DIR="/backup/radioledger"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

# Database dump
docker compose -f /home/youruser/radioledger/docker-compose.yml exec -T db \
  pg_dump -U radioledger radioledger | gzip > "$BACKUP_DIR/db_$DATE.sql.gz"

# Keep last 30 days
find "$BACKUP_DIR" -name "db_*.sql.gz" -mtime +30 -delete

echo "Backup complete: db_$DATE.sql.gz"
```

```bash
# Schedule daily at 3am
crontab -e
# Add:
0 3 * * * /usr/local/bin/radioledger-backup.sh >> /var/log/radioledger-backup.log 2>&1
```

### Docker Volume Backup

For a complete volume backup (includes all PostgreSQL files):

```bash
docker compose down
docker run --rm -v radioledger_pgdata:/source -v /backup:/dest alpine \
  tar czf /dest/pgdata_$(date +%Y%m%d).tar.gz -C /source .
docker compose up -d
```

## Master Key Backup

Store your `RADIOLEDGER_MASTER_KEY` in a **separate location** from your database:

- Password manager (Bitwarden, 1Password, etc.)
- Encrypted USB drive stored off-site
- Secure note service

```bash
# Find your current master key
grep RADIOLEDGER_MASTER_KEY ~/radioledger/.env
```

## Restore

### From pg_dump

```bash
# Stop the API (prevent writes during restore)
docker compose stop api

# Drop and recreate the database
docker compose exec db psql -U radioledger -c "DROP DATABASE radioledger;"
docker compose exec db psql -U radioledger -c "CREATE DATABASE radioledger;"

# Restore
cat backup_20260228.sql | docker compose exec -T db psql -U radioledger radioledger
# Or compressed:
zcat backup_20260228.sql.gz | docker compose exec -T db psql -U radioledger radioledger

# Restart
docker compose up -d
```

### On New Hardware

1. Install Docker and Docker Compose on the new machine
2. Copy your `docker-compose.yml` and `.env` (including `RADIOLEDGER_MASTER_KEY`)
3. Start the database only: `docker compose up -d db`
4. Restore the backup: `zcat backup.sql.gz | docker compose exec -T db psql -U radioledger radioledger`
5. Start everything: `docker compose up -d`

## Verify Backup Integrity

Test your backups periodically:

```bash
# Restore to a test container and verify
docker run --rm -e POSTGRES_PASSWORD=test postgres:17 \
  sh -c "pg_restore --list backup.sql | head -20"
```

TODO: Document more comprehensive backup testing procedure.

## Related

- [Docker Setup](docker-setup.md)
- [Configuration Reference](configuration.md)
- [Updating RadioLedger](updating.md)
