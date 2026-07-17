# Mobile App Installation

> Install the RadioLedger mobile app on iOS or Android.

## iOS

**Requirements:** iOS 16 or later.

1. Search for "RadioLedger" in the App Store — TODO: direct App Store link
2. Tap **Get** and authenticate with Face ID / Touch ID / Apple ID
3. Open the app

**Permissions the app requests:**
- **Location** — optional, used for GPS grid square detection and POTA/SOTA park proximity
- **Local network** — not required by RadioLedger (no local discovery)

## Android

**Requirements:** Android 12 (API level 31) or later.

1. Search for "RadioLedger" in Google Play — TODO: direct Play Store link
2. Tap **Install**
3. Open the app

**Permissions the app requests:**
- **Location** — optional, for GPS grid and park proximity
- **Internet** — for sync with RadioLedger server

## First Launch

### Log In

Tap **Sign In**. The app opens your device browser to the RadioLedger login page. After signing in, the browser returns you to the app.

Your login tokens are stored in the iOS Keychain or Android Keystore — not in plain storage.

### Initial Sync

The app downloads:
- Your logbook (recent QSOs, configurable how many)
- DXCC entity reference data
- Callsign lookup cache (compressed, downloaded in background)

This may take a few minutes on first launch. You can start logging while the download completes.

### Offline Database

The callsign lookup database is cached locally so lookups work in the field without connectivity. The database is updated when you open the app with internet access.

TODO: Document callsign database size and update frequency.

## Permissions

### Location (Optional but Recommended)

Location is used for:
- Auto-filling your Maidenhead grid square
- Finding nearby POTA parks
- SOTA altitude verification

You can enter your grid manually if you prefer not to grant location permission.

## Updates

The app updates through the normal App Store / Google Play update mechanism. Enable automatic updates to always have the latest version.

## Troubleshooting

**App won't log in?**
- Ensure you have a RadioLedger account
- Check your internet connection for the initial login
- If the browser redirect fails, try the login URL manually

**Callsign lookup not working offline?**
- The callsign database may still be downloading. Check app settings for download status.

## Related

- [Mobile App Overview](index.md)
- [POTA Activation Guide](pota-activation.md)
- [Offline Logging](offline-logging.md)
