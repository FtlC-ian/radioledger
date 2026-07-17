# Mobile App

> Log QSOs in the field with the RadioLedger mobile app — designed for POTA and SOTA activations.

The RadioLedger mobile app is built for operators on the go. Whether you're activating a park for POTA, climbing a summit for SOTA, or just operating portable, the mobile app keeps your log running even without cell service.

## Key Features

| Feature | Description |
|---------|-------------|
| **Offline-first logging** | Log QSOs without internet — syncs when you reconnect |
| **POTA activation mode** | Park reference, QSO counter, validation, upload |
| **SOTA activation mode** | Summit reference, S2S flagging, CSV upload |
| **Quick entry UI** | Large touch targets, one-tap band/mode, glove-friendly |
| **Callsign lookup** | Name, QTH, grid — works offline with cached database |
| **Self-spot** | Post to POTA and SOTA spotting networks |
| **GPS integration** | Auto-detect nearby parks, grid square, altitude |

## Platform Support

| Platform | Status |
|----------|--------|
| iOS 16+ | ✓ Supported |
| Android 12+ | ✓ Supported |

TODO: App Store and Google Play links when available.

## Design Philosophy

Logging a QSO in the field should take under 5 seconds. The mobile app is designed for:

- **Fat fingers** — large touch targets, buttons not dropdowns for band/mode
- **Sunlight** — high-contrast theme option
- **Gloves** — capacitive-tip glove compatible
- **Battery** — dark mode default, minimal background GPS drain
- **No signal** — everything important works offline

## In This Section

| Guide | What it covers |
|-------|---------------|
| [installation.md](installation.md) | Install on iOS and Android |
| [pota-activation.md](pota-activation.md) | Full POTA activation workflow |
| [sota-activation.md](sota-activation.md) | SOTA summit activation workflow |
| [offline-logging.md](offline-logging.md) | Logging without connectivity |
| [sync.md](sync.md) | Syncing with the RadioLedger server |

## Quick Start

1. Install the app from the App Store or Google Play
2. Log in with your RadioLedger account
3. The app downloads your logbook for offline access
4. For POTA/SOTA: tap **Start Activation** and select your park/summit

## Related

- [POTA Sync](../sync/pota.md)
- [SOTA Sync](../sync/sota.md)
- [Desktop Client](../desktop/index.md)
- [Offline-First Architecture](../MOBILE_APP.md)
