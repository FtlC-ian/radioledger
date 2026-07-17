# HamQTH API Research

## API Status
- **Status**: Active / Public (no specific limits mentioned)
- **Official Documentation**: [https://www.hamqth.com/developers.php](https://www.hamqth.com/developers.php)

## Authentication Method
- **Method**: Session Key (obtained via login endpoint).
- **Parameters**: `u` (username), `p` (password).
- **Process**: Call `https://www.hamqth.com/xml.php?u=USER&p=PASS` to get an XML response containing a `<session_id>`. All subsequent calls must include `id=SESSION_ID`.

## Available Operations
- **Callsign Search**: Search by callsign (standard XML API).
- **QSL Upload**: [https://www.hamqth.com/xml.php?id=SESSION_ID&adif=ADIF_DATA](https://www.hamqth.com/xml.php?id=SESSION_ID&adif=ADIF_DATA)
- **DXCC Lookup**: API for DXCC tables and prefix management.
- **Solar/WX Info**: Propagation and solar flux data.

## Rate Limits and Usage Guidelines
- **Limits**: "Doesn't have any limits" according to their homepage.
- **Usage**: Developers are encouraged to cache session keys for as long as they are valid (usually 1 hour).

## Working Open-Source Implementations
1. **HamQTHClient** (Java): [https://github.com/kevinhooke/HamQTHClient](https://github.com/kevinhooke/HamQTHClient)
2. **go-hamqth** (Go): [https://github.com/m0vfc/go-hamqth](https://github.com/m0vfc/go-hamqth)
3. **Cloudlog**: `application/models/Hamqth_model.php`.

## Code Snippet (Auth Example)
```bash
# Step 1: Login
curl "https://www.hamqth.com/xml.php?u=MYCALL&p=MYPASSWORD"
# Response contains <session_id>1234567890</session_id>

# Step 2: Upload ADIF
curl "https://www.hamqth.com/xml.php?id=1234567890&adif=<CALL:4>K1ABC<QSO_DATE:8>20231027<TIME_ON:4>1234<BAND:3>20M<MODE:2>CW<EOR>"
```

## Known Gotchas
- **Line Endings**: Do NOT mix LF and CRLF line endings in API requests (RFC2616 requires CRLF for HTTP headers). Use CRLF to be safe.
- **ADIF Size**: Very large ADIF files should be chunked if being sent in a URL parameter (better to use POST if available).
- **Session Duration**: The session ID is valid for about 60 minutes of inactivity. Don't re-log for every single query; reuse the ID.
