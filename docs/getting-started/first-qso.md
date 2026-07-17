# Log Your First QSO

> Walk through logging a QSO manually from start to finish.

This guide assumes you have an account on your own RadioLedger deployment. If you have not set one up yet, start with the [installation guide](installation.md).

## Before You Begin

- Log in to RadioLedger (web UI or desktop client)
- Have your station callsign ready
- Know the callsign of the station you worked, the band, and the mode

## Step 1: Open Your Logbook

On first login, RadioLedger creates a default logbook named after your callsign. You can create additional logbooks for portable operations, club callsigns, or contest logging — see [Managing Logbooks](../user-guide/logbooks.md).

The logbook page shows your recent QSOs first. If this is a new account, the list is empty and the primary action is **+ Log QSO**.

## Step 2: Click "New QSO"

Click the **+ Log QSO** button in the top-right corner of the logbook view.

The QSO form starts with callsign, UTC date/time, band, and mode. Optional fields such as grid, name, QTH, frequency, power, and comments can be filled as needed.

## Step 3: Fill In the QSO Details

### Required Fields

| Field | Description | Example |
|-------|-------------|---------|
| **Callsign** | The callsign of the station you worked | `W1AW` |
| **Date** | UTC date of the QSO | `2026-02-28` |
| **Time (UTC)** | UTC time QSO started | `1432` |
| **Band** | Amateur band | `20m` |
| **Mode** | Operating mode | `SSB` |

All times are UTC. See [UTC discipline](../reference/glossary.md#utc) if you're not familiar with operating in UTC.

### Optional but Recommended Fields

| Field | Description |
|-------|-------------|
| **RST Sent** | Signal report you sent (59 for phone, 599 for CW) |
| **RST Received** | Signal report you received |
| **Name** | Operator name |
| **QTH** | Operator location |
| **Grid** | Maidenhead grid square (e.g., `FN42`) |
| **Frequency (MHz)** | Exact frequency (e.g., `14.225`) |
| **Power (watts)** | Your transmit power |
| **Comment** | Any notes about the QSO |

RadioLedger auto-resolves the DXCC entity from the callsign — you don't need to fill that in manually.

### Callsign Suffixes

Type callsigns exactly as they were used, including portable suffixes:
- `W1AW/P` — portable
- `W1AW/MM` — maritime mobile
- `VK9/W1AW` — callsign with prefix override (DXCC entity is VK9, not USA)

RadioLedger handles DXCC resolution automatically. See [Callsign Parsing](../reference/callsign-parsing.md) for details.

## Step 4: Save the QSO

Click **Save QSO**. The QSO appears immediately in your logbook.

After saving, return to the logbook list and confirm the callsign, UTC timestamp, band, and mode look right.

## Step 5: What Happens Next

RadioLedger automatically:

- Resolves the DXCC entity from the callsign
- Checks if this is a new band/mode/entity for your awards progress
- Queues the QSO for sync to any connected services (LoTW, QRZ, etc.)

Confirmation status starts as pending until a connected service such as LoTW, QRZ, eQSL, or Club Log reports a match. Paper QSL cards can be tracked manually.

## Editing a QSO

Click any QSO in the list to open the detail view. Click **Edit** to make changes.

## Deleting a QSO

In the QSO detail view, click **Delete**. Deleted QSOs are soft-deleted first instead of being removed from the database immediately.

Retention depends on the deployment policy. Self-hosters should check their cleanup settings before assuming deleted QSOs are recoverable indefinitely.

## Next Steps

- **[Import your existing log](import-existing-log.md)** — don't retype years of QSOs
- **[Connect LoTW and QRZ](connect-services.md)** — automate confirmations
- **[Set up the desktop client](../desktop/index.md)** — auto-log from WSJT-X
- **[Set up the mobile app](../mobile/index.md)** — log POTA activations in the field

## Related

- [Logging QSOs (User Guide)](../user-guide/logging-qsos.md)
- [Callsign Parsing Reference](../reference/callsign-parsing.md)
- [Bands and Modes](../reference/bands-and-modes.md)
- [Glossary](../reference/glossary.md)
