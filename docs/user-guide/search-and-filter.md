# Search and Filter

> Find QSOs quickly by callsign, date range, band, mode, DXCC entity, and more.

## Quick Search

Press `/` from any logbook view to focus the search bar. Type a callsign (or partial callsign) to filter immediately.

TODO: Screenshot of search bar with results.

## Filter Options

The filter panel (click **Filters** or press `F`) lets you combine multiple criteria:

| Filter | Description | Example |
|--------|-------------|---------|
| **Callsign** | Exact or prefix match | `W1AW`, `W1*` |
| **Date range** | UTC date range | `2025-01-01` to `2025-12-31` |
| **Band** | One or more bands | `20m`, `40m` |
| **Mode** | One or more modes | `FT8`, `SSB` |
| **DXCC entity** | By DXCC entity name or prefix | `Japan`, `JA` |
| **DXCC entity number** | By ADIF DXCC number | `339` |
| **Continent** | NA, EU, AS, AF, OC, SA, AN | `EU` |
| **Grid square** | 2, 4, or 6 char prefix | `FN`, `FN42` |
| **State** | US state (for WAS) | `CA` |
| **CQ zone** | CQ zone number | `14` |
| **QSL status** | Sent / received / confirmed by service | LoTW confirmed |
| **Awards** | Filter QSOs that count for specific awards | DXCC new entity |
| **Comment** | Full-text search in comments | `rare DX` |
| **Logbook** | Cross-logbook search | all logbooks |

Filters combine with AND logic. Selecting 20m AND FT8 returns QSOs on 20m FT8 only.

## Saved Searches

Click **Save Filter** to bookmark a filter set. Saved searches appear in the left sidebar.

TODO: Document saved search management (rename, delete).

## Sorting

Click any column header to sort. Default sort is by date/time descending (newest first).

| Column | Sortable |
|--------|----------|
| Date/Time | ✓ |
| Callsign | ✓ |
| Band | ✓ |
| Mode | ✓ |
| DXCC entity | ✓ |
| RST | ✗ |

## URL-Based Filters

Filters are reflected in the URL, so you can bookmark or share specific views:

```
/logbooks/abc123/qsos?band=20m&mode=FT8&dxcc=339
```

TODO: Document full query string parameter reference.

## API Search

The search API supports the same filters programmatically. See [Search API](../api/endpoints/search.md).

## Performance Notes

RadioLedger is designed for large logs (50,000+ QSOs). All filter operations use indexed database queries. Search results page instantly even on large logbooks.

## Related

- [Logging QSOs](logging-qsos.md)
- [Awards Tracking](awards-tracking.md)
- [API: Search Endpoint](../api/endpoints/search.md)
- [DXCC Entities Reference](../reference/dxcc-entities.md)
