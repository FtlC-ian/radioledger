# ADIF Field Mapping

> Complete mapping from ADIF field names to RadioLedger database columns.

RadioLedger stores commonly-used ADIF fields in dedicated typed columns. Uncommon or unrecognized fields are stored in the `extra` JSONB column — data is never lost.

## Column Mapping

| ADIF Field | DB Column | Type | Notes |
|-----------|-----------|------|-------|
| `CALL` | `callsign` | TEXT | Normalized to uppercase; stored as-is |
| `QSO_DATE` + `TIME_ON` | `datetime_on` | TIMESTAMPTZ | Merged to UTC timestamp |
| `QSO_DATE_OFF` + `TIME_OFF` | `datetime_off` | TIMESTAMPTZ | End of QSO |
| `BAND` | `band` | TEXT | Enumerated; see [Bands](bands-and-modes.md) |
| `FREQ` | `freq` | NUMERIC(10,6) | MHz |
| `FREQ_RX` | `freq_rx` | NUMERIC(10,6) | Split RX frequency |
| `MODE` | `mode` | TEXT | Enumerated; see [Modes](bands-and-modes.md) |
| `SUBMODE` | `submode` | TEXT | FT8, FT4, JS8, PSK31, etc. |
| `RST_SENT` | `rst_sent` | TEXT | As entered |
| `RST_RCVD` | `rst_rcvd` | TEXT | As entered |
| `NAME` | `name` | TEXT | Operator name |
| `QTH` | `qth` | TEXT | City/location |
| `GRIDSQUARE` | `gridsquare` | TEXT | Maidenhead locator |
| `DXCC` | `dxcc_entity_id` | INTEGER | ADIF DXCC number |
| `STATE` | `state` | TEXT | US state |
| `CNTY` | `county` | TEXT | US county |
| `CQZ` | `cq_zone` | SMALLINT | CQ zone 1–40 |
| `ITUZ` | `itu_zone` | SMALLINT | ITU zone |
| `CONT` | `continent` | TEXT | NA, EU, AS, etc. |
| `TX_PWR` | `tx_pwr` | NUMERIC(7,2) | Watts |
| `COMMENT` | `comment` | TEXT | Free text |
| `NOTES` | `notes` | TEXT | Longer notes |
| `IOTA` | `iota_ref` | TEXT | IOTA reference |
| `SOTA_REF` | `sota_ref` | TEXT | Worked station's summit |
| `MY_SOTA_REF` | `my_sota_ref` | TEXT | Activator's summit |
| `POTA_REF` | `pota_ref` | TEXT | Worked station's park |
| `MY_POTA_REF` | `my_pota_ref` | TEXT | Activator's park |
| `SIG` | `sig` | TEXT | Activity type |
| `SIG_INFO` | `sig_info` | TEXT | Activity reference |
| `MY_SIG` | `my_sig` | TEXT | My activity type |
| `MY_SIG_INFO` | `my_sig_info` | TEXT | My activity reference |
| `QSL_SENT` | `qsl_sent` | ENUM | Y/N/R/I/Q |
| `QSL_RCVD` | `qsl_rcvd` | ENUM | Y/N/R/I/Q |
| `QSL_VIA` | `qsl_via` | TEXT | QSL manager callsign |
| `QSLMSG` | `qsl_msg` | TEXT | Message on QSL card |
| `LOTW_QSL_SENT` | (sync_status) | — | In sync tracking table |
| `LOTW_QSL_RCVD` | (sync_status) | — | In sync tracking table |
| `EQSL_QSL_SENT` | (sync_status) | — | In sync tracking table |
| `EQSL_QSL_RCVD` | (sync_status) | — | In sync tracking table |
| `APP_*` | `extra` | JSONB | App-specific fields |
| (any unknown) | `extra` | JSONB | Never dropped |

TODO: Complete this table from SCHEMA.md once all columns are finalized.

## The `extra` JSONB Column

Any ADIF field without a dedicated column is stored in `extra`:

```json
{
  "IOTA": "NA-001",
  "APP_WSJT_BAND_RX": "20m",
  "MY_CITY": "Newington"
}
```

`extra` preserves exact ADIF field names and raw values. This enables semantic-lossless round-trips even for fields RadioLedger doesn't know about.

## LOTW and eQSL Status Fields

LoTW and eQSL sync status is stored in the `sync_status` table (not in `qsos`). On ADIF import, `LOTW_QSL_SENT` and `LOTW_QSL_RCVD` fields populate the sync_status table.

On ADIF export, these fields are reconstructed from sync_status.

## Related

- [ADIF.md](../ADIF.md) — full ADIF handling internals
- [ADIF Quick Reference (for developers)](../contributing/adif-reference.md)
- [Bands and Modes](bands-and-modes.md)
