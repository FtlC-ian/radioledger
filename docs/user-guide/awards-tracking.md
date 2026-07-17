# Awards Tracking

> Track your progress toward DXCC, WAS, VUCC, WAZ, WPX, POTA, SOTA, and other operating awards automatically.

RadioLedger tracks award progress in real time. Every QSO you log updates your counts automatically — no manual tallying required.

## Supported Awards

### DXCC (DX Century Club)

Track worked and confirmed entities on each band and mode.

| Metric | Description |
|--------|-------------|
| Entities worked | Unique DXCC entities with at least one QSO |
| Entities confirmed | Entities confirmed via LoTW, QSL card, or electronic confirmation |
| Band/mode breakdown | Progress per band and per mode (Mixed, Phone, CW, Digital) |
| Needed list | Entities you haven't worked yet |

**Entity resolution:** RadioLedger resolves DXCC entities from callsigns automatically. Prefix overrides (e.g., `VK9/W1AW`), maritime mobile, and aeronautical mobile callsigns are handled correctly. See [DXCC Entities](../reference/dxcc-entities.md) and [Callsign Parsing](../reference/callsign-parsing.md).

### WAS (Worked All States)

Track QSOs with all 50 US states.

| Metric | Description |
|--------|-------------|
| States worked | Unique US states with at least one QSO |
| States confirmed | States confirmed via LoTW or QSL |
| Band/mode breakdown | Per-band, per-mode progress |
| Missing states | Which states you still need |

### VUCC (VHF/UHF Century Club)

Track Maidenhead grid squares worked on VHF and above.

| Metric | Description |
|--------|-------------|
| Grids worked | Unique 4-character grid squares |
| Grids confirmed | Grids confirmed via LoTW or QSL |
| Band breakdown | Per-band grid counts |
| Grid map | Visual map of worked/confirmed grids |

### WAZ (Worked All Zones)

Track QSOs with all 40 CQ zones.

| Metric | Description |
|--------|-------------|
| Zones worked | Unique CQ zones with at least one QSO |
| Zones confirmed | Zones confirmed via LoTW or QSL |
| Band/mode breakdown | Progress per band and mode |
| Missing zones | Which zones you still need |

### WPX (Worked All Prefixes)

Track unique callsign prefixes worked.

| Metric | Description |
|--------|-------------|
| Prefixes worked | Total unique prefixes (e.g., K1, G4, 7J1) |
| Prefixes confirmed | Prefixes confirmed via LoTW or QSL |
| Milestone tracking | Progress toward WPX 300, 600, etc. |

Prefix extraction follows the standard ITU algorithm: letters before the first digit plus the first digit (e.g., `KI5BRG` → `KI5`, `G4ABC` → `G4`).

### POTA (Parks on the Air)

Track park activations (as activator) and hunted parks (as hunter).

| Metric | Description |
|--------|-------------|
| Parks activated | Unique parks where you've made 10+ QSOs |
| Parks attempted | Parks where you've logged QSOs but not yet 10 |
| Parks hunted | Unique parks you've contacted |
| Countries | Unique countries with POTA parks activated/hunted |

POTA requires specific ADIF fields (`POTA_REF`, `MY_POTA_REF`). See [POTA Log Upload](../sync/pota.md).

### SOTA (Summits on the Air)

Track summit activations (as activator) and chased summits (as chaser).

| Metric | Description |
|--------|-------------|
| Summits activated | Unique summits you've activated |
| Summits chased | Unique summits you've contacted |
| Points | SOTA activation and chaser points |
| Mountain Goat progress | Points toward Mountain Goat award (1,000 pts) |
| Shack Sloth progress | Points toward Shack Sloth award (1,000 chase pts) |

See [SOTA Log Upload](../sync/sota.md).

## How Award Tracking Works

Award progress is calculated from your logbook in the background using a **dirty-flag pattern**:

1. When you log or update a QSO, the affected award rows are marked as "dirty".
2. A background worker (`award_progress_refresh`) periodically sweeps dirty rows and recalculates totals.
3. Progress counters and maps update on the dashboard.

This architecture ensures the UI remains fast even with hundreds of thousands of QSOs, as heavy calculations happen asynchronously.

## Awards Dashboard

The Awards section (accessible from the left sidebar or **Awards** in the top nav) provides a tabbed interface for each award type.

### Visualizations

- **Interactive Maps**: View worked and confirmed entities/states/grids on a world map.
- **Progress Bars**: Visual tracking toward major milestones (e.g., DXCC 100, WAS 50).
- **Milestone Notifications**: RadioLedger notifies you when you cross significant award thresholds.

### Filtering

Filter your award progress by:
- **Band**: All-band or specific bands (160m through 70cm)
- **Mode**: Mixed, Phone, CW, or Digital
- **Date Range**: All time or specific years

## Related

- [QSL Management](qsl-management.md)
- [Statistics](statistics.md)
- [DXCC Entities Reference](../reference/dxcc-entities.md)
- [Callsign Parsing](../reference/callsign-parsing.md)
- [Maidenhead Grids](../reference/maidenhead-grids.md)
- [POTA Log Upload](../sync/pota.md)
- [SOTA Log Upload](../sync/sota.md)
