# PSK Reporter API Research

## API Status
- **Status**: Active / Public (Free for development)
- **Official Documentation**: [https://pskreporter.info/pskdev.html](https://pskreporter.info/pskdev.html)
- **Primary API Host**: `https://pskreporter.info/`

## Authentication Method
- **Method**: User Agent (Application Name) + Callsign.
- **Security**: No strict API key required for basic submission, but your application name must be unique and identifiable.

## Available Operations
- **Upload Signal Report**: Send data about a station heard (typically for digital modes like FT8, PSK31).
- **Fetch Spots/Reception Reports**: Query what has been heard by a specific callsign or in a geographic area.
- **URL Parameters**: [https://pskreporter.info/pskdev.html#URLParams](https://pskreporter.info/pskdev.html#URLParams)

## Rate Limits and Usage Guidelines
- **Guidelines**: "Do not send more than one record every 30 minutes for the same callsign/mode/frequency combination."
- **Polling**: "Do not poll more than once every 5 minutes."

## Working Open-Source Implementations
1. **go-pskreporter** (Go): [https://github.com/jasonhancock/go-pskreporter](https://github.com/jasonhancock/go-pskreporter)
2. **WSJT-X**: The gold standard (C++). Implements PSK Reporter via UDP/TCP.
3. **psk-reporter-node** (Node.js): [https://github.com/vsergeev/psk-reporter-node](https://github.com/vsergeev/psk-reporter-node)

## Code Snippet (Query Example)
```bash
# Get all FT8 signals heard by K1ABC in the last 15 minutes
curl "https://pskreporter.info/query?receiverCallsign=K1ABC&mode=FT8&flowStartSeconds=-900"
```

## Known Gotchas
- **ADIF Fields**: Data sent must include `CALL`, `QSO_DATE`, `TIME_ON`, `BAND`, `MODE`.
- **Automatic vs Manual**: For propagation mapping, the `AUTOMATIC` field should be set to `1`. If set to `2`, it indicates a confirmed QSO (rarely used for propagation mapping).
- **Holdback Mechanism**: PSK Reporter has an internal "holdback" to prevent duplicate signals from flooding the database.
- **TCP Protocol**: For high-volume reporting (like from a gateway), there is a custom TCP binary protocol documented in the "Developer Information" PDF.
