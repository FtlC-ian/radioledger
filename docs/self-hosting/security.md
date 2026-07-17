# Security Hardening

> Security best practices for self-hosted RadioLedger deployments.

For the broader product security posture, including implemented controls and known caveats, see [Security Posture](../security.md).

## Default Security Posture

RadioLedger's Docker Compose configuration is security-first by default:

- `read_only: true` filesystem on API container
- `no-new-privileges: true` — containers cannot gain additional privileges
- `cap_drop: [ALL]` — no Linux capabilities
- Database port NOT exposed to host network
- HTTPS not configured by default (your responsibility to add)

## High-Priority Actions

### 1. Use HTTPS

Never run RadioLedger over plain HTTP on a public-facing server.

See [Reverse Proxy Setup](reverse-proxy.md) for Caddy, Nginx, and Traefik examples.

### 2. Generate and Back Up Your Master Key

The `RADIOLEDGER_MASTER_KEY` encrypts service credentials (QRZ API keys, eQSL passwords). If lost, these credentials are permanently unrecoverable.

For self-hosted deployments, `init.sh` should generate a random key and write it to your `.env` on first setup. Verify that the key is present before relying on the instance, and do not depend on an undocumented container-generated fallback.

```bash
# Check the generated .env file
grep RADIOLEDGER_MASTER_KEY .env
# Store this value in a password manager, separately from your database backup
```

### 3. Key Rotation

If your master key is compromised, use the `rotate-credentials-key` CLI tool to re-encrypt all credentials with a new key.

```bash
# From within the API container
./api/cmd/rotate-credentials-key/rotate-credentials-key --old-key <old_key> --new-key <new_key>
```

Update your `.env` with the new key and restart the stack.

### 4. Strong Database Password

Your `DB_PASSWORD` should be a long random string:

```bash
openssl rand -base64 32
```

Do not deploy with example values such as `change_me`, `password`, or `radioledger`.

### 5. Firewall Configuration

Expose only what's needed:

```bash
# Allow only HTTPS (and HTTP for redirect)
ufw allow 443/tcp
ufw allow 80/tcp

# If SSH is the only other access
ufw allow 22/tcp

# Enable
ufw enable
```

The PostgreSQL port (5432) should NEVER be open to the internet — it's already inside the Docker network, but verify your host firewall blocks it too.

### 6. Restrict Registration

If your instance is personal, family-only, club-only, or otherwise private, restrict Zitadel registration to invited users. Open registration means anyone who reaches the login page may be able to create an account.

### 7. Keep the Stack Updated

```bash
docker compose pull && docker compose up -d
```

Subscribe to RadioLedger security advisories: [github.com/FtlC-ian/radioledger/security](https://github.com/FtlC-ian/radioledger/security)

## Additional Hardening

### Limit API Rate Limits

For a personal installation with just your household:

```dotenv
RADIOLEDGER_RATE_LIMIT_IP=30/min
RADIOLEDGER_RATE_LIMIT_USER=300/min
```

Lower limits reduce the blast radius if credentials are compromised.

### Restrict Admin Access

Set the `RADIOLEDGER_ADMIN_EMAILS` environment variable to a comma-separated list of authorized administrator emails.

```dotenv
RADIOLEDGER_ADMIN_EMAILS=ian@example.com
```

Use dedicated administrator accounts where practical. Remove departed operators promptly.

### Trusted Proxies

Only trust `X-Forwarded-For` and `X-Real-IP` headers when RadioLedger is behind a proxy you control. If the API is exposed directly, clients can spoof those headers.

If your deployment has a trusted-proxy setting, configure it with the IP ranges of your reverse proxy or load balancer. Do not use forwarded IP headers for rate limiting, IP allowlists, or incident response unless this is configured.

### Docker Socket Security

If running Watchtower or other Docker management tools, restrict socket access. Avoid mounting the Docker socket in containers unless absolutely necessary.

### Log Retention

Logs may contain IP addresses. Configure rotation:

```yaml
# docker-compose.yml
services:
  api:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "5"
```

Per GDPR/privacy considerations: source IP addresses in logs should be purged after 90 days. Sensitive query parameters (e.g., tokens) are automatically scrubbed from logs.

### PostgreSQL Access

The database is only accessible inside the Docker network. If you need to access it for maintenance:

```bash
# Connect via Docker (no exposed port needed)
docker compose exec db psql -U radioledger radioledger
```

Never expose the PostgreSQL port externally.

## Reporting Security Issues

To report a security vulnerability, use the repository's private security-reporting channel when one is available, or contact the project maintainers privately. Do not post vulnerability details in public issues.

## Related

- [Docker Setup](docker-setup.md)
- [Configuration Reference](configuration.md)
- [Reverse Proxy Setup](reverse-proxy.md)
- [Backup and Restore](backup-restore.md)
