# Callsign Parsing and Reference Data

> How RadioLedger identifies callsign countries, license classes, and geographic data.

## Callsign Database Sources

RadioLedger maintains a local cache of global callsign databases, covering approximately 1.5 million records.

### Supported Countries

The following countries' regulatory databases are synced regularly:

| Country | Source | Sync Frequency | Records (Approx) |
|---------|--------|----------------|-------------------|
| **USA** | FCC ULS | Weekly Full / Daily Diff | 780,000 |
| **Canada** | ISED | Monthly | 72,000 |
| **Australia** | ACMA | Monthly | 16,000 |
| **France** | ANFR | Monthly | 14,000 |
| **Mexico** | IFT | Monthly | 4,000 |
| **Netherlands** | RDI | Monthly | 12,000 |
| **UK** | Ofcom | Monthly | 75,000 |
| **Germany** | BNetzA | Monthly (PDF Parsing) | 65,000 |
| **Japan** | JJ1WTL/MIC | Monthly | 380,000 |

### Background Sync Infrastructure

Syncing is handled by River background workers. Each source has a dedicated parser and sync worker:
- **FCC**: Daily differential updates and weekly full refreshes.
- **BNetzA**: Uses a specialized PDF parser to extract records from official German regulatory documents.
- **Others**: Standardized CSV/Pipe/JSON imports via monthly River jobs.

### Supplemental Lookups

For countries not covered by bulk imports, RadioLedger uses:
- **HamQTH XML API**: A free, supplemental source for international callsigns.
- **HamDB.org**: A free API for quick, real-time lookups.

## Callsign Parsing Algorithm

RadioLedger uses a multi-step process to parse a callsign string into actionable data.

### 1. Prefix Extraction

Extracts the 1–3 character ITU prefix to determine the base country (DXCC entity).
Example: `KI5BRG` → `KI` → `United States`.

### 2. Prefix Overrides

Handles prefixes and suffixes that change the entity or location.
Example: `VK9/W1AW` → `VK9` → `Lord Howe Island`.

### 3. Database Lookup

Queries the local `callsign_records` table for an exact match. If found, it populates:
- **Full Name** and **Address**
- **License Class** (Technician, General, Extra, etc.)
- **Maidenhead Grid Square** (calculated from address if not provided)

### 4. Special Identifiers

Identifies and handles special-use callsigns:
- **Event Stations**: Short-term special event prefixes.
- **Portable Indicators**: `/P`, `/M`, `/AM`, `/MM`.
- **Club Stations**.

## License Classes

RadioLedger normalizes license classes across different regulatory sources to a common set of categories for filtering and statistics:
- **Entry**: Foundation, Technician
- **Mid**: General, Intermediate
- **Advanced**: Extra, Advanced, Class A

## Related

- [DXCC Entities Reference](dxcc-entities.md)
- [Maidenhead Grids](maidenhead-grids.md)
- [Sync Worker Infrastructure](../sync/worker-infrastructure.md)
- [Callsign Database Spec](../CALLSIGN_DATABASE_SPEC.md)
