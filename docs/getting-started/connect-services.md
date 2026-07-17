# Connect External Services

> Link LoTW, QRZ, eQSL, ClubLog, and other services to automate QSL confirmations and log sync.

RadioLedger syncs with the major ham radio logging services so you don't have to upload separately to each one. Configure a service once and RadioLedger keeps everything in sync automatically.

## Available Services

| Service | What it does | Auth type |
|---------|-------------|-----------|
| [LoTW](../sync/lotw.md) | ARRL QSL confirmation | tQSL certificate (desktop client required) |
| [QRZ Logbook](../sync/qrz.md) | QRZ.com log sync, callsign data | API key (QRZ subscription required) |
| [eQSL](../sync/eqsl.md) | Electronic QSL cards | Username/password |
| [ClubLog](../sync/clublog.md) | DXCC tracking, propagation data | API key |
| [POTA](../sync/pota.md) | Parks on the Air log upload | POTA account |
| [SOTA](../sync/sota.md) | Summits on the Air log upload | SOTA account |

## Setting Up LoTW

LoTW requires signing QSOs with an ARRL tQSL certificate. RadioLedger supports desktop-local signing through the desktop client, keeping certificate material on the operator's computer.

For desktop-local signing:

1. [Install the desktop client](../desktop/index.md)
2. Run the setup wizard — it detects your tQSL installation
3. Select your certificate and station location
4. LoTW sync runs automatically while the desktop client is running

→ **Full guide: [LoTW Setup](../sync/lotw.md)**

## Setting Up QRZ Logbook

1. Go to **Settings → Connected Services → QRZ**
2. Enter your QRZ API key (requires a QRZ subscription)
3. Click **Connect**
4. RadioLedger uploads new QSOs to QRZ and pulls confirmations on schedule

→ **Full guide: [QRZ Setup](../sync/qrz.md)**

## Setting Up eQSL

1. Go to **Settings → Connected Services → eQSL**
2. Enter your eQSL username and password
3. Click **Connect**

→ **Full guide: [eQSL Setup](../sync/eqsl.md)**

## Setting Up ClubLog

TODO: Describe ClubLog API key setup flow.

→ **Full guide: [ClubLog Setup](../sync/clublog.md)**

## Setting Up POTA

TODO: Describe POTA account connection flow.

→ **Full guide: [POTA Logging](../sync/pota.md)**

## Setting Up SOTA

TODO: Describe SOTA account connection flow.

→ **Full guide: [SOTA Logging](../sync/sota.md)**

## Sync Frequency

By default, RadioLedger syncs with each service every 5 minutes for outbound QSOs, and polls for inbound confirmations hourly. Intervals are configurable in Settings.

TODO: Document sync interval settings.

## Viewing Sync Status

The logbook view shows per-QSO sync status icons:

| Icon | Meaning |
|------|---------|
| ✓ | Uploaded to this service |
| ⟳ | Pending upload |
| ✉ | QSL confirmed by this service |
| ✗ | Upload error (hover for details) |

TODO: Finalize sync status icon design.

## Troubleshooting

**LoTW not syncing?** Make sure the desktop client is running. LoTW signing happens on your machine.

**QRZ showing errors?** Verify your API key is a Logbook API key, not a callsign lookup key.

**eQSL rate limit?** eQSL has aggressive rate limits. RadioLedger backs off automatically.

TODO: More troubleshooting scenarios for each service.

## Related

- [Sync Overview](../sync/index.md)
- [LoTW Setup](../sync/lotw.md)
- [Desktop Client (required for LoTW)](../desktop/index.md)
- [QSL Management](../user-guide/qsl-management.md)
