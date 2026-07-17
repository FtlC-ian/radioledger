# N1MM+ Integration

> Use N1MM+ as your contest logging front-end with RadioLedger receiving QSOs in real time.

Many contest operators prefer N1MM+ for its advanced dupe checking, sprint mode, and SO2R features. RadioLedger integrates via N1MM+'s UDP broadcast — N1MM+ remains your contest logger, but QSOs appear in RadioLedger for archiving, sync, and award tracking.

## Setup

### Desktop Client

N1MM+ integration goes through the RadioLedger desktop client. See [N1MM+ Desktop Setup](../desktop/n1mm-setup.md) for the complete configuration guide.

### Contest Session

Create a contest session in RadioLedger before the contest — this is where N1MM+ QSOs land.

1. Create a contest session: [Contest Setup](setup.md)
2. In desktop client settings, set the N1MM+ target logbook to your contest session

## What Gets Synced from N1MM+

| N1MM+ message | RadioLedger action |
|---------------|-------------------|
| `ContactInfo` | Creates new QSO in the contest session |
| `ContactReplace` | Updates an existing QSO |
| `ContactDelete` | Soft-deletes the QSO |
| `RadioInfo` | Updates frequency/mode display (not stored) |

## Limitations

RadioLedger receives QSOs from N1MM+ — it does NOT send dupe/mult data back to N1MM+. N1MM+ handles all contest scoring logic. RadioLedger is a receiver/archive in this workflow.

## Cabrillo Export

Use N1MM+ for Cabrillo export (it has more contest-specific formatting options). Alternatively, if your contest session in RadioLedger is complete, you can export from [RadioLedger Cabrillo export](cabrillo-export.md).

## Related

- [N1MM+ Desktop Setup](../desktop/n1mm-setup.md)
- [Contest Setup](setup.md)
- [Cabrillo Export](cabrillo-export.md)
