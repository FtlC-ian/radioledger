# testdata/

Test fixtures for RadioLedger, primarily ADIF files for regression and fuzz testing.

## What Goes Here

- `adif/` — Real-world ADIF files from various logging programs:
  - `wsjtx/` — WSJT-X (FT8, FT4, JT65, MSK144 exports)
  - `hrd/` — Ham Radio Deluxe
  - `n1mm/` — N1MM+ contest logger
  - `log4om/` — Log4OM (popular European logger)
  - `dxkeeper/` — DXKeeper (DXLab suite)
  - `maclggerdx/` — MacLoggerDX
  - `icom/` — ICOM IC-7300 built-in logger (minimal ADIF)
  - `aclog/` — AC Log
  - `malformed/` — Intentionally broken files for parser hardening tests

## Usage

The ADIF corpus is used for:
1. **Round-trip tests:** Import → Export → compare (must be byte-identical for known-good files)
2. **Fuzz testing:** `go test -fuzz=FuzzADIFParser ./pkg/adif/`
3. **Performance tests:** 500k QSO import must complete in < 30 seconds

## Adding Test Files

Contributed ADIF files should be anonymized (callsigns replaced with fictitious ones)
before committing. Real callsigns from real operators must not be committed without consent.
