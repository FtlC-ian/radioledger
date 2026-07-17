# Webhooks

> Planned real-time notifications for events that happen in RadioLedger.

Webhooks are planned but not part of the stable API yet. This page documents the intended security model and draft event shape for integrations that react to RadioLedger events, such as a new QSO, a LoTW confirmation, or an import completing.

## Supported Events

| Event | Description |
|-------|-------------|
| `qso.created` | A new QSO was logged |
| `qso.updated` | A QSO was edited |
| `qso.deleted` | A QSO was deleted |
| `qso.confirmed` | A QSO was confirmed (any service) |
| `import.completed` | An ADIF import job finished |
| `import.failed` | An ADIF import job failed |
| `sync.error` | A sync job encountered an error |
| `lotw.cert_expiring` | A LoTW certificate is expiring soon |

TODO: Implement webhook routes and finalize event catalog.

## Planned Setup Flow

1. Go to **Settings → Webhooks**
2. Click **+ New Webhook**
3. Enter your endpoint URL (must be HTTPS)
4. Select events to subscribe to
5. Copy the signing secret

Planned API shape:

```bash
curl -X POST \
  -H "Authorization: Bearer <key>" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://yourapp.example.com/webhooks/radioledger",
    "events": ["qso.created", "qso.confirmed"],
    "secret": "your-webhook-secret"
  }' \
  https://your-radioledger.example/v1/webhooks
```

## Webhook Payload

```json
{
  "id": "evt_01HN...",
  "event": "qso.created",
  "created_at": "2026-02-28T14:32:00Z",
  "data": {
    "qso": {
      "uuid": "550e8400-e29b-41d4-a716-446655440000",
      "callsign": "W1AW",
      "band": "20m",
      "mode": "FT8",
      "datetime_on": "2026-02-28T14:32:00Z",
      ...
    }
  }
}
```

## Verifying Webhook Signatures

When webhook delivery ships, each delivery should include an `X-RadioLedger-Signature` header. Verify it to ensure the request came from RadioLedger.

Security expectations:

- Webhook target URLs must use HTTPS.
- Verify the signature over the exact raw request body before parsing JSON.
- Compare signatures with a constant-time comparison function.
- Reject unexpected event types and old timestamps when timestamp headers are available.
- Store the signing secret like a password.

```python
import hmac
import hashlib

def verify_signature(payload: bytes, signature: str, secret: str) -> bool:
    expected = hmac.new(
        secret.encode(),
        payload,
        hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(f"sha256={expected}", signature)
```

Always verify the signature before processing webhook payloads.

The webhook feature and event catalog are still being finalized. Signature verification and HTTPS are planned requirements; individual event names may change before a stable release.

## Planned Retry Policy

RadioLedger should retry failed webhook deliveries:

| Attempt | Delay |
|---------|-------|
| 1 (immediate) | 0 |
| 2 | 1 minute |
| 3 | 5 minutes |
| 4 | 30 minutes |
| 5 | 2 hours |

After 5 failures, the webhook should be disabled and an alert should be sent to your account email.

## Webhook Logs

When implemented, recent webhook deliveries and responses should appear in **Settings → Webhooks → Delivery Log**.

## Related

- [API Overview](index.md)
- [Authentication](authentication.md)
- [Security Posture](../security.md)
