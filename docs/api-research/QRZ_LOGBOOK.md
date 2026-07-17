# QRZ Logbook API Research

## API Status
- **Status**: Active / Public (requires XML Logbook Data subscription)
- **Official Documentation**: [https://www.qrz.com/docs/logbook/QRZLogbookAPI.html](https://www.qrz.com/docs/logbook/QRZLogbookAPI.html)

## Authentication Method
- **Method**: API Key (specifically a Logbook API Key, not the general XML API key).
- **Process**: User obtains the key from their Logbook Settings on QRZ.com. Each logbook has its own unique API key.
- **Parameters**: `KEY` and `ACTION` are passed in HTTP POST requests.

## Available Operations
- **INSERT**: Add a new QSO to the logbook. Uses ADIF format data in the `ADATA` field.
- **DELETE**: Delete a QSO by `LOGID`.
- **FETCH**: Retrieve one or more QSOs based on filters (e.g., `LOGID`, `CALL`, `START`, `END`).
- **STATUS**: Check the status of the logbook/API key.

## Rate Limits and Usage Guidelines
- **Requirement**: Requires a paid subscription (XML Logbook Data).
- **Limit**: No specific numeric rate limit is mentioned in the official doc, but "fair use" is implied. Most integrations sync on-demand or at fixed intervals.

## Working Open-Source Implementations
1. **PyQRZ** (Python): [https://github.com/vsergeev/pyqrz](https://github.com/vsergeev/pyqrz)
2. **QRZ-Logbook-API-Client** (PHP): [https://github.com/K0YI/QRZ-Logbook-API-Client](https://github.com/K0YI/QRZ-Logbook-API-Client)
3. **Cloudlog** (PHP/CodeIgniter): Extensive QRZ integration in `application/models/Logbook_model.php`.

## Code Snippet (POST Example)
```bash
curl -X POST https://logbook.qrz.com/api \
     -d "KEY=YOUR-LOGBOOK-API-KEY" \
     -d "ACTION=INSERT" \
     -d "ADATA=<CALL:6>W1AW/7 <QSO_DATE:8>20231027 <TIME_ON:4>1234 <BAND:3>20M <MODE:2>CW <EOR>"
```

## Known Gotchas
- **API Key vs Session Key**: QRZ has a general XML Subscription API (callsign lookup) and a Logbook API. They use different keys and different endpoints (`xmldata.qrz.com` vs `logbook.qrz.com/api`).
- **ADIF Encoding**: The `ADATA` field must be valid ADIF. QRZ is sensitive to certain ADIF tags; ensure mandatory fields like `CALL`, `QSO_DATE`, `TIME_ON`, `BAND`, and `MODE` are present.
- **Synchronous Response**: The API returns an ADIF-formatted response indicating success or failure.
