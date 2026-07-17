# QRZ Logbook Sync

> Sync your RadioLedger logbook with QRZ.com's logbook service.

## Overview

QRZ.com offers a logbook service with QSL confirmation tracking. RadioLedger syncs QSOs bidirectionally: new QSOs upload to QRZ automatically, and QSL confirmations from QRZ flow back into RadioLedger.

QRZ also provides callsign lookup data — when you log a QSO, RadioLedger can prefill the operator's name, QTH, and grid from QRZ.

## Prerequisites

- A QRZ.com account with an **XML Data Subscription** (required for API access)
- Your QRZ logbook API key

## Getting Your QRZ API Key

1. Log in to QRZ.com
2. Go to **My Logbook → Settings → API Access**
3. Copy your Logbook API key (separate from the callsign lookup API key)

TODO: Verify current QRZ API key location — their UI changes occasionally.

## Setup

1. In RadioLedger, go to **Settings → Connected Services → QRZ**
2. Paste your QRZ Logbook API key
3. Click **Connect**
4. RadioLedger tests the connection and begins initial sync

TODO: Screenshot of QRZ setup panel.

## What Gets Synced

| Direction | What |
|-----------|------|
| RadioLedger → QRZ | All new QSOs (with all ADIF fields) |
| QRZ → RadioLedger | QSL confirmations (both sent and received) |
| QRZ → RadioLedger | Callsign lookup data (name, QTH, grid) for auto-fill |

## Sync Schedule

| Direction | Default interval |
|-----------|----------------|
| Outbound (new QSOs → QRZ) | Every 5 minutes |
| Inbound (confirmations → RadioLedger) | Hourly |

## QRZ Callsign Lookup

When enabled, RadioLedger queries QRZ for callsign data when you enter a callsign in the log form:

- **Name**: Prefills the operator name field
- **QTH**: Prefills the location field
- **Grid**: Prefills the Maidenhead grid field
- **DXCC**: Cross-checked against RadioLedger's own resolution

Requires an active QRZ subscription.

TODO: Document how to enable/disable callsign lookup separately from log sync.

## Troubleshooting

**"Authentication failed"**: Verify you're using the Logbook API key, not the callsign lookup API key.

**QSOs not appearing in QRZ?** Check Settings → Sync → QRZ for error details.

**Callsign lookup not working?** Confirm your QRZ subscription includes XML data access.

## Related

- [Sync Overview](index.md)
- [Getting Started: Connect Services](../getting-started/connect-services.md)
- [QSL Management](../user-guide/qsl-management.md)
