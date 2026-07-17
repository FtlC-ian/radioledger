# SOTA Log Upload

> Upload Summits on the Air logs to the SOTA system.

## Overview

SOTA (Summits on the Air) is an award program for operating from mountain summits. Unlike most ham radio data exchange, SOTA uses its own CSV format for log uploads — not ADIF.

RadioLedger handles this conversion automatically: log your summit activation in ADIF format, and RadioLedger converts to the required SOTA CSV when you upload.

## SOTA Log Format

SOTA accepts two CSV formats:

### Activator Log (V2 format)
```
V2
<Callsign>,<SOTA Summit Ref>,<DD/MM/YY>,<HH:MM>,<Worked Callsign>,<Mode>,<Band>,<Sent RST>,<Received RST>
W1AW/P,W0/SP-001,28/02/26,14:32,K7ABC,SSB,14MHz,59,55
```

### Chaser Log (V2 format)
```
V2
<Callsign>,<SOTA Summit Ref>,<DD/MM/YY>,<HH:MM>,<Activator Callsign>,<Mode>,<Band>
W1AW,W0/SP-001,28/02/26,14:32,K7ABC/P,SSB,14MHz
```

RadioLedger generates these from your QSO data automatically.

## Prerequisites

- A SOTA Reflector account at sotawatch.org
- Your callsign registered with SOTA

## Setup

TODO: Document SOTA account connection flow once API details are confirmed.

## Logging a SOTA Activation

### Using the Mobile App (Recommended)

See [SOTA Activation Guide (Mobile)](../mobile/sota-activation.md) for the in-summit workflow.

### Using the Web UI

1. In your logbook, click **Start Activation → SOTA**
2. Enter your summit reference (e.g., `W0/SP-001`)
3. Log QSOs normally — summit reference is auto-attached to each QSO
4. When done, click **Upload to SOTA**

## Uploading to SOTA

1. Go to your activation logbook
2. Click **Upload to SOTA**
3. Select activation type: Activator or Chaser
4. Click **Upload**

RadioLedger converts your ADIF QSOs to SOTA CSV format and uploads.

## S2S (Summit-to-Summit) QSOs

When you work another SOTA activator from your summit:

- Enable the **S2S** toggle on the QSO
- Enter the worked activator's summit reference in `SOTA_REF`

S2S QSOs are counted separately for bonus points.

## ADIF Fields Used

| ADIF Field | SOTA equivalent | Notes |
|-----------|-----------------|-------|
| `MY_SOTA_REF` | Your summit | Set automatically in activation mode |
| `SOTA_REF` | Worked station's summit | For S2S QSOs |
| `CALL` | Worked callsign | |
| `QSO_DATE` + `TIME_ON` | Date/time | |
| `BAND` | Band (MHz format) | Converted to SOTA format |
| `MODE` | Mode | |
| `RST_SENT` / `RST_RCVD` | RST reports | |

## Related

- [SOTA Activation Guide (Mobile)](../mobile/sota-activation.md)
- [Awards Tracking (SOTA)](../user-guide/awards-tracking.md)
- [Sync Overview](index.md)
