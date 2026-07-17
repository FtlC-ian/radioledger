# POTA Activation Guide (Mobile)

> Log a Parks on the Air activation from the field using the RadioLedger mobile app.

## Before You Leave Home

1. Open the app with internet access (sync the latest data)
2. Verify your POTA account is connected: **Settings → Connected Services → POTA**
3. Download the park reference (it's in the cached database, but confirm)

## Starting an Activation

At the park:

1. Tap **+ New Activation** on the home screen
2. Tap **POTA Activation**
3. Search for your park by reference number (e.g., `K-4566`) or name
4. The park info screen shows: name, state, distance from your GPS location
5. Tap **Start Activation**

TODO: Screenshot of activation start screen.

## Logging QSOs

The activation logging screen shows:

| Element | Description |
|---------|-------------|
| **Park name & reference** | Persistent header showing your active park |
| **QSO counter** | `7/10 QSOs` — progress toward valid activation (10 minimum) |
| **Callsign field** | Large, auto-capitalized |
| **Band buttons** | One-tap band selection (80m, 40m, 20m, 15m, 10m, 6m, 2m, 70cm) |
| **Mode buttons** | One-tap mode selection (SSB, CW, FT8, FM, AM, other) |
| **RST fields** | Quick-fill (defaults: 59/599) |
| **Log QSO button** | Large — works with gloves |
| **Previous QSOs** | Scrollable list below the entry form |

### Logging Steps

1. Enter the callsign (previous callsigns auto-complete from history)
2. Tap your band and mode
3. Optionally: adjust RST, add name/grid
4. Tap **Log QSO**

The QSO saves locally (no internet required). The QSO counter increments.

### P2P (Park-to-Park) QSOs

When you work another POTA activator:
1. Enable the **P2P** toggle
2. Enter their park reference (e.g., `K-1234`)
3. Log normally

P2P QSOs show a special icon in the QSO list and count toward your P2P tally.

## Spotting Yourself

Tap **Spot Me** to post your activation to the POTA spotting network:

| Field | Description |
|-------|-------------|
| Frequency | Auto-filled from last QSO or manual entry |
| Mode | Auto-filled |
| Comment | Optional note |

Requires internet connectivity. The spot appears on pota.app immediately.

## GPS Features

With location permission granted:

- **Grid square**: Auto-filled from GPS
- **Nearby parks**: Shows POTA parks within range (useful when you're near park boundaries)
- **Distance to park center**: Shown on the activation start screen

TODO: Document GPS accuracy requirements for park validation.

## Ending an Activation

Tap **End Activation** when done. The app shows:

- Total QSOs logged
- Unique callsigns worked
- Activation validity status (✓ Valid if ≥10 QSOs)
- P2P count

### Uploading to POTA

If you have internet connectivity, tap **Upload to POTA**. The app generates the POTA-required ADIF and uploads.

If offline, the upload is queued and completes automatically when connectivity returns.

## After the Activation

- QSOs sync to RadioLedger server (automatic when online)
- POTA award tracking updates
- POTA confirmation typically arrives within 24-48 hours

## Related

- [POTA Log Upload](../sync/pota.md)
- [Awards Tracking (POTA)](../user-guide/awards-tracking.md)
- [Offline Logging](offline-logging.md)
- [SOTA Activation Guide](sota-activation.md)
