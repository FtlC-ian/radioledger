# QRZ Logbook API Research

## Official Documentation
- **Endpoint**: `https://logbook.qrz.com/api`
- **Docs**: [QRZ Logbook API Developer Guide](https://www.qrz.com/docs/logbook/QRZLogbookAPI.html)

## Authentication
Authentication is session-less. Every request must include:
- `KEY`: A QRZ-supplied logbook access key (opaque string).
- `ACTION`: The specific API command.

Note: Most operations (INSERT, DELETE, STATUS, FETCH) require a QRZ XML Logbook Data subscription.

## API Interface
- **Method**: HTTP POST
- **Content-Type**: `application/x-www-form-urlencoded`
- **Format**: Name-value pairs. QSO data is passed as ADIF in the `ADIF` parameter.
- **User-Agent Requirement**: Must provide a descriptive User-Agent (e.g., `RadioLedger/1.0.0 (CALLSIGN)`).

## Operations

### 1. Upload QSO (INSERT)
- **Action**: `INSERT`
- **Parameters**:
  - `KEY`: API Key
  - `ADIF`: ADIF record (e.g., `<band:3>20m<mode:2>CW<call:5>W1AW...<eor>`)
  - `OPTION`: `REPLACE` (optional, to overwrite duplicates)
- **Response**: `RESULT=OK&LOGID=12345678&COUNT=1`

### 2. Fetch Confirmations (FETCH)
- **Action**: `FETCH`
- **Options** (comma-separated):
  - `STATUS:CONFIRMED`: Fetch only confirmed records.
  - `MODSINCE:YYYY-MM-DD`: Fetch records modified since date.
  - `MAX:250`: Recommended for paging.
  - `AFTERLOGID:nnn`: For paging through results.
- **Response**: Returns `RESULT=OK`, `COUNT`, and `ADIF` containing the records.

## Rate Limits & Guidelines
- QRZ does not specify a hard numerical rate limit in the docs but warns that generic User-Agents (like `python-requests`) may be throttled.
- **Paging**: Paging is recommended for large logbooks using `MAX` and `AFTERLOGID`.
- **Batching**: While `INSERT` is for single records, large uploads should use the separate (but similar) Logbook Upload interface if available, though `INSERT` is standard for real-time.

## Working Implementations
1. **[k0swe/qrz-logbook](https://github.com/k0swe/qrz-logbook)** (Go)
   - Recent activity (2024).
   - Provides Go bindings and an OpenAPI spec.
2. **[n5bur/qrz-logbook-api](https://github.com/n5bur/qrz-logbook-api)** (Rust/Reference)
   - Good for logic reference despite being Rust.
3. **[QSPyLib](https://github.com/dbm/qspylib)** (Python)
   - Comprehensive wrapper for multiple ham services.

## Code Snippet (Go)
```go
// Example using k0swe/qrz-logbook logic
payload := url.Values{}
payload.Set("KEY", apiKey)
payload.Set("ACTION", "INSERT")
payload.Set("ADIF", adifData)

client := &http.Client{}
req, _ := http.NewRequest("POST", "https://logbook.qrz.com/api", strings.NewReader(payload.Encode()))
req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
req.Header.Set("User-Agent", "RadioLedger/1.0.0 (YOURCALL)")

resp, err := client.Do(req)
// Parse RESULT=OK&LOGID=... from body
```

## Gotchas
- **XML Key vs Logbook Key**: Ensure the user provides the **Logbook API Key** (found in logbook settings), not the general QRZ XML Subscriber Key.
- **Plain Text**: The API is HTTPS, but data is sent as simple form fields.
- **Confirmed Records**: confirmed records cannot be modified via `REPLACE` unless certain conditions are met; usually better to `INSERT` and let QRZ match.
