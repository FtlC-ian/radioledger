# PostGIS Integration

## Why PostGIS

Ham radio is spatial by nature. Every QSO involves two locations, awards are geographic, and the hobby is fundamentally about "who can I reach from where." PostGIS gives us spatial queries, distance calculations, and geographic analysis that no other logging platform offers.

Enable PostGIS from day one. Even if early features don't use it heavily, having the extension loaded and geometry columns in place means we can build spatial features without schema migrations later.

## Schema Additions

### Geometry Columns on Core Tables

```sql
CREATE EXTENSION IF NOT EXISTS postgis;

-- Add geometry columns to users (home station location)
ALTER TABLE users ADD COLUMN location GEOMETRY(Point, 4326);
-- Auto-compute from grid_square on insert/update via trigger

-- Add geometry columns to qsos (both sides of the contact)
ALTER TABLE qsos ADD COLUMN my_location GEOMETRY(Point, 4326);
ALTER TABLE qsos ADD COLUMN their_location GEOMETRY(Point, 4326);
ALTER TABLE qsos ADD COLUMN distance_km NUMERIC;  -- computed on insert

-- Spatial indexes
CREATE INDEX idx_users_location ON users USING GIST(location);
CREATE INDEX idx_qsos_my_location ON qsos USING GIST(my_location);
CREATE INDEX idx_qsos_their_location ON qsos USING GIST(their_location);
```

### Maidenhead Grid ↔ Geometry Conversion

```sql
-- Function to convert Maidenhead grid to center point
CREATE OR REPLACE FUNCTION maidenhead_to_point(grid TEXT)
RETURNS GEOMETRY(Point, 4326) AS $$
DECLARE
    lon NUMERIC;
    lat NUMERIC;
    g TEXT;
BEGIN
    IF grid IS NULL OR length(grid) < 4 THEN
        RETURN NULL;
    END IF;
    
    g := upper(grid);
    
    -- Field (18x18, 20° lon x 10° lat)
    lon := (ascii(substr(g, 1, 1)) - ascii('A')) * 20 - 180;
    lat := (ascii(substr(g, 2, 1)) - ascii('A')) * 10 - 90;
    
    -- Square (10x10, 2° lon x 1° lat)
    lon := lon + (ascii(substr(g, 3, 1)) - ascii('0')) * 2;
    lat := lat + (ascii(substr(g, 4, 1)) - ascii('0')) * 1;
    
    -- Subsquare (24x24, 5' lon x 2.5' lat)
    IF length(g) >= 6 THEN
        lon := lon + (ascii(substr(g, 5, 1)) - ascii('A')) * (2.0/24);
        lat := lat + (ascii(substr(g, 6, 1)) - ascii('A')) * (1.0/24);
        -- Center of subsquare
        lon := lon + (1.0/24);
        lat := lat + (0.5/24);
    ELSE
        -- Center of square
        lon := lon + 1;
        lat := lat + 0.5;
    END IF;
    
    RETURN ST_SetSRID(ST_MakePoint(lon, lat), 4326);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Trigger to auto-compute geometry from grid
CREATE OR REPLACE FUNCTION update_qso_locations() RETURNS TRIGGER AS $$
BEGIN
    NEW.my_location := maidenhead_to_point(NEW.my_gridsquare);
    NEW.their_location := maidenhead_to_point(NEW.gridsquare);
    
    IF NEW.my_location IS NOT NULL AND NEW.their_location IS NOT NULL THEN
        NEW.distance_km := ST_DistanceSphere(NEW.my_location, NEW.their_location) / 1000;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_qso_locations
    BEFORE INSERT OR UPDATE ON qsos
    FOR EACH ROW EXECUTE FUNCTION update_qso_locations();
```

## Features PostGIS Enables

### Map Visualizations
- World map with all contacts plotted
- Great circle lines from your station to each contact
- Heatmap of contact density by region
- Animated "QSO map" showing contacts over time (contest replay)

### Spatial Queries
- "Show me all stations I've worked within 100km of grid EM35"
- "Which POTA parks are within 2 hours drive?" (with road network data, future)
- "Find QSOs where the other station was in a specific county/state/country"
- Nearest-neighbor queries: "Who's the closest station I've worked on 2m?"

### Distance & Bearing
- Automatic distance calculation for every QSO (great circle)
- Bearing from my station to theirs
- Distance-based statistics: longest QSO per band, average distance by mode
- Distance scoring for contests that use it

### Award Tracking (Spatial)
- VUCC grid square map: worked/confirmed grids as polygon overlay
- WAS: state polygons colored by status
- DXCC: country polygons with worked/confirmed status
- County hunting: county boundary polygons
- Custom geographic awards: "work all grid squares in EM" as a spatial query

### POTA/SOTA Integration
- Park boundaries as polygons (POTA publishes park data)
- Summit locations as points (SOTA database)
- "Parks near me" from mobile app GPS
- "Which parks have I activated within this state?"
- Distance from park/summit to contact (interesting stat for activators)

### Propagation Analysis (Future)
- Plot QSOs on a map filtered by band/mode/time to visualize propagation
- Overlay solar terminator (grey line)
- Antenna pattern visualization relative to contact directions
- "Where are my contacts concentrated by band?" — reveals antenna directivity

## Reference Data (Spatial)

### DXCC Boundaries
- Country/entity polygons for map coloring
- Source: Natural Earth Data + DXCC entity list mapping
- Handle overlapping/disputed entities

### US State & County Boundaries
- For WAS and county hunting awards
- Source: US Census TIGER/Line shapefiles

### Grid Square Grid
- Maidenhead grid polygons (computed, not stored — they're regular)
- Used for VUCC visualization

### POTA Park Boundaries
- POTA publishes park reference data
- Many parks have polygon boundaries available from NPS/state agencies
- At minimum: point locations for all parks

### SOTA Summits
- Point locations with elevation
- Source: SOTA database (publicly available)

## Performance Considerations

- Spatial indexes (GIST) are essential — already included above
- Geometry columns add ~32 bytes per row (Point type) — negligible
- Distance computation in trigger adds minimal overhead on insert
- Heavy spatial queries (e.g., "all contacts within polygon") will be fast with GIST indexes
- Consider materialized views for expensive spatial aggregations (grid square counts, etc.)

## PlanetScale Compatibility

- Verify PlanetScale Postgres supports PostGIS extension
- If not: spatial features work on self-hosted, degrade gracefully on managed
- Fallback: compute distances in Go using Haversine, store as plain NUMERIC
- Most spatial queries can be approximated with bounding-box lat/lon range queries

## Open Questions

- [ ] Does PlanetScale Postgres support PostGIS? (critical to verify)
- [ ] Should we pre-load park/summit/country boundary data or download on demand?
- [ ] Antenna pattern modeling — how deep do we go? (azimuthal plots are useful but complex)
- [ ] Integration with OpenStreetMap for map tiles?
