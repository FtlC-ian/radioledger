# ARRL Contest APIs Research

## API Status
- **Status**: Manual-Only (No Public Submission API)
- **Official Documentation**: [https://contest-log-submission.arrl.org/](https://contest-log-submission.arrl.org/)
- **Primary Host**: `https://contest-log-submission.arrl.org/`

## Authentication Method
- **Method**: Manual Web Form.
- **Security**: Requires an email address and callsign verification on the website.

## Available Operations
- **Log Upload**: Manual upload of Cabrillo format files.
- **Log Status**: Check if a log has been received.
- **Score Reporting**: Manual entry of final score before log submission.

## Rate Limits and Usage Guidelines
- **Guidelines**: Logs must be submitted within a specific timeframe (usually 5-7 days after the contest ends).
- **Log Format**: Cabrillo 3.0 is the required format. ADIF is not accepted for contest logs.

## Working Open-Source Implementations
1. **N1MM Logger+**: The standard for contest logging (Closed source but free).
2. **TR4W**: Open-source contest logger (Delphi). [https://github.com/tr4w/tr4w](https://github.com/tr4w/tr4w)
3. **Cabrillo.js**: JavaScript library for parsing/generating Cabrillo logs. [https://github.com/vsergeev/cabrillo-js](https://github.com/vsergeev/cabrillo-js)

## Code Snippet (Manual Form POST Mockup)
*There is no supported programmatic way to submit contest logs.*
```bash
# Example of what the manual web form submission looks like internally (for reference)
curl -X POST "https://contest-log-submission.arrl.org/upload" \
     -F "callsign=K1ABC" \
     -F "email=user@example.com" \
     -F "file=@/path/to/log.log"
```

## Known Gotchas
- **Cabrillo Format**: ARRL and most major contest sponsors use the Cabrillo format for log submission, which is significantly different from ADIF. It is a column-oriented plain text format.
- **Contest Deadlines**: Deadlines are strictly enforced. Automated submission would be a significant feature, but ARRL has not yet provided an official API for it.
- **Email Submission**: Some contests still allow log submission via email (usually `contests@arrl.org`), but this is discouraged in favor of the web portal.
- **Real-Time Scoreboards**: While not for log submission, many contesters use "Real-Time Scoreboard" APIs (e.g., [https://contestonescore.com/](https://contestonescore.com/)) to stream their progress during the event.
