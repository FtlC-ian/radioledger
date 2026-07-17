# POTA (Parks on the Air) API Research

## API Status
- **Status**: Active / Public (Some endpoints "Under Construction")
- **Official Documentation**: [https://docs.pota.app/api/index.html](https://docs.pota.app/api/index.html)
- **Primary API Host**: `https://api.pota.app/`

## Authentication Method
- **Method**: JWT Token (obtained via user login).
- **Public Endpoints**: Many read-only endpoints (spots, park info) do not require authentication.
- **Private Endpoints**: Activation log uploads require an `Authorization: Bearer <TOKEN>` header.

## Available Operations
- **Fetch Spots**: `GET https://api.pota.app/spot/` (JSON).
- **Fetch Park Info**: `GET https://api.pota.app/park/<PARK_ID>` (JSON).
- **Upload Activation Log**: `POST https://api.pota.app/log/` (Requires authentication).
- **Activation Stats**: `GET https://api.pota.app/stats/activator/<CALLSIGN>`.

## Rate Limits and Usage Guidelines
- **Guidelines**: Fair use. Avoid polling `/spot/` more than once every 30-60 seconds. Use official clients where possible.
- **Log Uploads**: While manual upload via the website is standard, many loggers are integrating the API.

## Working Open-Source Implementations
1. **POTA-Logger** (Python): [https://github.com/vsergeev/pota-logger](https://github.com/vsergeev/pota-logger)
2. **HamSpots-POTA** (Ruby): [https://github.com/m0vfc/hamspots-pota](https://github.com/m0vfc/hamspots-pota)
3. **POTA-Exporter** (Go): Simple utility to fetch data and export it.

## Code Snippet (Spot Fetch Example)
```bash
# Fetch latest spots
curl -X GET "https://api.pota.app/spot/" | jq '.'
```

## Known Gotchas
- **ADIF Requirements**: POTA is very strict about ADIF field mapping. Ensure `MY_POTA_REF` (activator) and `SIG_INFO` (hunter) are used correctly.
- **Park Numbers**: Always use the full park number (e.g., `K-1234`), not just the digits.
- **Manual Upload Preference**: For a long time, POTA discouraged automated uploads to ensure data quality. The API is becoming more open, but user review is still encouraged before final submission.
- **Spot Expiry**: Spots are only returned if they were received within the last few hours.
