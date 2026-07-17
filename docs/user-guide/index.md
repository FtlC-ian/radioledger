# User Guide

> Complete reference for RadioLedger's features and daily use.

This section covers everything you can do in RadioLedger once you're set up. For initial setup, see the [Getting Started](../getting-started/index.md) guide.

## In This Section

| Topic | Description |
|-------|-------------|
| [logging-qsos.md](logging-qsos.md) | Manual QSO entry, fields, editing, and deleting |
| [logbooks.md](logbooks.md) | Creating and managing multiple logbooks |
| [search-and-filter.md](search-and-filter.md) | Finding QSOs by callsign, band, date, DXCC, and more |
| [import-export.md](import-export.md) | ADIF import and export in depth |
| [awards-tracking.md](awards-tracking.md) | DXCC, WAS, VUCC, POTA, and SOTA progress |
| [statistics.md](statistics.md) | Stats dashboard, band/mode breakdowns, maps |
| [qsl-management.md](qsl-management.md) | QSL tracking, bureau workflow, paper QSL batches |
| [settings.md](settings.md) | Account, logbook, and display settings |

## Key Concepts

### Logbooks

A logbook is a named collection of QSOs. You might have:
- A main personal logbook (your home callsign)
- A club station logbook
- A portable/POTA logbook
- A contest logbook

Each logbook tracks QSOs, sync status, and awards progress independently.

### QSO Fields

RadioLedger stores all standard ADIF fields. Key fields:

- **Callsign** — stored exactly as logged (including `/P`, `/MM`, etc.)
- **Date/Time** — always UTC; never stored as local time
- **Band** — derived from frequency when possible
- **Mode** — including digital submodes (FT8, JS8, etc.)
- **DXCC Entity** — auto-resolved from the callsign

### Operator vs. Station Callsign

RadioLedger distinguishes between the **operator** (person at the controls) and the **station callsign** (the callsign used for the QSO). This matters for:
- Club stations where multiple operators use one callsign
- Multi-op contest operations
- POTA activations where the operator and station callsign differ

For most single-operator home stations, this distinction is invisible — it works the way you'd expect.

## Getting Help

- [FAQ](../faq.md)
- [Glossary](../reference/glossary.md)
- GitHub Issues for bug reports

## Related

- [Getting Started](../getting-started/index.md)
- [Sync Services](../sync/index.md)
- [Desktop Client](../desktop/index.md)
- [Mobile App](../mobile/index.md)
