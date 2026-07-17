# eQSL.cc API Research

## API Status
- **Status**: Active / Public (requires Bronze, Silver, or Gold membership for some features)
- **Official Documentation**: [https://www.eqsl.cc/qslcard/AgDocumentation.cfm](https://www.eqsl.cc/qslcard/AgDocumentation.cfm)

## Authentication Method
- **Method**: Username + Password (Basic HTTP Authentication or plain POST fields).
- **Parameters**: `EQSL_USER`, `EQSL_PSWD` (passed in the request body).
- **Security Note**: Plaintext password submission is the standard method here; use HTTPS (`https://www.eqsl.cc/...`).

## Available Operations
- **ADIF Upload**: [https://www.eqsl.cc/qslcard/ImportADIF.cfm](https://www.eqsl.cc/qslcard/ImportADIF.cfm)
- **Inbound eQSL Check**: Query the server for new confirmed eQSLs.
- **Card Download**: Fetch the digital card image (requires membership).

## Rate Limits and Usage Guidelines
- **Guidelines**: "Fair use" policy. Do not hammer the server.
- **Membership**: Basic (Free) accounts can upload. Automated downloading of received cards usually requires a "Bronze" or higher membership level ($12/year).

## Working Open-Source Implementations
1. **Cloudlog**: `application/models/Eqsl_model.php` (comprehensive PHP implementation).
2. **Hamlib-Tools/Eqsl-Uploader**: [https://github.com/on6zq/Eqsl-Uploader](https://github.com/on6zq/Eqsl-Uploader) (Perl script).
3. **Log4OM**: Mature C# implementation of eQSL sync.

## Code Snippet (Upload Example)
```bash
curl -X POST https://www.eqsl.cc/qslcard/ImportADIF.cfm \
     -F "EQSL_USER=MYCALL" \
     -F "EQSL_PSWD=MYPASSWORD" \
     -F "ADIFData=<CALL:4>K1ABC<QSO_DATE:8>20231027<TIME_ON:4>1234<BAND:3>20M<MODE:2>CW<EOR>"
```

## Known Gotchas
- **Plaintext Auth**: Many modern developers are wary of sending passwords in plain POST fields, but it is the required method for eQSL. Always use the HTTPS endpoint.
- **Response Format**: The response is often HTML/text that needs to be parsed (e.g., searching for "Result: OK" or "Records Imported: 1").
- **Syncing**: There is no "update" mechanism. If you upload the same QSO twice, it may result in a "Duplicate QSO" error.
- **ADIF Tags**: Be sure to include `MY_RIG`, `MY_ANTENNA`, etc., if you want them on the digital card.
