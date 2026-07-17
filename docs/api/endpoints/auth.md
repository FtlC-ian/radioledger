# Authentication Endpoints

> RadioLedger authentication and authorization API.
> These endpoints handle user login, session management, and short-lived tokens.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/auth/login` | User login (identity provider proxy) |
| `POST` | `/v1/auth/logout` | End current session |
| `POST` | `/v1/stream-token` | Create a short-lived, single-use stream token |

---

## Stream Tokens

`POST /v1/stream-token`

Used to establish secure EventSource (SSE) connections without embedding long-lived Bearer tokens in URLs.

### Response

```json
{
  "success": true,
  "data": {
    "token": "st_example_one_time_token",
    "expires_at": "2026-03-01T12:05:00Z"
  }
}
```

### Usage

1. Call `POST /v1/stream-token` (authenticated via Bearer header).
2. Use the returned `token` as a query parameter for SSE:
   `new EventSource("/v1/logbooks/{uuid}/stream?token=st_example_one_time_token")`
3. The server validates the stream token and initiates the connection.
4. Tokens expire in 60 seconds or upon first use.

---

## Related

- [API Authentication](../authentication.md)
- [API Errors](../errors.md)
