# Managing Logbooks

> Create and organize multiple logbooks for home, portable, contest, and club operations.

## What Is a Logbook?

A logbook is a named, independent collection of QSOs. You can have multiple logbooks under one account. Each logbook has its own:

- QSO list
- Awards progress (DXCC, WAS, VUCC, etc.)
- Sync service configuration
- Station callsign(s)

## Default Logbook

When you create your account, RadioLedger creates a default logbook named after your callsign. For most operators, this is the only logbook you need.

## Why Use Multiple Logbooks?

Common reasons:

| Scenario | Logbook strategy |
|----------|-----------------|
| Home station | Default logbook |
| Club station | Separate logbook per club callsign |
| POTA activations | Separate portable logbook (or per-activation) |
| Contest operations | Separate logbook per contest (or per season) |
| Different callsigns (e.g., /MM, /P) | Separate logbook or single logbook with station callsign filter |

## Creating a Logbook

1. Click your account name → **Logbooks**
2. Click **+ New Logbook**
3. Enter a name (e.g., "POTA - K-4566")
4. Select the primary station callsign for this logbook
5. Optionally set a description
6. Click **Create**

TODO: Screenshot of the new logbook dialog.

## Logbook Settings

Each logbook has configurable settings:

| Setting | Description |
|---------|-------------|
| **Name** | Display name |
| **Station callsign** | Primary callsign for this logbook |
| **Operator** | Default operator (your call, or leave blank for club use) |
| **Default mode** | Pre-fill mode on new QSOs |
| **Default power** | Pre-fill TX power on new QSOs |
| **Dedup window** | Seconds for duplicate detection (default: 30) |
| **Sync services** | Which services to sync this logbook to |
| **Awards tracking** | Enable/disable awards calculation for this logbook |

TODO: Document all logbook settings in detail.

## Switching Between Logbooks

The logbook selector is in the top navigation bar. Click it to switch. The URL changes to reflect the active logbook UUID — you can bookmark specific logbooks.

## Sharing a Logbook

TODO: Logbook sharing / multi-operator access is planned. Describe when available.

For club stations, multiple operators can share a logbook. Each operator logs in with their own account and selects the shared logbook. QSOs record both the station callsign and the individual operator's callsign.

## Merging Logbooks

TODO: Describe logbook merge workflow if/when implemented.

## Deleting a Logbook

Go to **Logbook Settings → Delete Logbook**. This soft-deletes the logbook and all its QSOs. Recovery is available for 30 days via support.

You cannot delete your only logbook.

## Exporting a Logbook

Export the entire logbook as ADIF from **Logbook Settings → Export → ADIF**.

See [Import/Export](import-export.md) for details.

## Related

- [Logging QSOs](logging-qsos.md)
- [Import/Export](import-export.md)
- [Awards Tracking](awards-tracking.md)
- [Settings](settings.md)
