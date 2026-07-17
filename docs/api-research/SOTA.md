# SOTA (Summits on the Air) API Research

## API Status
- **Status**: Active / Public (Some v1 and v2 endpoints coexist)
- **Official Documentation**: [https://api-db.sota.org.uk/docs/](https://api-db.sota.org.uk/docs/)
- **Primary API Host**: `https://api-db.sota.org.uk/`

## Authentication Method
- **Method**: API Key (obtained via user profile).
- **Public Endpoints**: Read-only summit and spot information.
- **Private Endpoints**: Activation log uploads require authentication.

## Available Operations
- **Fetch Spots**: `GET https://api-db.sota.org.uk/spots/all` (JSON).
- **Fetch Summit Info**: `GET https://api-db.sota.org.uk/summits/<SUMMIT_ID>`.
- **Upload Activation Log**: `POST https://api-db.sota.org.uk/logs/activator/` (V2 API).
- **Search Summits by Bounding Box**: `GET https://api-db.sota.org.uk/summits/bounding_box` (Requires coords).

## Rate Limits and Usage Guidelines
- **Guidelines**: "Do not hammer." Keep polling to a minimum.
- **SOTA Watch**: Polling for new spots should be limited to once every 60 seconds.

## Working Open-Source Implementations
1. **SOTAmāt** (Server-side): [https://sotamat.com/](https://sotamat.com/) (Extensive spot/alert integration).
2. **SOTA-Watcher** (JavaScript): [https://github.com/vsergeev/sota-watcher](https://github.com/vsergeev/sota-watcher)
3. **ON6ZQ SOTA Tools** (Perl): [https://github.com/on6zq/SOTA-Tools](https://github.com/on6zq/SOTA-Tools)

## Code Snippet (Spot Fetch Example)
```bash
# Fetch latest SOTA spots
curl -X GET "https://api-db.sota.org.uk/spots/all" | jq '.'
```

## Known Gotchas
- **V1 vs V2**: SOTA is transitioning to a V2 API. Some older documentation may refer to V1 endpoints (e.g., `sotawatch.org`). Use `api-db.sota.org.uk` for modern integrations.
- **Log Format (V2 CSV)**: SOTA prefers a specific CSV format (`V2,Callsign,Summit,Date...`) over standard ADIF for its internal database, although converters exist.
- **Summit Refs**: Format is `Association/Region-Number` (e.g., `W5N/OR-001`). Ensure correct casing.
- **Time in UTC**: All timestamps must be in UTC. SOTA is global and does not use local time.
