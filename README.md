# RadioLedger

**RadioLedger — a self-hostable ledger for radio amateurs.**

RadioLedger is an open-source ham radio logging platform built as a Go API,
PostgreSQL/PostGIS data model, Vue/Quasar web app, and native desktop companion
for station-local integrations.

The project began as a plan for a nonprofit-operated shared service: a neutral,
interoperable home for amateur-radio logs that could automate QSL submission and
confirmation tracking across otherwise disconnected services. The software is
also designed for operators who want to run it themselves, retain ownership of
their log data, and use security-conscious integrations with ham radio services.

## Project Status

RadioLedger is available today as a self-hostable application and an open
foundation for further development. There is no public hosted instance or
support commitment from the original maintainer.

That does not close the door on the original shared-service idea. A nonprofit,
community organization, or other capable operator could run RadioLedger and
take responsibility for its infrastructure, security, user support, and ongoing
development.

What is real here:
- Docker Compose self-hosting with API, web UI, and PostGIS database services.
- A Go REST API design with tenant isolation, UUID-facing resources, and
  security-first response/error conventions.
- A relational schema for serious logging: ADIF fidelity, callsign/operator
  identity, contest metadata, QSL workflows, awards, and sync state.
- A native desktop direction for UDP capture, OAuth, offline buffering, and local
  LoTW signing so private certificate material stays on the operator's machine.
- Documentation for architecture, schema, sync services, desktop/mobile clients,
  and the roadmap.

Important caveats:
- There is no public hosted RadioLedger service to sign up for today.
- The original maintainer is not offering hosting or committing to ongoing free
  operational support.
- Mobile apps and some sync/service flows are planned or partially specified, not
  all production-ready.
- Federation and cross-instance discovery are still design topics, not shipped
  features.
- Self-hosters are responsible for their own backups, upgrades, TLS, and secrets.

## Quick Start (Self-Hosted)

Requires: [Docker](https://docs.docker.com/get-docker/) with Compose v2+

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger/docker

./init.sh        # generates .env with random secrets (first run only)

docker compose up -d

# Watch migrations and startup:
docker compose logs -f api

# Visit http://localhost:3000
```

> **Backup your `.env` file** — it contains the master encryption key used to
> protect your external service credentials (QRZ, eQSL, ClubLog). If you lose
> it, those credentials are unrecoverable.

### What's Running

| Service | URL | Notes |
|---------|-----|-------|
| Web UI  | http://localhost:3000 | Quasar SPA |
| API     | http://localhost:9091 | Go REST API (`/v1/*`) |
| DB      | internal only | PostGIS — not exposed to host |

### Development Stack

For local development with DB access and hot-reload support:

```bash
cd docker

# Start with development overrides (exposes DB port, adds pgAdmin)
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d

# pgAdmin: http://localhost:5050
# DB direct: psql -h localhost -U radioledger -d radioledger
```

## Vision

A modern, flexible ham radio logging system that:
- Can support an operator-owned deployment or, with a capable organization
  behind it, a shared nonprofit service.
- Handles ADIF import/export with semantic-lossless fidelity.
- Makes QSL tracking more interoperable by syncing thoughtfully with LoTW, QRZ,
  eQSL, ClubLog, POTA, and related services.
- Captures live QSOs from WSJT-X, JS8Call, N1MM+, and similar tools through a
  native desktop client instead of forcing browser-only station integrations.
- Supports field operations such as POTA/SOTA through mobile/offline workflows as
  the client work matures.
- Uses PostgreSQL with a schema designed for extensibility, correctness, and
  tenant isolation.

RadioLedger is currently distributed as self-hostable software. This repository
does not offer or operate a hosted RadioLedger service, but the architecture and
license leave room for someone prepared to build and operate one.

## Docs

- [Schema Design](docs/SCHEMA.md)
- [ADIF Handling](docs/ADIF.md)
- [Sync Services](docs/SYNC_SERVICES.md)
- [Security Posture](docs/security.md)
- [Desktop Client](docs/DESKTOP_CLIENT.md)
- [Mobile App](docs/MOBILE_APP.md)

## Tech Stack

- **Backend:** Go API server
- **Database:** PostgreSQL + PostGIS
- **Frontend:** Vue 3 + Quasar (web, PWA, Capacitor for iOS/Android)
- **Mobile:** Cross-platform — Flutter (preferred)
- **Desktop Client:** Tauri (UDP capture + OAuth + local LoTW signing)
- **Deployment:** Docker Compose for self-hosting

## Repository Structure

See [REPO_STRUCTURE.md](REPO_STRUCTURE.md) for the full monorepo layout.

## License

AGPLv3 — see [LICENSE](LICENSE). Third-party and derived-data notices are in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
