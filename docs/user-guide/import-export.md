# Import and Export

> Import ADIF files from other loggers and export your log in ADIF format.

## ADIF Overview

ADIF (Amateur Data Interchange Format) is the standard exchange format for ham radio logs. RadioLedger imports older ADIF 2.x and 3.x files, and its canonical export format targets ADIF 3.1.7.

See also: [Getting Started: Import Existing Log](../getting-started/import-existing-log.md) for the quick import walkthrough.

## Importing ADIF

### Starting an Import

1. Open your logbook
2. Click **Import → ADIF**
3. Select your `.adi` or `.adx` file (no size limit; large files process in background)
4. Configure import options (see below)
5. Click **Start Import**

### Import Options

| Option | Default | Description |
|--------|---------|-------------|
| **Duplicate handling** | Skip | What to do when a matching QSO already exists: Skip, Overwrite, or Import Both |
| **Duplicate window** | 30 seconds | Time window for considering QSOs as duplicates |
| **Callsign normalization** | Uppercase | Normalize callsigns to uppercase on import |
| **Timestamp handling** | Assume UTC | How to interpret date/time fields. Options: Assume UTC, Use source timezone if present, Ask |

### Import Progress

Large imports run asynchronously. The import page shows:
- Records processed so far
- Records imported, skipped, and errored
- Estimated time remaining
- Real-time error log

You can close the page and return to check progress. The import continues regardless.

### Import Errors and Warnings

After import, a summary shows:
- **Imported**: QSOs successfully added to your logbook
- **Skipped**: Duplicates (not counted as errors)
- **Warnings**: QSOs imported but with data issues (e.g., unrecognized ADIF fields stored in `extra`)
- **Errors**: QSOs that could not be imported at all

Download the full error report as CSV or JSON for detailed record-by-record analysis.

### What Happens to Unknown ADIF Fields?

Any ADIF field RadioLedger doesn't have a dedicated column for is stored in the `extra` JSONB column. Your data is never lost. Future versions may promote common fields to dedicated columns.

See [ADIF Field Mapping](../reference/adif-field-mapping.md) for the complete field-to-column mapping.

## Exporting ADIF

### Full Logbook Export

1. Open your logbook
2. Click **Export → ADIF**
3. Review any filters you want applied
4. Click **Download**

### Filtered Export

Apply filters first (see [Search and Filter](search-and-filter.md)), then click **Export → ADIF (filtered view)**. Only matching QSOs are exported.

### Export Defaults

| Behavior | Current value | Description |
|--------|---------|-------------|
| **ADIF version** | 3.1.7 | Canonical export header emitted by RadioLedger |
| **Extra fields** | Included | Long-tail ADIF fields stored in `extra` are preserved on export |
| **Timestamp format** | ADIF standard | UTC date + time split into `QSO_DATE` and `TIME_ON` |
| **Callsign format** | Uppercase | Uppercase (standard) |

### Round-Trip Fidelity

RadioLedger is designed to preserve **semantic-lossless** round-trips for supported ADIF data: if you import an ADIF file and export it again, all meaningful data should be preserved. Byte-identical output is not guaranteed because export uses canonical field ordering, header metadata, and recognized mode normalization.

Examples of current canonical export behavior:
- `FT8` exports as `MODE=FT8`
- `FT4`, `FT2`, `JS8`, and `Q65` export as `MODE=MFSK` plus `SUBMODE`
- `DMR` exports as `MODE=DIGITALVOICE` plus `SUBMODE=DMR`
- `PACKET` exports as `MODE=PKT`

### Export for Specific Services

Some services require specific ADIF fields or formats:

- **LoTW**: Handled by the desktop client. See [LoTW Setup](../sync/lotw.md).
- **POTA**: See [POTA Log Upload](../sync/pota.md) for field requirements.
- **SOTA**: See [SOTA Log Upload](../sync/sota.md) (uses CSV format, not ADIF).
- **Cabrillo** (contest): See [Cabrillo Export](../contest/cabrillo-export.md).

### API Export

Export via the API: `GET /v1/logbooks/{uuid}/export?format=adif`

Current implementation note: the export endpoint always emits canonical ADIF 3.1.7 output. Older docs mentioned a version picker, but that is not currently exposed.

See [Import/Export API](../api/endpoints/import-export.md).

## Related

- [Getting Started: Import Existing Log](../getting-started/import-existing-log.md)
- [ADIF Field Mapping Reference](../reference/adif-field-mapping.md)
- [ADIF Format Reference (for developers)](../contributing/adif-reference.md)
- [API: Import/Export Endpoints](../api/endpoints/import-export.md)
- [Cabrillo Export (Contest)](../contest/cabrillo-export.md)
