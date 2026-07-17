# Cabrillo Export

> Export your contest log in Cabrillo format for submission to contest sponsors.

Cabrillo is the standard format for contest log submission. RadioLedger generates Cabrillo from the structured contest session data — no manual editing required.

## Exporting Cabrillo

1. Open your contest session
2. Click **Export → Cabrillo**
3. Review the Cabrillo header (category, operators, club, etc.)
4. Click **Download**

The Cabrillo file is generated from the `contest_sessions` and `contest_qso_exchange` database tables, ensuring accurate field ordering and no data reconstruction guesswork.

## Cabrillo Header Fields

RadioLedger pre-fills the Cabrillo header from your contest session settings:

```
START-OF-LOG: 3.0
CALLSIGN: W1AW
CONTEST: CQ-WW-SSB
CATEGORY-OPERATOR: SINGLE-OP
CATEGORY-BAND: ALL
CATEGORY-POWER: LOW
CATEGORY-MODE: SSB
OPERATORS: W1AW
CLAIMED-SCORE: 12345
CLUB: Connecticut DX Association
NAME: First Last
ADDRESS: 123 Main St
ADDRESS: City, ST 00000
CREATED-BY: RadioLedger vX.Y
```

TODO: Document all supported Cabrillo header fields and how they map to contest session settings.

## QSO Record Format

The QSO records are generated in the contest-specific format. Example for CQ WW:

```
QSO: 14225 PH 2026-10-24 1432 W1AW          59  05 ZS6ABC         59  38
```

Format: `frequency mode date time mycall sent-rst sent-exchange worked-call rcvd-rst rcvd-exchange`

## Verifying the Log

Before submitting, verify:
- QSO count matches your operating log
- Claimed score is reasonable
- All multipliers are correct
- Category header matches your actual operation

## Submitting to the Contest Sponsor

Each contest has its own submission process:

- **CQ contests**: Robot submission at [cqww.com](https://www.cqww.com)
- **ARRL contests**: Robot at [arrl.org/contest-log-submission](https://www.arrl.org/contest-log-submission)
- **Other contests**: Refer to the contest rules

RadioLedger does not submit directly to contest robots — download the Cabrillo file and submit manually.

TODO: Consider adding direct submission links for major contests.

## Related

- [Contest Setup](setup.md)
- [Multi-Operator Setup](multi-op.md)
- [N1MM+ Integration](n1mm-integration.md)
