# N1MM+ Setup

> Integrate N1MM+ contest logger with RadioLedger via UDP broadcast.

N1MM+ is the most popular contest logging software for Windows. RadioLedger integrates with N1MM+ via its UDP broadcast feature, allowing contest QSOs to appear in RadioLedger in real time.

## Prerequisites

- N1MM+ installed and configured for your contest
- A [contest logbook](../contest/index.md) set up in RadioLedger
- RadioLedger desktop client installed

## N1MM+ UDP Configuration

In N1MM+: **Config → Configure Ports, Mode Control, Audio, Other**

Under **Broadcast Data**:

| Setting | Value |
|---------|-------|
| **Enable UDP Broadcast** | ✓ Checked |
| **Broadcast Address** | `127.0.0.1` |
| **Broadcast Port** | `12060` |
| **Contacts** | ✓ Checked (ContactInfo, ContactReplace) |
| **Radio** | ✓ Checked (RadioInfo) |

TODO: Screenshot of N1MM+ broadcast configuration.

## RadioLedger Desktop Client

In desktop client: **Settings → UDP → N1MM+**

| Setting | Value |
|---------|-------|
| **Enabled** | ✓ |
| **Port** | 12060 |
| **Target Logbook** | Select your contest logbook |

## N1MM+ Message Types

RadioLedger handles:

| Message | What it triggers |
|---------|-----------------|
| `ContactInfo` | Logs a new QSO |
| `ContactReplace` | Updates an existing QSO |
| `RadioInfo` | Updates frequency/mode in the current QSO form |

## Contest Scoring

N1MM+ handles dupe checking and score calculation. RadioLedger receives the QSOs for logging and award tracking purposes. For export as Cabrillo, use N1MM+ directly or see [Cabrillo Export](../contest/cabrillo-export.md).

## Troubleshooting

**QSOs not appearing?** Verify N1MM+ is broadcasting to `127.0.0.1:12060` and the desktop client is running with N1MM+ integration enabled.

**Wrong logbook?** Set the target logbook in desktop client → N1MM+ settings.

## Related

- [Contest Overview](../contest/index.md)
- [N1MM+ Integration](../contest/n1mm-integration.md)
- [Desktop Client Overview](index.md)
- [Troubleshooting](troubleshooting.md)
