# Repository Structure

The public repository contains the complete self-hostable RadioLedger platform.
Anyone can clone, build, test, and run it without access to private operations
material.

```
radioledger/
├── api/                    # Go API server
│   ├── cmd/                # Entry points (radioledger-api, radioledger-worker)
│   ├── internal/           # Business logic, handlers, middleware
│   │   ├── api/            # HTTP handlers, router, middleware
│   │   ├── service/        # Business logic (QSO, ADIF, sync, awards)
│   │   ├── repository/     # sqlc-generated data access layer
│   │   ├── worker/         # River job definitions and workers
│   │   └── domain/         # Shared types: QSO, Logbook, User, etc.
│   └── pkg/                # Internal libraries (ADIF parser, Maidenhead, callsign)
├── web/                    # Web UI (SPA — framework TBD)
├── desktop/                # Tauri desktop client (macOS, Windows, Linux)
├── mobile/                 # Mobile app (Flutter — iOS + Android)
├── database/
│   ├── migrations/         # Sequential SQL migrations (goose)
│   │   └── 001_initial.sql # Full initial schema
│   └── seeds/              # Reference data (bands, modes, DXCC, POTA parks, SOTA)
├── docker/
│   ├── docker-compose.yml  # Self-host stack (API + PostGIS + Zitadel + Caddy)
│   ├── Dockerfile.api      # API server image
│   └── Dockerfile.web      # Web UI image
├── pkg/                    # Shared Go libraries (may become standalone modules)
│   ├── adif/               # Streaming ADIF parser (ADI + ADX)
│   ├── maidenhead/         # Grid square utilities
│   └── callsign/           # Callsign normalization, DXCC resolution
├── testdata/               # Test fixtures
│   └── adif/               # Real-world ADIF corpus for regression/fuzz testing
├── docs/                   # Public documentation
│   ├── SCHEMA.md
│   ├── ADIF.md
│   ├── SYNC_SERVICES.md
│   ├── DESKTOP_CLIENT.md
│   ├── MOBILE_APP.md
│   ├── POSTGIS.md
│   ├── QSL_AND_AWARDS.md
│   ├── self-hosting/       # Self-hosting guides
│   ├── api/                # API reference
│   └── contributing/       # Contributor guides
├── README.md
├── REPO_STRUCTURE.md
├── LICENSE                 # AGPLv3
└── Makefile
```

### What's Public
- All application code (API, web, desktop, mobile)
- Database schema, migrations, seeds
- Docker Compose for self-hosting
- All public documentation
- ADIF parser library (candidate for its own Go module)
- Test suite and test data

### License

**AGPLv3** — see [LICENSE](LICENSE). See
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for attribution requirements.

## Release boundary

Private deployment configuration, credentials, incident material, customer data,
and draft legal or planning documents are not part of this repository. Public
candidates are generated as fresh, one-commit exports and fail closed if the
privacy denylist detects private material.
