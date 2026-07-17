# Security Posture

RadioLedger is designed for multi-tenant logbook storage, self-hosted deployments, local desktop integrations, and external ham-radio service sync. This page summarizes the public security posture as of the current pre-1.0 development line.

It is intentionally specific about what exists, what is planned, and what still needs testing. Historical audit notes remain in the repository for traceability, but this page is the public summary.

## Current Controls

### Authentication

RadioLedger supports bearer-token authentication for the API.

- Deployments can use OAuth2/OIDC with Zitadel.
- Web, desktop, and mobile clients use authorization-code flows with PKCE.
- Access tokens are short-lived. Refresh-token lifetime depends on client type, and refresh tokens are expected to rotate on use.
- API keys are shown once, stored as hashes, can expire, and can be scoped to the minimum permissions an integration needs.
- Development authentication modes are for local development and tests only. Production deployments should refuse unsafe development auth modes.

### Authorization and Tenant Isolation

RadioLedger uses PostgreSQL Row-Level Security for tenant isolation. Tenant-scoped tables are protected by database policies, and authenticated requests set tenant context inside the same transaction that performs application queries.

Public API responses use external UUIDs rather than internal database IDs. Internal BIGSERIAL IDs are for joins and database relationships only.

### Credential Storage

External service credentials, such as QRZ API keys, eQSL passwords, and ClubLog API keys, are stored encrypted rather than plaintext.

The documented default design is AES-256-GCM with per-user keys derived from a master key by HKDF. The master key lives outside the database. Self-hosters are responsible for generating, storing, backing up, and rotating that key.

If the master key is lost, encrypted external-service credentials are not recoverable. Database backups are not enough by themselves.

### LoTW Signing and Local Secrets

RadioLedger documents desktop-local LoTW signing: the desktop client signs LoTW
uploads locally through tQSL and reports upload status back to the operator's
own deployment. tQSL certificate contents and private keys remain on the
operator's machine.

Desktop authentication tokens are intended to be stored in the operating-system keychain. Configuration files should not contain OAuth tokens, API keys, service passwords, or LoTW certificate private keys.

### API and Web Security

RadioLedger's API design includes:

- Strict CORS allowlists rather than wildcard origins.
- Security headers for browser-facing responses.
- Request body size limits for upload and JSON endpoints.
- Per-IP and per-user rate limiting.
- Structured security logging for authentication, API key, and administrative events.
- Parameterized SQL through generated query code.

### Webhooks

Webhook delivery is planned, but the public event catalog and delivery implementation are not stable yet. When webhooks ship, endpoint URLs should require HTTPS, deliveries should include an HMAC-SHA256 signature such as `X-RadioLedger-Signature`, and receivers should verify the signature before processing payloads.

### Self-Hosting

The Docker Compose deployment is intended to be secure by default:

- The database is not exposed to the host network by default.
- The API container runs with a read-only filesystem where practical.
- Linux capabilities are dropped.
- `no-new-privileges` is enabled.

Self-hosters remain responsible for:

- Running HTTPS in front of public deployments.
- Backing up the database and `RADIOLEDGER_MASTER_KEY`.
- Using strong database, Zitadel, SMTP, and admin credentials.
- Restricting account registration if the instance is private.
- Keeping images and host packages updated.
- Configuring firewall rules and log retention.
- Configuring trusted proxy behavior before relying on forwarded client IP headers.

See [Self-Hosting Security](self-hosting/security.md) for deployment hardening steps.

## Known Caveats and Planned Hardening

RadioLedger is pre-1.0, and the following areas should be treated as active hardening work:

- Account-level throttling or lockout after repeated failed logins.
- Trusted proxy configuration for `X-Forwarded-For` and related headers.
- Clear release verification for desktop local database encryption with SQLCipher.
- High-availability storage for short-lived stream tokens if running multiple API replicas.
- Webhook delivery implementation, signature verification, timestamp/replay protection, and automated tests.
- More public detail around Zitadel self-hosting configuration and invitation-only registration.
- Continued review of job payloads so inline credentials are not persisted in background job metadata.

These caveats do not mean the system is unsafe for development or private testing. They are the items that should remain visible before a broad public launch.

## Historical Audits

The repository contains historical security audits and architecture reviews. They are useful for traceability, but some findings have been fixed, superseded, or moved into implementation issues.

For public evaluation, use this page first, then consult the historical audit files if you need the original finding history.

## Reporting Security Issues

Do not post vulnerabilities in public issues. Use the repository's private
security-reporting channel when one is available, or contact the project
maintainers privately.
