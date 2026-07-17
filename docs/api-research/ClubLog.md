# ClubLog API Research

## ⚠️ IMPLEMENTATION STATUS
- **Code Status**: Implemented but **UNTESTED**
- **Blocker**: Requires RadioLedger developer API key from ClubLog
- **Request Process**: Submit request at [https://clublog.org/need_api.php](https://clublog.org/need_api.php)
- **User Accounts**: Free (users create accounts at clublog.org)
- **Testing**: Cannot test until developer API key is obtained

## API Status
- **Status**: Active / Public (requires developer API key)
- **Official Documentation**: [https://clublog.freshdesk.com/support/solutions/folders/100474](https://clublog.freshdesk.com/support/solutions/folders/100474)

## Authentication Method
- **Method**: API Key + User Login (Email + Password).
- **Process**: Developers must request an API key via the [ClubLog Helpdesk](https://clublog.org/need_api.php).
- **Parameters**: `api`, `email`, `password`.

## Available Operations
- **Upload ADIF**: POST `https://clublog.org/put_adif.php` with file content.
- **Delete QSO**: POST `https://clublog.org/delete_qso.php`.
- **Fetch DXCC Info**: Query prefix and exceptions XML (public, no key needed).
- **Live Stream**: Send real-time QSO updates as they happen.

## Rate Limits and Usage Guidelines
- **Limits**: ClubLog limits large uploads (ADIF files) to be processed in a background queue. Small, single-QSO uploads (real-time) are preferred.
- **Developer API Key**: Required for any software that automates uploads for other users.

## Working Open-Source Implementations
1. **ClubLog Python Library**: [https://github.com/vsergeev/pyclublog](https://github.com/vsergeev/pyclublog) (Simple, well-structured).
2. **Cloudlog**: `application/models/Clublog_model.php` (PHP implementation of both bulk and real-time sync).
3. **Log4OM**: Comprehensive C# implementation with live streaming.

## Code Snippet (Real-Time Upload Example)
```bash
curl -X POST https://clublog.org/put_adif.php \
     -d "api=YOUR-DEVELOPER-API-KEY" \
     -d "email=user@example.com" \
     -d "password=USER-PASSWORD" \
     -d "callsign=MYCALL" \
     -d "adif=<CALL:4>K1ABC<QSO_DATE:8>20231027<TIME_ON:4>1234<BAND:3>20M<MODE:2>CW<EOR>"
```

## Known Gotchas
- **Email vs Callsign**: ClubLog uses the user's login email for authentication, not the callsign.
- **Real-Time Stream**: For "Live" updates, the API expects a simplified POST format to `https://clublog.org/livestream.php`.
- **ADIF Size**: Large ADIF files should be sent as a file upload (`-F file=@...`) rather than a simple POST field to avoid timeout issues.
- **Developer Key Requirement**: Unlike QRZ, where users get their own keys, ClubLog requires a single key for the *application* (RadioLedger) and then user credentials for the individual account.
