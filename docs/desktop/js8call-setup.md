# JS8Call Setup

> Configure JS8Call to send QSOs to RadioLedger automatically via UDP.

JS8Call uses a UDP protocol compatible with WSJT-X. Configuration is nearly identical.

## Setup

In JS8Call: **File → Settings → Reporting** tab

| Setting | Value |
|---------|-------|
| **UDP Server** | `127.0.0.1` |
| **UDP Server port number** | `2242` (JS8Call default; different from WSJT-X's 2237) |
| **Accept UDP requests** | ☑ Checked |

In RadioLedger desktop client: **Settings → UDP → JS8Call**

| Setting | Default |
|---------|---------|
| **Enabled** | ✗ (enable this) |
| **Port** | 2242 |
| **Bind address** | 127.0.0.1 |

TODO: Screenshot of JS8Call settings dialog.

## JS8Call-Specific Notes

- JS8Call has additional message types for heartbeat and messaging that RadioLedger ignores (only QSO log events are processed)
- JS8 QSOs may have longer exchange strings than FT8 — all fields are captured

## Running WSJT-X and JS8Call Together

If you run both simultaneously, use different ports (2237 for WSJT-X, 2242 for JS8Call). Both can be active in the desktop client at the same time.

## Troubleshooting

See [Desktop Client Troubleshooting](troubleshooting.md) for general UDP issues.

## Related

- [WSJT-X Setup](wsjtx-setup.md)
- [Desktop Client Overview](index.md)
- [Troubleshooting](troubleshooting.md)
