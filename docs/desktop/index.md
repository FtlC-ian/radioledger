# Desktop Client

> The RadioLedger desktop app for your shack — UDP auto-logging, LoTW signing, and rig control.

The desktop client is the bridge between your shack software and RadioLedger. It's not a standalone logger — the RadioLedger server is the source of truth — but it brings RadioLedger to your desktop and connects your existing logging workflow.

## What the Desktop Client Does

| Feature | Description |
|---------|-------------|
| **Auto-logging from WSJT-X** | Listens for UDP broadcasts and logs FT8/FT4/etc. QSOs automatically |
| **JS8Call integration** | Same UDP protocol, auto-logs JS8Call QSOs |
| **N1MM+ integration** | Receives N1MM+ UDP broadcasts for contest QSOs |
| **LoTW signing** | Signs and uploads QSOs to LoTW using your local tQSL certificate |
| **Rig control** | Reads frequency and mode from Flrig/Hamlib |
| **Offline buffer** | Queues QSOs when server is unreachable, syncs when back |
| **System tray** | Runs in the background with a minimal system tray presence |
| **Local cache** | Encrypted local copy of your logbook for offline access |

**Security note:** Desktop-local LoTW signing keeps your tQSL certificate material on your machine.

## Supported Platforms

| Platform | Status |
|----------|--------|
| macOS 12+ (Apple Silicon + Intel) | ✓ Supported |
| Windows 10/11 | ✓ Supported |
| Linux (AppImage) | ✓ Supported |

Built with [Tauri](https://tauri.app) — small binary (~5MB), native system tray, no Electron/Chromium overhead.

## In This Section

| Guide | What it covers |
|-------|---------------|
| [installation.md](installation.md) | Download and install on Windows, macOS, Linux |
| [wsjtx-setup.md](wsjtx-setup.md) | Configure WSJT-X UDP for auto-logging |
| [js8call-setup.md](js8call-setup.md) | Configure JS8Call UDP |
| [n1mm-setup.md](n1mm-setup.md) | Configure N1MM+ for contest logging |
| [rig-control.md](rig-control.md) | Flrig and Hamlib rig control setup |
| [lotw-certificates.md](lotw-certificates.md) | tQSL certificate management |
| [troubleshooting.md](troubleshooting.md) | Common issues and solutions |

## Quick Setup

1. [Download and install](installation.md)
2. Launch the app — it opens the setup wizard
3. Log in with your RadioLedger account (OAuth via system browser)
4. Let the wizard detect WSJT-X, JS8Call, and tQSL
5. Configure LoTW certificate (if you use LoTW)
6. Done — the app runs in the system tray

## Security Design

- Tokens stored in OS keychain (never in config files)
- UDP listener binds to `127.0.0.1` by default (loopback only — LAN injection protection)
- Local logbook cache encryption with SQLCipher is planned for stable releases; verify release notes before assuming local QSO data is encrypted at rest
- Auto-updates signed with Ed25519 signatures

See [Desktop Client Architecture](../DESKTOP_CLIENT.md) for full security details.

## Related

- [LoTW Sync](../sync/lotw.md)
- [WSJT-X Setup](wsjtx-setup.md)
- [rig Control](rig-control.md)
- [Getting Started: Connect Services](../getting-started/connect-services.md)
