# DXCC Entities

> How RadioLedger resolves DXCC entities from callsigns.

## What is DXCC?

DXCC (DX Century Club) is the ARRL's award program for working stations in at least 100 different "entities" (countries and territories). DXCC entities are defined by the ARRL and number approximately 340.

## DXCC Entity Table

RadioLedger includes the complete DXCC entity list as reference data. Each entity has:

- **ADIF entity number** — used in ADIF files
- **Entity name** — human-readable name
- **Primary prefix** — the callsign prefix (e.g., `W`, `JA`, `VK`)
- **Continent** — NA, EU, AS, AF, OC, SA, AN
- **CQ zone** — CQ zone 1–40
- **ITU zone** — ITU zone
- **Latitude/Longitude** — approximate center of entity (for distance/bearing)

TODO: Embed the full entity table or link to the reference data file.

## Entity Resolution from Callsigns

RadioLedger resolves the DXCC entity automatically from the callsign. The algorithm:

### 1. Check for Prefix Override

If the callsign has a prefix before `/`, it's a prefix override:
- `VK9/W1AW` → entity is VK9 (Cocos Keeling), not USA (W prefix)
- `JW/LA3ABC` → entity is JW (Svalbard), not Norway (LA)

### 2. Parse the Base Callsign Prefix

For standard callsigns, find the longest matching prefix in the DXCC entity table:
- `W1AW` → prefix `W` → USA
- `JA1ABC` → prefix `JA` → Japan
- `VK2ABC` → prefix `VK` → Australia

### 3. Handle Special Cases

- Maritime mobile (`/MM`): DXCC entity = Maritime Mobile (not based on prefix)
- Aeronautical mobile (`/AM`): DXCC entity = Aeronautical Mobile
- Portable (`/P`): entity is from the base prefix, not the suffix
- Numbered portable (`/7`): entity is based on base callsign prefix

### 4. Store Results

After resolution:
- `dxcc_entity_id` — ADIF DXCC entity number
- `dxcc_prefix` — resolved prefix

If resolution fails, these fields are NULL. You can manually set the DXCC entity in the QSO edit form.

## Disputed Entities

Some entities are disputed or not universally recognized. RadioLedger stores the ADIF-defined entity list without taking political positions.

## Entity Updates

ARRL periodically updates the DXCC entity list (new entities, deleted entities). RadioLedger's entity table is updated with each release. Self-hosters get updates via Docker image updates.

## Related

- [Callsign Parsing](callsign-parsing.md)
- [Awards Tracking](../user-guide/awards-tracking.md)
- [SCHEMA.md](../SCHEMA.md)
