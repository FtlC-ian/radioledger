# ADIF Handling

## Overview

ADIF (Amateur Data Interchange Format) is the standard for ham radio log exchange. It's a tag-based format that's been around since the 90s and it shows. We need to handle it perfectly because it's how data moves in and out of the ecosystem.

Canonical export target: ADIF 3.1.7 (https://adif.org/)

RadioLedger still imports older ADIF 2.x and 3.x files, plus a broad set of legacy mode aliases that common logging programs emit.

## Format Basics

```
ADIF file example:

<ADIF_VER:5>3.1.7
<PROGRAMID:11>RadioLedger
<PROGRAMVERSION:5>0.1.0
<EOH>

<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <RST_SENT:2>59 <RST_RCVD:2>59 <EOR>
<CALL:4>K5XX <BAND:3>40m <MODE:3>FT8 <QSO_DATE:8>20260228 <TIME_ON:6>160000 <FREQ:8>7.074500 <EOR>
<CALL:5>K1ABC <BAND:2>6m <MODE:4>MFSK <SUBMODE:3>FT2 <QSO_DATE:8>20260228 <TIME_ON:6>160015 <EOR>
```

Each field: `<FIELDNAME:LENGTH[:TYPE]>VALUE`

## Import Pipeline

1. **Parse**: Handle both ADI (text) and ADX (XML) formats
2. **Validate**: Check field lengths, date formats, enum values against ADIF spec
3. **Normalize**: Uppercase callsigns, standardize band/mode names, parse dates to UTC
4. **Deduplicate**: Check against existing QSOs using composite key match
5. **Map**: Known fields → typed columns, unknown → JSONB `extra`
6. **Store**: Bulk insert with conflict handling

### Validation Rules
- QSO_DATE must be valid YYYYMMDD
- TIME_ON must be valid HHMMSS or HHMM (UTC assumed unless offset specified)
- BAND must be a recognized amateur band
- MODE must be a recognized mode per ADIF spec
- FREQ must fall within the specified BAND range (warn if mismatch)
- DXCC entity numbers must exist in our reference table
- Callsign format: loose validation (prefixes vary wildly by country)

### Conflict Resolution on Import
- Exact duplicate (same composite key): skip or update based on user preference
- Near duplicate (within time window): flag for review
- Field conflicts on update: user chooses per-import (keep existing / overwrite / merge)

## Export Pipeline

1. **Query**: Filtered QSO set (all, date range, logbook, etc.)
2. **Map**: Typed columns → ADIF field names
3. **Merge**: Include JSONB `extra` fields
4. **Format**: Standard ADIF header + records
5. **Deliver**: File download or direct push to sync service

### Export Behavior
- Full log or filtered subset
- Canonical ADIF 3.1.7 header and field ordering
- Known MODE/SUBMODE pairs rewritten to canonical export values
- Unknown long-tail fields preserved from JSONB `extra`
- Cabrillo export for contest submissions is separate

Current implementation note: RadioLedger does not currently expose a user-selectable ADIF version or an include/exclude-extra toggle on the main export path. Docs should not claim those options yet.

## Known ADIF Quirks to Handle

- **Date/time split**: QSO_DATE and TIME_ON are separate fields (not a timestamp)
- **Multiple QSO dates**: QSO_DATE vs QSO_DATE_OFF (contact can span midnight)
- **Band vs Frequency**: Both may be present, may conflict. Frequency is authoritative.
- **Mode/Submode**: Some programs put the submode in MODE directly. RadioLedger's import-alias policy: **every ADIF 3.1.7 submode value is accepted as a bare MODE on import** and normalized to the canonical MODE + SUBMODE pair. This is auto-derived from the `CanonicalADIFSubmodes` map in `pkg/adif/mode.go` — no manual alias entry is needed for new submodes. Examples: `FT4` → `MODE=MFSK, SUBMODE=FT4`; `FST4W` → `MODE=MFSK, SUBMODE=FST4W`; `MFSK31` → `MODE=MFSK, SUBMODE=MFSK31`; `SCAMP_FAST` → `MODE=FSK, SUBMODE=SCAMP_FAST`. Export always uses canonical pairs: `FT8` exports as `MODE=FT8`; MFSK-family modes export as `MODE=MFSK` + `SUBMODE`; `DMR` exports as `MODE=DIGITALVOICE` + `SUBMODE=DMR`; `PACKET` exports as `MODE=PKT`.
- **Character encoding**: Officially ASCII, but real-world files have Latin-1, UTF-8, Windows-1252. Detect and handle.
- **Line endings**: CR, LF, CRLF — all must work
- **Case sensitivity**: Field names are case-insensitive per spec
- **Intl callsigns**: Extended characters in some newer callsign formats
- **POTA multi-park**: POTA_REF can contain comma-separated park references (e.g., "K-1234,K-5678")
- **POTA export requirements**: upload files for activators must include `MY_SIG=POTA` and `MY_SIG_INFO=<park ref>` on every record; `STATION_CALLSIGN` and `MY_GRIDSQUARE` are required for submission readiness.

## ADIF Fields We Explicitly Map

(See SCHEMA.md for the full column list — these are the ~40 fields we promote to typed columns. Everything else goes to JSONB.)

## Testing Strategy

- Maintain a corpus of real-world ADIF files from various programs
- Round-trip test: import → export → diff = zero changes
- Fuzz testing with malformed ADIF
- Performance: handle 100k+ QSO imports without timeout
