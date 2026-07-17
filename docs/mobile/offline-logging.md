# Offline Logging

> Log QSOs in the field without internet connectivity — RadioLedger syncs when you're back online.

## How Offline Logging Works

The mobile app stores QSOs locally in an encrypted SQLite database. When you're offline:

- New QSOs are saved to the local database
- Callsign lookups use the cached local database
- All app features work normally

When connectivity returns, the app syncs your offline QSOs to the RadioLedger server automatically.

## Before You Go Into the Field

Prepare your device while you have connectivity:

1. **Open the app** — this triggers a sync and refreshes the callsign database
2. **Start an activation session** (if POTA/SOTA) — so the park/summit reference is set
3. **Check local storage** — Settings → Offline Database shows how many QSOs and how much data is cached

TODO: Screenshot of offline database status screen.

## Logging Offline

There's nothing special to do — just log QSOs as normal. The app detects when it's offline and saves locally.

An offline indicator appears in the app header when you have no connectivity.

## Sync When Back Online

When connectivity returns (driving back to town, summit descent, etc.):

1. The app detects the connection
2. Pending QSOs are uploaded to the RadioLedger server
3. Any server-side changes (confirmations, edits from web UI) sync down
4. A sync summary notification shows the result

You can also trigger manual sync: **Settings → Sync → Sync Now**

## Conflict Handling

If you edited a QSO both offline and on the web UI while offline:

- **Confirmation status**: Server always wins
- **Data fields**: The most recently edited version wins (field-level last-write-wins)
- **True conflicts** (same field edited on both sides): Flagged for manual review

TODO: Screenshot of conflict resolution UI.

## Offline Limitations

| Feature | Offline behavior |
|---------|-----------------|
| Logging QSOs | ✓ Works offline |
| Callsign lookup | ✓ Cached database |
| POTA/SOTA upload | ✗ Requires connectivity |
| LoTW sync | ✗ Requires connectivity |
| Award progress | ✓ Calculated from local data |
| Statistics | ✓ From local data |

## Storage Management

The local database grows as you log. In **Settings → Offline Database**:
- View current database size
- Configure how many historical QSOs to keep locally
- Clear old data (server copy is unaffected)

## Related

- [POTA Activation Guide](pota-activation.md)
- [SOTA Activation Guide](sota-activation.md)
- [Sync with Server](sync.md)
- [Mobile App Overview](index.md)
