# Logging QSOs

> Everything about manually entering, editing, and managing QSOs in RadioLedger.

## Quick Entry

Click **+ Log QSO** (or press `N`) from any logbook view. The QSO entry form opens.

TODO: Screenshot of the QSO entry form.

## Required Fields

Every QSO needs at minimum:

| Field | Notes |
|-------|-------|
| **Callsign** (`CALL`) | Type as worked, including suffixes like `/P` or `/MM` |
| **Date** (`QSO_DATE`) | UTC date (YYYY-MM-DD) |
| **Time** (`TIME_ON`) | UTC time QSO started (HHMM or HHMMSS) |
| **Band** (`BAND`) | Select from list, or RadioLedger derives from frequency |
| **Mode** (`MODE`) | Select from list (SSB, CW, FT8, etc.) |

## Optional Fields

RadioLedger supports all standard ADIF fields. Commonly used:

| Field | ADIF name | Notes |
|-------|-----------|-------|
| Frequency | `FREQ` | MHz (e.g., 14.225); band auto-derived |
| RST sent | `RST_SENT` | 59 for phone, 599 for CW |
| RST received | `RST_RCVD` | |
| Operator name | `NAME` | Their name |
| QTH | `QTH` | Their city/location |
| Grid square | `GRIDSQUARE` | 4 or 6 char Maidenhead (e.g., `FN42aa`) |
| Power (W) | `TX_PWR` | Your transmit power in watts |
| Antenna | `ANT_PATH` | Propagation path |
| Contest ID | `CONTEST_ID` | For contest QSOs |
| Comment | `COMMENT` | Free text notes |
| IOTA ref | `IOTA` | Islands on the Air reference |
| SOTA ref | `SOTA_REF` | Summit reference |
| POTA ref | `POTA_REF` | Park reference |
| State | `STATE` | US state (for WAS tracking) |
| CQ zone | `CQZ` | CQ zone |
| ITU zone | `ITUZ` | ITU zone |

Any ADIF field not listed above can be added via the **Advanced Fields** panel.

## Callsign Entry

Type the callsign exactly as it was used:

- `W1AW` — standard callsign
- `W1AW/P` — portable operation
- `W1AW/MM` — maritime mobile
- `W1AW/7` — operating in W7-land
- `VK9/W1AW` — prefix override (entity is VK9, not USA)

RadioLedger auto-resolves the DXCC entity. You never need to set the DXCC field manually.

Callsigns are normalized to uppercase. The original form is preserved in the database.

## Submode / Digital Modes

For digital modes, select the submode where applicable:

- FT8 exports as `MODE=FT8`
- FT4 exports as `MODE=MFSK`, `SUBMODE=FT4`
- JS8 exports as `MODE=MFSK`, `SUBMODE=JS8`
- RTTY stays `MODE=RTTY`

FT8, FT4, JS8, Q65, DMR, VARA, and similar common digital names can be selected directly. RadioLedger accepts those friendly names on input and rewrites recognized pairs to canonical ADIF export values. Plain `VARA` currently maps to VARA HF. For VARA FM 1200, enter `VARAFM1200` explicitly.

TODO: List all supported modes and submodes — see [Bands and Modes](../reference/bands-and-modes.md).

## Editing a QSO

Click any QSO row in the logbook to open the detail view. Click **Edit** to modify fields. All edits are logged with a timestamp.

QSOs that have already been uploaded to LoTW cannot have core fields (callsign, band, mode, datetime) changed without creating a new LoTW transaction. RadioLedger warns you before you edit these fields.

## Deleting a QSO

In the QSO detail view, click **Delete**. The QSO is soft-deleted (recoverable for 30 days via Settings → Deleted QSOs).

Deleted QSOs that were uploaded to LoTW are NOT automatically deleted from LoTW — LoTW does not support QSO deletion.

## Bulk Actions

From the logbook list, select multiple QSOs using the checkbox column:

- **Export selected** — download as ADIF
- **Delete selected** — bulk soft-delete
- **Assign to logbook** — move to a different logbook

TODO: Document any additional bulk actions.

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `N` | New QSO |
| `Enter` | Save QSO (when form focused) |
| `Esc` | Cancel / close form |
| `/` | Focus search bar |

TODO: Complete keyboard shortcut list.

## Auto-Population

When the desktop client is connected and rig control is configured, RadioLedger auto-populates:

- **Frequency** and **mode** from Flrig/Hamlib
- **Callsign** from the last decoded signal in WSJT-X

TODO: Detail the auto-population fields and how to enable/disable each.

## Related

- [Logbooks](logbooks.md)
- [Search and Filter](search-and-filter.md)
- [Import/Export](import-export.md)
- [Callsign Parsing](../reference/callsign-parsing.md)
- [Bands and Modes](../reference/bands-and-modes.md)
- [Desktop Client Auto-Logging](../desktop/wsjtx-setup.md)
