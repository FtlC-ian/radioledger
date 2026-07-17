# ADIF Quick Reference for Developers

> ADIF format essentials for RadioLedger contributors.

## What is ADIF?

ADIF (Amateur Data Interchange Format) is the standard file format for exchanging ham radio log data between applications. It's used by virtually all logging software and all major QSL services (LoTW, QRZ, eQSL, ClubLog).

RadioLedger imports and exports ADIF. Understanding the format is essential for ADIF parser and database work.

## File Format (.adi)

ADIF's native text format uses field descriptors:

```
<FIELD_NAME:LENGTH>VALUE
```

Example QSO record:
```
<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:4>1432 <RST_SENT:2>59 <RST_RCVD:2>59 <EOR>
```

Fields:
- `<FIELD_NAME:LENGTH>` — field name and value length
- `VALUE` — the actual data
- `<EOR>` — End of Record marker (required)
- `<EOH>` — End of Header marker (in file header)

## Key Fields

| ADIF Field | Type | Description | Our column |
|-----------|------|-------------|-----------|
| `CALL` | String | Worked callsign | `callsign` |
| `QSO_DATE` | Date (YYYYMMDD) | UTC date | `datetime_on` |
| `TIME_ON` | Time (HHMM or HHMMSS) | UTC time start | `datetime_on` |
| `TIME_OFF` | Time | UTC time end | `datetime_off` |
| `BAND` | Enumeration | Amateur band | `band` |
| `FREQ` | Number | Frequency in MHz | `freq` |
| `MODE` | Enumeration | Operating mode | `mode` |
| `SUBMODE` | String | Mode subtype (FT8, etc.) | `submode` |
| `RST_SENT` | String | Signal report sent | `rst_sent` |
| `RST_RCVD` | String | Signal report received | `rst_rcvd` |
| `GRIDSQUARE` | String | Maidenhead grid | `gridsquare` |
| `DXCC` | Integer | DXCC entity number | `dxcc_entity_id` |
| `NAME` | String | Operator name | `name` |
| `QTH` | String | City/location | `qth` |
| `STATE` | String | US state | `state` |
| `CQZ` | Integer | CQ zone | `cq_zone` |
| `ITUZ` | Integer | ITU zone | `itu_zone` |
| `TX_PWR` | Number | Transmit power (watts) | `tx_pwr` |
| `COMMENT` | String | Free-form notes | `comment` |
| `IOTA` | String | IOTA reference | `iota_ref` |
| `SOTA_REF` | String | SOTA summit reference | `sota_ref` |
| `POTA_REF` | String | POTA park reference | `pota_ref` |
| `MY_SOTA_REF` | String | Activator summit ref | `my_sota_ref` |
| `MY_POTA_REF` | String | Activator park ref | `my_pota_ref` |
| `SIG` | String | Activity reference type | (in extra) |
| `SIG_INFO` | String | Activity reference value | (in extra) |
| `MY_SIG` | String | My activity reference type | `my_sig` |
| `MY_SIG_INFO` | String | My activity reference value | `my_sig_info` |
| `QSL_SENT` | Enumeration | QSL card sent status | `qsl_sent` |
| `QSL_RCVD` | Enumeration | QSL card received status | `qsl_rcvd` |
| `LOTW_QSL_SENT` | Enumeration | LoTW upload status | (in sync_status) |
| `LOTW_QSL_RCVD` | Enumeration | LoTW confirmation | (in sync_status) |

See [ADIF Field Mapping](../reference/adif-field-mapping.md) for the complete mapping.

## ADIF Parsing Rules

From [AGENTS.md](../../AGENTS.md):

1. Parse all fields — unknown ones go to `extra` JSONB
2. Normalize callsigns to uppercase
3. Validate dates, frequencies, band/mode combinations
4. Never silently drop data
5. Store original ADIF field names in `extra` for fields we normalize
6. Handle malformed records gracefully — partial import beats total failure

## Band/Frequency Mapping

| Band | Frequency range (MHz) |
|------|--------------------|
| 160m | 1.8–2.0 |
| 80m | 3.5–4.0 |
| 60m | 5.3–5.4 |
| 40m | 7.0–7.3 |
| 30m | 10.1–10.15 |
| 20m | 14.0–14.35 |
| 17m | 18.068–18.168 |
| 15m | 21.0–21.45 |
| 12m | 24.89–24.99 |
| 10m | 28.0–29.7 |

See [Bands and Modes](../reference/bands-and-modes.md) for the full list.

## Mode/Submode

RadioLedger's import-alias policy: **any ADIF 3.1.7 submode value is accepted as a bare MODE on import and normalized to the canonical MODE + SUBMODE pair.** For example, an incoming record with `MODE=MFSK31` (no SUBMODE) is stored as `MODE=MFSK, SUBMODE=MFSK31`. Export always uses canonical pairs.

This policy is auto-enforced: every entry in `CanonicalADIFSubmodes` automatically becomes a valid bare-MODE import alias. Adding a new submode to that map is sufficient to accept it on import — no manual `ModeAliases` entry is needed.

Common ADIF mode + submode combinations RadioLedger currently normalizes for export:

| ADIF MODE | ADIF SUBMODE | Display name |
|-----------|-------------|-------------|
| `SSB` | | Phone (SSB) |
| `SSB` | `USB` | USB |
| `SSB` | `LSB` | LSB |
| `CW` | | CW |
| `FT8` | | FT8 |
| `MFSK` | `FT4` | FT4 |
| `MFSK` | `FT2` | FT2 |
| `MFSK` | `JS8` | JS8Call |
| `MFSK` | `Q65` | Q65 |
| `MFSK` | `FST4` | FST4 |
| `MFSK` | `FST4W` | FST4W |
| `MFSK` | `FSQCALL` | FSQCALL |
| `MFSK` | `JTMS` | JTMS |
| `MFSK` | `MFSK4`–`MFSK128L` | Fldigi MFSK variants |
| `PSK` | `PSK31` | PSK31 |
| `DIGITALVOICE` | `DMR` | DMR |
| `DIGITALVOICE` | `FREEDV` | FreeDV |
| `DYNAMIC` | `VARA HF` | VARA HF |
| `FSK` | `SCAMP_FAST` | SCAMP Fast |
| `OFDM` | `RIBBIT_PIX` | Ribbit (image) |
| `PKT` | | Packet |
| `RTTY` | | RTTY |
| `FM` | | FM |
| `AM` | | AM |

All ADIF 3.1.7 submodes listed in the table above (and any others in `CanonicalADIFSubmodes`) are accepted as bare MODE values on import. For example, `MODE=MFSK31` and `MODE=SCAMP_FAST` are valid import aliases that normalize to their canonical pairs.

## ADIF Specification

RadioLedger's canonical export target is ADIF 3.1.7. The official ADIF spec lives at [adif.org](https://adif.org).

## Related

- [ADIF Field Mapping Reference](../reference/adif-field-mapping.md)
- [ADIF.md](../ADIF.md) — full RadioLedger ADIF handling document
- [Import/Export](../user-guide/import-export.md)
