# Import Your Existing Log

> Upload an ADIF file from your current logging software and bring all your past QSOs into RadioLedger.

RadioLedger supports the ADIF (Amateur Data Interchange Format) standard used by virtually every ham radio logger. If your current software can export ADIF, you can import it here.

## Supported Formats

- **ADIF 3.x** (`.adi` or `.adx`) — full support
- **ADIF 2.x** — supported with automatic field mapping
- **Any ADIF-compatible export** — tested with WSJT-X, HRD Logbook, Log4OM, N1MM+, MacLoggerDX, DXKeeper, Logger32, and more

## Before You Import

1. Export an ADIF file from your current logger (see software-specific instructions below)
2. Make sure the file includes at minimum: callsign, band or frequency, mode, and date/time

**Large logs are fine.** RadioLedger processes imports asynchronously — a 30-year log with 100,000 QSOs will import in the background while you continue using the app.

## Step 1: Export ADIF from Your Current Logger

### WSJT-X
`File → Export Log (ADIF)` — exports all logged QSOs as a `.adi` file.

### HRD Logbook
`File → Export → ADIF` — choose "All Records" for a full export.

### Log4OM
`Tools → Export → ADIF Export` — configure field selection if needed.

### N1MM+
`File → Export → ADIF Export` — per-logbook export.

### MacLoggerDX
`File → Export → ADIF...`

### DXKeeper
`QSL → Export → Entire Log to an ADIF file`

TODO: Verify these export paths are current for each logger's latest version.

## Step 2: Upload the ADIF File

1. Open your logbook in RadioLedger
2. Click **Import → ADIF**
3. Select your `.adi` or `.adx` file
4. Click **Start Import**

The API returns immediately with a job ID. You can monitor progress on the import status page.

## Step 3: Monitor Import Progress

The import status page shows real-time progress:

```
Importing: 49,953 of 50,000 records
  ✓ 49,906 imported
  ⚠   47 skipped (see details)
  ✗    0 errors
```

Import continues in the background — you can close the tab and come back.

## Step 4: Review Import Results

TODO: Describe the import results UI — link to downloadable error report.

### What Gets Skipped?

RadioLedger skips records when:
- **Duplicate detected** — a QSO with the same callsign, band, mode, and datetime already exists in your logbook. The duplicate strategy is configurable.
- **Unparseable record** — a record is so malformed that no fields can be recovered. The raw data is logged in the error report.

### What Gets Imported Even If Partially Malformed?

RadioLedger imports the record and stores valid fields, logging a warning for unrecognized or invalid fields. A 30-year-old DOS-era log with quirks will still import successfully.

### ADIF Fields We Don't Recognize

Unknown ADIF fields are stored in a `extra` JSONB column — your data is never lost. Future RadioLedger versions may add support for additional fields.

## Deduplication Behavior

By default, RadioLedger uses a composite key of `(callsign, band, mode, datetime ± 30s)` to detect duplicates. This handles minor timestamp rounding differences between loggers.

TODO: Document deduplication options (strict / relaxed / none) and how to configure them.

## After Import

- Your QSOs appear in the logbook immediately (imported in background, visible as they arrive)
- DXCC entities are auto-resolved from callsigns
- Awards progress updates automatically
- QSOs are queued for sync to LoTW, QRZ, etc. (if connected)

## Troubleshooting

**Import stuck?** Check the API logs. The job runs in the background — if the container restarted during import, re-upload to resume.

**All QSOs showing as duplicates?** Your logbook may already have an import. Check if you imported twice, or adjust the deduplication sensitivity.

**DXCC entity wrong?** RadioLedger resolves from the callsign prefix. If the callsign used a portable prefix (e.g., `VK9/W1AW`), check [callsign parsing](../reference/callsign-parsing.md).

TODO: Add more common import troubleshooting scenarios.

## Related

- [ADIF Import/Export (User Guide)](../user-guide/import-export.md)
- [ADIF Field Mapping Reference](../reference/adif-field-mapping.md)
- [ADIF Format Reference (for developers)](../contributing/adif-reference.md)
- [API: Import Endpoint](../api/endpoints/import-export.md)
