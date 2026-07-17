# Bands and Modes

> All amateur radio bands and modes supported by RadioLedger.

## HF Bands

| Band | Frequency Range (MHz) | ADIF name |
|------|--------------------|-----------|
| 160m | 1.800–2.000 | 160m |
| 80m | 3.500–4.000 | 80m |
| 60m | 5.250–5.450 | 60m |
| 40m | 7.000–7.300 | 40m |
| 30m | 10.100–10.150 | 30m |
| 20m | 14.000–14.350 | 20m |
| 17m | 18.068–18.168 | 17m |
| 15m | 21.000–21.450 | 15m |
| 12m | 24.890–24.990 | 12m |
| 10m | 28.000–29.700 | 10m |

## VHF/UHF and Microwave Bands

| Band | Frequency Range | ADIF name |
|------|----------------|-----------|
| 6m | 50–54 MHz | 6m |
| 4m | 70–70.5 MHz | 4m |
| 2m | 144–148 MHz | 2m |
| 1.25m | 222–225 MHz | 1.25m |
| 70cm | 420–450 MHz | 70cm |
| 33cm | 902–928 MHz | 33cm |
| 23cm | 1240–1300 MHz | 23cm |
| 13cm | 2300–2450 MHz | 13cm |
| 9cm | 3300–3500 MHz | 9cm |
| 6cm | 5650–5925 MHz | 6cm |
| 3cm | 10000–10500 MHz | 3cm |
| 1.25cm | 24000–24250 MHz | 1.25cm |
| 6mm | 47000–47200 MHz | 6mm |
| 4mm | 75500–81000 MHz | 4mm |
| 2.5mm | 119980–120020 MHz | 2.5mm |
| 2mm | 142000–149000 MHz | 2mm |
| 1mm | 241000–250000 MHz | 1mm |

## Modes

### Phone Modes

| Mode | ADIF name | Submode |
|------|-----------|---------|
| Single-sideband | `SSB` | `LSB`, `USB` |
| AM | `AM` | |
| FM | `FM` | |

### CW

| Mode | ADIF name |
|------|-----------|
| Morse code | `CW` |

### Digital Modes

| Mode | ADIF name | Submode | Notes |
|------|-----------|---------|-------|
| FT8 | `FT8` | | Canonical direct ADIF mode |
| FT4 | `MFSK` | `FT4` | Canonical ADIF 3.1.7 pair |
| FT2 | `MFSK` | `FT2` | Canonical ADIF 3.1.7 pair |
| JS8 | `MFSK` | `JS8` | Canonical export pair |
| Q65 | `MFSK` | `Q65` | Canonical export pair |
| WSPR | `WSPR` | | Weak Signal Propagation Reporter |
| PSK31 | `PSK` | `PSK31` | Canonical export pair |
| RTTY | `RTTY` | | Radio teletype |
| Olivia | `OLIVIA` | | |
| Codec2 | `CODEC2` | | Digital voice |
| DMR | `DIGITALVOICE` | `DMR` | Canonical export pair |
| FreeDV | `DIGITALVOICE` | `FREEDV` | Canonical export pair |
| VARA HF | `DYNAMIC` | `VARA HF` | Canonical export pair |
| VARA FM 1200 | `DYNAMIC` | `VARA FM 1200` | Canonical export pair |
| PACKET | `PKT` | | Canonical export mode |
| APRS | `APRS` | | Position reporting |

TODO: Add complete list of recognized modes from ADIF spec. This table is representative, not exhaustive.

## Mode to ADIF Mapping

RadioLedger maps common shorthand mode names to ADIF:

| User enters | ADIF MODE | ADIF SUBMODE |
|------------|-----------|-------------|
| FT8 | FT8 | |
| FT4 | MFSK | FT4 |
| FT2 | MFSK | FT2 |
| JS8 | MFSK | JS8 |
| Q65 | MFSK | Q65 |
| DMR | DIGITALVOICE | DMR |
| VARAHF | DYNAMIC | VARA HF |
| PACKET | PKT | |
| SSB | SSB | |
| LSB | SSB | LSB |
| USB | SSB | USB |

## Related

- [ADIF Field Mapping](adif-field-mapping.md)
- [Logging QSOs](../user-guide/logging-qsos.md)
- [ADIF Reference for Developers](../contributing/adif-reference.md)
