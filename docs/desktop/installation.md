# Desktop Client Installation

> Download and install the RadioLedger desktop client on Windows, macOS, or Linux.

## Download

Download the latest release from [github.com/FtlC-ian/radioledger/releases](https://github.com/FtlC-ian/radioledger/releases).

## Platform-Specific Instructions

### macOS

**Requirements:** macOS 12 Monterey or later. Both Apple Silicon (M1/M2/M3) and Intel are supported as a universal binary.

1. Download `RadioLedger_x.y.z_macos_universal.dmg`
2. Open the DMG and drag RadioLedger to Applications
3. First launch: right-click → Open (required for unsigned apps if not yet notarized)
4. macOS may prompt for permission to receive incoming network connections — allow this for UDP functionality

**Gatekeeper:** RadioLedger is notarized by Apple. If you see a warning, run:
```bash
xattr -d com.apple.quarantine /Applications/RadioLedger.app
```

### Windows

**Requirements:** Windows 10 or later (64-bit).

1. Download `RadioLedger_x.y.z_windows_x64_setup.exe`
2. Run the installer (may require administrator rights)
3. Windows Defender may show a SmartScreen warning — click "More info" → "Run anyway"
4. RadioLedger appears in the system tray on completion

**Windows Firewall:** Allow RadioLedger through the firewall for UDP listening (the installer prompts for this).

### Linux

**Requirements:** A modern Linux distribution with libsecret (GNOME Keyring or KDE Wallet) for token storage.

1. Download `RadioLedger_x.y.z_linux_x86_64.AppImage`
2. Make executable: `chmod +x RadioLedger_*.AppImage`
3. Run: `./RadioLedger_*.AppImage`

For system tray support on GNOME, install the AppIndicator extension.

**Debian/Ubuntu package:** TODO — package for APT repository planned.

**Arch Linux (AUR):** TODO — AUR package planned.

## First Launch: Setup Wizard

On first launch, the setup wizard guides you through:

### Step 1: Log In

The wizard opens your system browser to the RadioLedger login page. Log in with your RadioLedger account. After login, the browser redirects back to the desktop client automatically.

Tokens are stored in your OS keychain — not in any config file.

### Step 2: Sync Your Logbook

RadioLedger downloads your logbook from the server. This may take a moment for large logs.

### Step 3: Detect Shack Software

The wizard scans for WSJT-X, JS8Call, N1MM+, and Flrig on default ports. For each one found, it offers to configure the software's UDP output automatically.

### Step 4: Configure LoTW (Optional)

If you use LoTW, the wizard detects your tQSL installation and lists available certificates. Select the certificate and station location to use for LoTW uploads.

See [LoTW Certificates](lotw-certificates.md).

### Step 5: Done

RadioLedger minimizes to the system tray. It runs in the background and syncs automatically.

## Configuration File

The desktop client stores settings in:

| Platform | Location |
|----------|----------|
| macOS | `~/.radioledger/config.yaml` |
| Windows | `%APPDATA%\RadioLedger\config.yaml` |
| Linux | `~/.radioledger/config.yaml` |

This file does **not** contain tokens, passwords, or API keys. It contains:
- Server URL
- UDP port configuration
- tQSL path
- Rig control settings

Edit this file with a text editor if needed. See [DESKTOP_CLIENT.md](../DESKTOP_CLIENT.md) for the full config reference.

## Auto-Updates

RadioLedger checks for updates on startup and periodically. Updates are signed with Ed25519 — you can verify update authenticity. The public key is embedded in the binary.

TODO: Document how to opt into/out of auto-updates, and beta channel availability.

## Uninstalling

| Platform | Method |
|----------|--------|
| macOS | Drag from Applications to Trash |
| Windows | Settings → Apps → RadioLedger → Uninstall |
| Linux | Delete the AppImage file |

On uninstall, RadioLedger asks whether to delete the local logbook cache and configuration. Tokens in the OS keychain are removed automatically.

## Related

- [Desktop Client Overview](index.md)
- [WSJT-X Setup](wsjtx-setup.md)
- [LoTW Certificates](lotw-certificates.md)
- [Troubleshooting](troubleshooting.md)
