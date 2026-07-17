# Maidenhead Grid Squares

> The Maidenhead Locator System used by RadioLedger for geographic coordinates.

## What Are Maidenhead Grids?

The Maidenhead Locator System (also called QRA locator or grid squares) divides the world into a hierarchical grid for radio communication purposes. It's more compact than latitude/longitude and commonly used in:

- VHF/UHF contests (VUCC award)
- Digital modes (FT8 automatically exchanges 4-char grids)
- SOTA and POTA activations
- Satellite operation

## Grid Square Format

Grids use alternating letter-number pairs:

| Precision | Format | Size | Example |
|-----------|--------|------|---------|
| Field (2 char) | AA | 20° lon × 10° lat | `FN` |
| Square (4 char) | AA00 | 2° lon × 1° lat | `FN42` |
| Subsquare (6 char) | AA00aa | 5' lon × 2.5' lat | `FN42aa` |
| Extended (8 char) | AA00aa00 | 30" lon × 15" lat | `FN42aa33` |

RadioLedger stores up to 6-character grids in the main `gridsquare` column. 8-character grids are supported via the `extra` field.

## Grid Square Examples

| Location | Grid square |
|----------|------------|
| ARRL HQ, Newington CT | FN31pr |
| London, UK | IO91wm |
| Tokyo, Japan | PM95tp |
| Sydney, Australia | QF56oa |
| São Paulo, Brazil | GG66mt |

## How RadioLedger Uses Grids

### Storage

- Stored in `qsos.gridsquare` (worked station's grid)
- `qsos.my_gridsquare` stores the operator's own grid at time of QSO
- Grids are normalized to uppercase before storage

### PostGIS Integration

RadioLedger converts grid squares to PostGIS geometry for:

- Distance calculation (km/miles to worked station)
- VUCC award tracking (grid count and map visualization)
- POTA/SOTA park proximity detection

See [POSTGIS.md](../POSTGIS.md) for spatial query details.

### FT8/FT4 Grid Squares

WSJT-X exchanges 4-character grid squares (field + square) in FT8/FT4 QSOs. These are imported automatically from WSJT-X UDP messages.

### VUCC Award

The VUCC award tracks worked and confirmed 4-character grid squares on VHF and above. RadioLedger's VUCC dashboard shows a grid map of your worked/confirmed squares.

## Converting Grid to Lat/Long

RadioLedger converts Maidenhead grids to lat/long internally using the PostGIS-based `maidenhead_to_point()` function.

For reference, the center of a grid square:
- `FN42` → approximately 42.5°N, 72°W

TODO: Document the conversion formula or link to the implementation in pkg/maidenhead/.

## Related

- [POSTGIS.md](../POSTGIS.md) — spatial features
- [VUCC Award Tracking](../user-guide/awards-tracking.md#vucc)
- [Statistics Dashboard](../user-guide/statistics.md)
