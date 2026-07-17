# POTA Log Upload

> Upload Parks on the Air activation logs to the POTA system.

## Overview

POTA (Parks on the Air) is a hugely popular activity where amateur radio operators activate parks and wildlife areas. Activators must upload their QSOs to the POTA system within a few days of an activation.

RadioLedger streamlines POTA logging — log the activation in the field with the mobile app, then upload to POTA from the desktop or web with one click.

## POTA-Required ADIF Fields

POTA requires specific ADIF fields in your upload:

| ADIF Field | Description | Example |
|-----------|-------------|---------|
| `MY_SIG` | Set to `POTA` | `POTA` |
| `MY_SIG_INFO` | Your park reference | `K-4566` |
| `CALL` | Worked station callsign | `W1AW` |
| `QSO_DATE` | UTC date | `20260228` |
| `TIME_ON` | UTC time | `1432` |
| `BAND` | Band | `20M` |
| `MODE` | Mode | `SSB` |

Optional but recommended:
- `SIG` / `SIG_INFO` — if the worked station is also a POTA activator (P2P QSO)
- `RST_SENT`, `RST_RCVD`
- `GRIDSQUARE`

RadioLedger sets these fields automatically when you log QSOs in **POTA Activation Mode**.

## Prerequisites

- A POTA account at pota.app
- Your callsign registered with POTA

TODO: Confirm POTA developer program API access requirements.

## Setup

TODO: Document POTA account connection flow once API details are confirmed.

1. In RadioLedger, go to **Settings → Connected Services → POTA**
2. Connect your POTA account
3. Click **Connect**

## Logging a POTA Activation

### Using the Mobile App (Recommended for Field)

See [POTA Activation Guide (Mobile)](../mobile/pota-activation.md) for the full in-field workflow.

### Using the Web UI

1. Create a new logbook or use your POTA logbook
2. Click **Start Activation** and enter your park reference
3. Log QSOs normally — the park reference is auto-attached
4. When done, click **Upload to POTA**

## Uploading to POTA

1. Go to your POTA logbook
2. Click **Upload to POTA**
3. Select the activation (park + date)
4. Click **Upload**

RadioLedger generates a POTA-compliant ADIF file and uploads it.

## P2P (Park-to-Park) QSOs

When you work another POTA activator, flag the QSO as P2P:

- Enable **P2P** toggle on the QSO entry form
- Enter the worked station's park reference in `SIG_INFO`

P2P QSOs are counted separately in POTA statistics.

## Validation

Before upload, RadioLedger validates:
- At least 10 QSOs for a valid activation (fewer QSOs = hunter log only)
- Required ADIF fields present
- Park reference format is valid

TODO: Document POTA validation rules.

## Related

- [POTA Activation Guide (Mobile)](../mobile/pota-activation.md)
- [Awards Tracking (POTA)](../user-guide/awards-tracking.md)
- [Sync Overview](index.md)
- [Import/Export](../user-guide/import-export.md)
