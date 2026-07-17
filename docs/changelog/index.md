# Changelog

> RadioLedger release notes, newest first.

## How We Version

RadioLedger follows [Semantic Versioning](https://semver.org/):
- **MAJOR**: Breaking API changes or major architectural shifts
- **MINOR**: New features, backward-compatible
- **PATCH**: Bug fixes, security updates

---

## Unreleased

### Docs

- Corrected ADIF docs and examples to match canonical 3.1.7 export behavior.
- Updated digital-mode guidance to reflect current normalization, including `FT2` exporting as `MODE=MFSK` + `SUBMODE=FT2`.
- Removed outdated docs claims about a user-selectable ADIF export version where that option is not currently exposed.

## v0.2.0 — Comprehensive Feature Update

*Date: 2026-03-07*

Significant update adding global callsign databases, enhanced awards tracking, statistics analytics, and security hardening.

### Callsign Databases

- **Global Coverage**: Local cache of regulatory databases for 9 countries (USA, Canada, Australia, France, Mexico, Netherlands, UK, Germany, Japan).
- **River Workers**: Weekly/monthly background sync jobs for each source.
- **FCC Daily Sync**: Daily differential updates for the US FCC ULS.
- **German BNetzA Parser**: Specialized PDF parsing for German callsign data.
- **Supplemental Sources**: Integration with HamQTH and HamDB.org APIs for international lookups.

### Awards Tracking (#50)

- **New Award Types**: Added WAZ (Worked All Zones) and WPX (Worked All Prefixes) tracking.
- **POTA/SOTA Integration**: Enhanced activator/hunter tracking with Mountain Goat and Shack Sloth progress.
- **Background Refresh**: River-based `award_progress_refresh` worker with dirty-flag pattern for real-time updates.
- **Milestone Notifications**: In-app notifications when crossing award thresholds.
- **Interactive Maps**: Map visualizations for DXCC, WAS, and VUCC progress.

### Statistics and Analytics (#51)

- **Statistics Dashboard**: New frontend page (`web/src/pages/StatsPage.vue`) with **Chart.js** visualizations.
- **QSO Analytics**: Breakdown by band, mode, and time period via `/v1/stats/by-period`.
- **Operating Patterns**: Activity heatmap showing UTC hour distribution via `/v1/stats/activity-heatmap`.
- **Trend Charts**: QSO activity over time and countries worked over time.

### Admin and Operations (#76)

- **Admin Job Dashboard**: Visibility into River background jobs with manual retry/trigger buttons (`web/src/pages/AdminJobsPage.vue`).
- **Sync Overview**: Real-time status and record counts for all callsign sources.
- **Admin Permissions**: Restricted access controlled via `RADIOLEDGER_ADMIN_EMAILS`.

### Security Hardening

- **Credential Encryption (#46)**: AES-256-GCM encryption for service credentials with per-user key derivation.
- **Key Rotation**: CLI tool for master key rotation and credential re-encryption (`api/cmd/rotate-credentials-key/`).
- **Credential Verification**: Immediate test on save with `last_verified_at` status in UI.
- **Master Key Auto-gen**: Automatic generation of `RADIOLEDGER_MASTER_KEY` for self-hosted deployments.
- **SSE Security (#34)**: Short-lived, single-use stream tokens for secure EventSource connections.
- **Log Scrubbing**: Automatic removal of sensitive query parameters from application logs.

---

## v0.1.0 — Initial Release

*Date: 2026-02-28*

Initial working release of RadioLedger foundation.

### Core Features

- QSO logging with full ADIF field support.
- ADIF import (async, with progress tracking and error reporting).
- ADIF export (filtered, canonicalized for RadioLedger's current export pipeline).
- Multiple logbook support.
- DXCC entity auto-resolution from callsigns.
- PostgreSQL Row-Level Security for multi-user data isolation.
- OAuth2/PKCE authentication via Zitadel.
- LoTW sync (via desktop client).
- QRZ logbook sync.
- eQSL sync.
- ClubLog upload.
- POTA log upload.
- SOTA log upload.
- Awards tracking: DXCC, WAS, VUCC, POTA, SOTA.
- Statistics dashboard (v1).
- Self-hosting via Docker Compose.
- REST API with full OpenAPI 3.1 spec.

---

*This file is maintained by the RadioLedger team. For security advisories, see [GitHub Security Advisories](https://github.com/FtlC-ian/radioledger/security/advisories).*
