# Syncing with the Server

> Understand how the mobile app syncs QSOs with the RadioLedger server.

## Automatic Sync

The mobile app syncs automatically whenever it has internet connectivity:

- When the app opens (foreground sync)
- Periodically in the background (when the OS allows it)
- Immediately after logging QSOs (if online)

A sync indicator in the header shows sync status.

## Manual Sync

Trigger a sync manually: **Settings → Sync → Sync Now**

This is useful after returning from a field operation to ensure everything is uploaded before closing the app.

## What Gets Synced

### Up (Mobile → Server)

- New QSOs logged offline
- QSO edits made while offline
- Deleted QSOs

### Down (Server → Mobile)

- QSO confirmations (LoTW, QRZ, eQSL)
- QSOs logged from other devices (web, desktop client)
- Logbook setting changes
- Callsign database updates

## Sync Status

View sync details: **Settings → Sync Status**

| Status | Description |
|--------|-------------|
| Synced | All QSOs up to date |
| Pending | QSOs waiting to upload |
| Conflict | QSO needs manual resolution |
| Error | Sync error (tap for details) |

## Offline Queue

QSOs logged offline are stored in the local pending queue. The queue count shows in the sync indicator. All pending QSOs upload when connectivity returns.

## Conflict Resolution

See [Offline Logging: Conflict Handling](offline-logging.md#conflict-handling).

## Background Sync

On iOS, background sync is subject to iOS's background task restrictions. Open the app periodically after field operations to ensure sync completes.

On Android, background sync is more reliable. You can configure the sync interval in Settings.

TODO: Document platform-specific background sync behavior.

## Related

- [Offline Logging](offline-logging.md)
- [POTA Activation](pota-activation.md)
- [SOTA Activation](sota-activation.md)
