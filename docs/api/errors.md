# Error Reference

> Error response format and error codes for the RadioLedger API.

## Response Format

All error responses use the standard envelope with `success: false`:

```json
{
  "success": false,
  "data": null,
  "message": "Human-readable description of the error",
  "error": {
    "code": "ERROR_CODE",
    "details": "Additional context if available",
    "fields": {
      "fieldname": "field-specific error message"
    }
  }
}
```

The `fields` object is only present for validation errors.

## HTTP Status Codes

| Status | When used |
|--------|----------|
| `200 OK` | Success AND resource-not-found (with `success: false`) |
| `202 Accepted` | Async operations (import jobs) |
| `400 Bad Request` | Malformed request (bad JSON, missing required fields) |
| `401 Unauthorized` | Missing or expired authentication |
| `403 Forbidden` | Authenticated but insufficient permissions |
| `404 Not Found` | **Route not found only** — missing resources return 200 |
| `413 Payload Too Large` | File upload exceeds size limit |
| `429 Too Many Requests` | Rate limit exceeded |
| `500 Internal Server Error` | Server error (no internal details exposed) |

**Why 200 for missing resources?** Resource-not-found is a business logic outcome, not an HTTP protocol error. This simplifies client error handling — you only need to check `success: false`, not both HTTP status and response body.

## Error Codes

| Code | Description |
|------|-------------|
| `VALIDATION_ERROR` | Input validation failed (see `fields`) |
| `NOT_FOUND` | Requested resource does not exist |
| `DUPLICATE` | Resource already exists (e.g., duplicate QSO on strict dedup) |
| `UNAUTHORIZED` | Authentication required or token invalid |
| `FORBIDDEN` | Insufficient permissions for this resource |
| `RATE_LIMIT_EXCEEDED` | Too many requests |
| `IMPORT_ERROR` | ADIF import failed (see import job for details) |
| `INVALID_ADIF` | Uploaded file is not valid ADIF |
| `SERVICE_UNAVAILABLE` | External service (LoTW, QRZ, etc.) temporarily unavailable |
| `INTERNAL_ERROR` | Server error — report this with the request ID |

## Validation Errors

Validation errors include a `fields` object:

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "QSO validation failed",
    "fields": {
      "callsign": "callsign is required",
      "datetime_on": "must be a valid ISO 8601 timestamp",
      "band": "unsupported band: 1234m"
    }
  }
}
```

## Request IDs

Every API response includes a request ID header:

```
X-Request-ID: 7f3a9b2c-1234-5678-abcd-ef0123456789
```

Include this ID when reporting issues.

## Related

- [API Overview](index.md)
- [Rate Limits](rate-limits.md)
