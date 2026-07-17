# ARRL Contest API Research

## Official Documentation
- **Log Submission URL**: `https://contest-log-submission.arrl.org/`
- **LoTW Endpoint**: `https://lotw.arrl.org/lotw-help/submitloggingapp/`
- **Docs**: [ARRL Contest Log Submission](http://www.arrl.org/log-submission)

## Authentication
ARRL uses different models for contest logs vs. general log syncing:
- **Contest Logs**: No API key is needed; the user manually uploads a **Cabrillo (.cbr)** file to the web portal.
- **LoTW (Logbook of the World)**: Requires **TQSL 2.x** and a digital certificate (.p12) to sign logs before upload.

## API Interface
- **Method**: Manual upload (POST) for contest logs; XML/POST via TQSL for LoTW.
- **Format**:
  - `Cabrillo`: Fixed-width text format (standard for contests).
  - `ADIF (Signed)`: Binary format (`.tq8`) for LoTW uploads.

## Operations

### 1. Contest Score Submission
- **API**: **None.** ARRL does not have a public API for automated, direct, non-interactive contest log submission.
- **Workflow**: RadioLedger must export a **Cabrillo (.cbr)** file that the user then uploads manually to the [ARRL Submission Portal](https://contest-log-submission.arrl.org/).
- **Validation**: The portal provides a "log check" to ensure the format is correct before final submission.

### 2. LoTW Log Sync (General)
- **API**: Supported via **TQSL integration**.
- **Process**:
  1. Generate ADIF log.
  2. Call the `tqsl` CLI tool to sign the log and produce a `.tq8` file.
  3. Upload the `.tq8` file to LoTW (usually handled by `tqsl` directly).

## Rate Limits & Guidelines
- **Contest Deadline**: Most ARRL contest logs must be submitted within **7 days** of the contest end.
- **TQSL**: TQSL handles rate limiting and security for LoTW uploads automatically.

## Working Implementations
1.  **[N1MM Logger+](https://n1mmwp.hamdocs.com/)** (C#)
    - Industry standard for ARRL contests and Cabrillo generation.
2.  **[k0swe/lotw-go](https://github.com/k0swe/lotw-go)** (Go)
    - Active (2024) library for LoTW integration and TQSL calling.

## Code Snippet (Go - Calling TQSL)
```go
// Example of how to call TQSL to sign and upload
cmd := exec.Command("tqsl", "-a", "LoTW", "-u", "-p", password, adifPath)
cmd.Run()
```

## Gotchas
- **No Direct Submission**: RadioLedger cannot "submit" contest scores on behalf of the user via API; it can only provide the correctly-formatted Cabrillo file.
- **TQSL Dependency**: For LoTW sync, the user **must** have TQSL installed and a valid certificate. RadioLedger cannot sign logs itself without reimplementing the TQSL signing logic (which is discouraged for security reasons).
