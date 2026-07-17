# Mobile App

## Purpose

Cross-platform mobile app primarily for field operations: POTA activations, SOTA summits, portable ops. Must work offline with sync-when-connected.

## Core Use Cases

1. **POTA/SOTA Activation Logging**: Quick QSO entry in the field
2. **Log Review**: Browse and search your log on the go
3. **Callsign Lookup**: Quick lookup of callsign info
4. **Spot Yourself**: Post to POTA/SOTA spotting networks
5. **Sync**: Upload field QSOs to server when back online

## Offline-First Architecture

This is non-negotiable. Cell coverage at parks and summits is unreliable.

```
┌─────────────────────────────────┐
│         Mobile App              │
│                                 │
│  ┌─────────────┐               │
│  │ Local SQLite│ ◄── QSO entry │
│  │ Database    │                │
│  └──────┬──────┘               │
│         │                       │
│    ┌────▼────┐                  │
│    │  Sync   │ ◄── When online  │
│    │  Engine │                  │
│    └────┬────┘                  │
│         │                       │
└─────────┼───────────────────────┘
          │
          ▼
    ┌──────────┐
    │ RadioLedger   │
    │ Server   │
    └──────────┘
```

### Local Storage
- SQLite database with same schema concepts as server (simplified)
- QSOs created offline get UUIDs for conflict-free merge
- Full callsign lookup database cached locally (downloadable update)
- DXCC/grid reference data cached

### Sync Strategy
- **Push**: New local QSOs → server (on connectivity)
- **Pull**: Server updates → local (on app open or manual refresh)
- **Conflict**: Server wins for confirmations, local wins for user edits, flag true conflicts

## Screens

### Quick Log (Primary)
- Optimized for speed: callsign → band → mode → exchange → save
- Large touch targets for field use with gloves
- Band/mode buttons (not dropdowns) for one-tap selection
- Auto-increment serial number for contests/activations
- Previous QSO list scrolling below entry form
- Running activation count (e.g., "7/10 QSOs for valid POTA activation")

### Log Browser
- Scrollable QSO list with search/filter
- Filter by: date range, band, mode, logbook, activity
- Tap to view/edit QSO detail

### Callsign Lookup
- Search by callsign
- Display: name, location, grid, QRZ photo (if available)
- Previous QSO history with this station
- Works offline with cached database

### Spot / Self-Spot
- Post to POTA spots (park ref, frequency, mode, comment)
- Post to SOTA spots
- View recent spots for your area/band

### Activation Mode
- Set operating context: park/summit ref, callsign, power
- Persistent header showing activation status
- QSO counter with validation thresholds
- Timer for activation duration
- One-tap ADIF export of activation QSOs

### Settings / Sync
- Server connection config (URL, auth)
- Offline database management
- Sync status and history
- Default operating parameters

## Tech Stack Options

### Flutter (Leaning this way)
- Single codebase for iOS and Android
- Good offline/SQLite support (drift/sqflite)
- Decent performance for this use case
- Validate Flutter prototype ergonomics before committing to the first production release.

### React Native
- JavaScript ecosystem, wider hiring pool
- Expo simplifies builds
- SQLite support via community packages

### Native (Swift + Kotlin)
- Best UX per platform
- 2x code and maintenance
- Only worth it if we need deep platform integration

**Recommendation: Flutter or React Native.** Leaning Flutter for performance and SQLite story.

## Key UX Principles

- **Speed over beauty**: Logging a QSO in the field should take <5 seconds
- **Fat fingers**: Big buttons, generous touch targets, landscape support
- **Battery conscious**: Minimize GPS/network usage, dark mode default
- **Glove-friendly**: Usable with light gloves (capacitive tips)
- **Sunlight readable**: High contrast theme option

## POTA-Specific Features

- Park lookup by reference or name
- Distance/bearing to park from current GPS location
- Activation checklist (equipment, frequencies, spot schedule)
- Auto-detect nearby parks from GPS
- P2P (park-to-park) QSO flagging
- Export activation ADIF in POTA-required format

## SOTA-Specific Features

- Summit lookup and info (elevation, points, activation count)
- GPS altitude for summit verification
- Chase vs activation mode
- S2S (summit-to-summit) flagging

## Open Questions

- [ ] Flutter vs React Native decision
- [ ] Offline callsign DB size — full worldwide is ~2GB, need to decide on subset strategy
- [ ] GPS usage policy — continuous for POTA/SOTA or manual entry?
- [ ] Bluetooth integration for external loggers/keyers?
