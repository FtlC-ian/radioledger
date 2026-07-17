# database/seeds/

Reference data for RadioLedger. These are static/semi-static datasets that populate
lookup tables and are safe to re-run (upsert semantics).

## Contents (planned)

- `bands.sql` — ITU amateur band allocations with frequency edges and WARC flags
- `modes.sql` — ADIF mode and submode definitions with canonical name mapping
- `dxcc_entities.sql` — ARRL DXCC entity list with LoTW name variants
- `pota_parks.sql` — POTA park reference data (updated weekly from api.pota.app)
- `sota_summits.sql` — SOTA summit database (updated periodically from sota.org.uk)

## Update Frequency

- Bands/Modes: Rarely change; update manually when ADIF spec changes
- DXCC: Rarely changes; update when ARRL announces entity changes
- POTA parks: Weekly automated job (`POTARefreshJob`)
- SOTA summits: Monthly automated job (`SOTARefreshJob`)
