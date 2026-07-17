# Updating RadioLedger

> Upgrade your self-hosted RadioLedger instance to a new version.

## Before You Update

1. **Read the changelog**: Check [Changelog](../changelog/index.md) for breaking changes in the new version
2. **Back up your data**: Always back up before upgrading — see [Backup and Restore](backup-restore.md)
3. **Check requirements**: Verify new version requirements match your hardware

```bash
# Create a backup before upgrading
docker compose exec db pg_dump -U radioledger radioledger > backup_$(date +%Y%m%d).sql
```

## Updating

### Standard Update (Patch/Minor Version)

```bash
cd ~/radioledger

# Pull new images
docker compose pull

# Restart with new images (migrations run automatically)
docker compose up -d
```

Migrations run on container startup. The API server starts only after migrations complete successfully.

### Verifying the Update

```bash
# Check all services are healthy
docker compose ps

# Verify migration status
docker compose run --rm api radioledger migrate status

# Check the API version
curl http://localhost:8080/health
```

### Checking Which Version Is Running

```bash
docker compose images
# or
curl http://localhost:8080/health | jq .version
```

## Pin to a Specific Version

For stability, pin to a specific version instead of `latest`:

```yaml
# docker-compose.yml
services:
  api:
    image: radioledger/api:1.2.3  # specific version
```

```bash
# Update to a specific version
docker compose pull
docker compose up -d
```

## Major Version Upgrades

Major versions may have breaking changes. The changelog will note:
- Required migration steps
- Configuration changes
- Deprecated features being removed

For major upgrades, follow the migration guide in the changelog.

## Rolling Back

If an update causes issues:

```bash
# Roll back to previous version (update the image tag in docker-compose.yml first)
docker compose pull
docker compose up -d
```

**Note:** Rollback may require a database restore if the new version ran migrations that changed the schema. Always back up before upgrading.

## Automated Updates (Optional)

TODO: Document Watchtower or similar for automated container updates. Note the risks of unattended updates and recommendation to only use for patch versions.

## Related

- [Backup and Restore](backup-restore.md)
- [Docker Setup](docker-setup.md)
- [Changelog](../changelog/index.md)
