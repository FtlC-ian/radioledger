# User Endpoints

> Manage account information and settings via API.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/users/me` | Get current user profile |
| `PUT` | `/v1/users/me` | Update profile |
| `GET` | `/v1/users/me/api-keys` | List API keys |
| `POST` | `/v1/users/me/api-keys` | Create an API key |
| `DELETE` | `/v1/users/me/api-keys/{key_id}` | Revoke an API key |

## Get Current User

`GET /v1/users/me`

```json
{
  "success": true,
  "data": {
    "uuid": "...",
    "email": "user@example.com",
    "callsign": "W1AW",
    "gridsquare": "FN42",
    "timezone": "America/New_York",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

## Update Profile

`PUT /v1/users/me`

Update callsign, grid square, timezone, and display preferences.

## API Key Management

### List API Keys

`GET /v1/users/me/api-keys`

Returns all active API keys (key values are not returned — only metadata).

### Create API Key

`POST /v1/users/me/api-keys`

```json
{
  "name": "My script",
  "scopes": ["qsos:read", "qsos:write"],
  "expires_at": "2027-01-01T00:00:00Z"
}
```

**Response includes the full key value — save it, it won't be shown again.**

### Revoke API Key

`DELETE /v1/users/me/api-keys/{key_id}`

Immediate revocation.

## Related

- [Authentication](../authentication.md)
- [Settings (User Guide)](../../user-guide/settings.md)
