# pkg/

Shared Go libraries that can be imported by other projects.

## What Goes Here

Public Go packages that may eventually be split into standalone modules:

- `adif/` — Streaming ADIF parser (ADI and ADX formats)
  - Handles UTF-8 BOM, CR/LF/CRLF, case-insensitive field names
  - APP_* namespace fields preserved in JSONB
  - Stream-based — never loads entire file into memory
  - Fuzz-tested against real-world ADIF corpus
- `maidenhead/` — Maidenhead grid square utilities
  - Grid → WGS-84 center point conversion
  - Grid validation (4 and 6 character)
  - Great-circle distance calculation
- `callsign/` — Callsign normalization and DXCC entity resolution
  - Portable suffix handling (W5XXX/4, W5XXX/MM, VK9/W5XXX)
  - DXCC entity lookup from callsign prefix

## License

AGPLv3 (same as the main project). If the ADIF parser is split into its own module,
it may be re-licensed under MIT/Apache 2.0 to maximize community adoption.
