# Authentication

> How to authenticate with the RadioLedger API using API keys or OAuth2 tokens.

## Authentication Methods

| Method | Use case |
|--------|----------|
| **API Key** | Scripts, integrations, personal automation |
| **OAuth2 Access Token** | User-authorized apps, web integrations |

Both methods use the `Authorization: Bearer <token>` header.

## API Keys

API keys are the simplest way to authenticate for personal use.

### Generating an API Key

1. Log in to RadioLedger
2. Go to **Settings → API Keys**
3. Click **+ New API Key**
4. Give it a name (e.g., "My WSJT-X script")
5. Select scopes (see below)
6. Copy the key — it's only shown once

### Using an API Key

```bash
curl -H "Authorization: Bearer rl_live_xxxxxxxxxxxxxxxxxxxx" \
  https://your-radioledger.example/v1/logbooks
```

### API Key Format

API keys are prefixed with `rl_live_` for production and `rl_test_` for test environments.

API keys are secrets. Store them like passwords, rotate them if exposed, and prefer short-lived scoped keys for scripts.

### Key Scopes

When creating an API key, select the minimum scopes needed:

| Scope | Permission |
|-------|-----------|
| `qsos:read` | Read QSOs from your logbooks |
| `qsos:write` | Create and edit QSOs |
| `qsos:delete` | Delete QSOs |
| `logbooks:read` | Read logbook metadata |
| `logbooks:write` | Create and configure logbooks |
| `import:write` | Upload ADIF files |
| `export:read` | Export logbook data |
| `sync:read` | Read sync service status |
| `sync:write` | Trigger sync jobs |
| `awards:read` | Read award progress |
| `account:read` | Read account information |
| `webhooks:write` | Manage webhooks |

### Revoking a Key

In Settings → API Keys, click **Revoke** next to any key. Revocation is immediate.

### Key Expiry

API keys can be set to expire. Expired keys return `HTTP 401`. Set a short expiry for one-off scripts.

## OAuth2 Access Tokens

For applications that act on behalf of users, use OAuth2 with PKCE.

### OAuth2 Flow (Authorization Code + PKCE)

1. Redirect the user to the RadioLedger authorization URL
2. User logs in and grants consent
3. RadioLedger redirects back with an authorization code
4. Exchange the code for access + refresh tokens
5. Use the access token in API requests

```
Authorization URL: https://auth.your-radioledger.example/oauth/v2/authorize
Token URL:         https://auth.your-radioledger.example/oauth/v2/token
```

TODO: Complete OAuth2 client registration and flow documentation.

### Token Lifetimes

| Token type | Lifetime |
|-----------|---------|
| Access token | 15 minutes |
| Refresh token (web) | 7 days |
| Refresh token (desktop) | 90 days |
| Refresh token (mobile) | 30 days |

Refresh tokens rotate on each use.

Development-only auth modes and test tokens are not for internet-facing deployments. Production deployments should use Zitadel/OIDC or a supported self-hosted auth configuration.

## Unauthenticated Endpoints

These endpoints do not require authentication:

| Endpoint | Purpose |
|---------|---------|
| `GET /health` | Liveness check |
| `GET /ready` | Readiness check |

All other endpoints return `HTTP 401` without a valid token.

## Error Responses

| HTTP Status | Meaning |
|------------|---------|
| `401 Unauthorized` | Missing or invalid token |
| `403 Forbidden` | Valid token but insufficient scope |

See [Error Reference](errors.md) for details.

## Related

- [API Overview](index.md)
- [Rate Limits](rate-limits.md)
- [User Endpoints](endpoints/users.md)
- [Security Posture](../security.md)
