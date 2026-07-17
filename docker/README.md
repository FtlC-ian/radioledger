# docker/

Docker Compose files and Dockerfiles for RadioLedger.

## What Goes Here

- `docker-compose.yml` — self-host stack (API + PostGIS + Zitadel + Caddy)
- `docker-compose.tls.yml` — TLS overlay via Caddy (automatic HTTPS / Let's Encrypt)
- `Dockerfile.api` — API server image
- `Dockerfile.web` — Web UI image
- `Caddyfile` — Caddy reverse proxy config

## Self-Hosting Quick Start (planned)

```bash
# Copy the example env file and fill in your domain
cp .env.example .env

# Build local images, then start the stack
docker compose build api lotw-vault web docs
docker compose up -d --no-build

# Check migration status
docker compose exec api radioledger migrate status
```

## Security Defaults

- PostgreSQL is NOT exposed to the host (internal Docker network only)
- All secrets auto-generated on first run (DB password, master encryption key, session secret)
- API container runs read-only with minimal Linux capabilities
- TLS via Caddy — automatic certificate provisioning

See `docs/SELF_HOSTING.md` for full setup guide.



## Production auth note

The default `.env.example` uses `APP_ENV=development` with `AUTH_MODE=dev` so a fresh local self-host smoke test can start without configuring an identity provider. Because development auth is intentionally insecure, published ports bind to `127.0.0.1` by default. Do **not** set `BIND_ADDRESS=0.0.0.0` or expose the stack to a public network until you switch to `APP_ENV=production`, set `AUTH_MODE=zitadel`, configure `ZITADEL_URL`, and start the Zitadel profile or point to an external Zitadel instance.

## Observability Stack

The compose stack now includes:

- **Prometheus** (`http://localhost:9090`) scraping `api:8080/metrics` and `worker:2112/metrics`
- **Grafana** (`http://localhost:3001`) with a pre-provisioned RadioLedger dashboard
- **Jaeger** (`http://localhost:16686`) under the `dev` profile for trace inspection

Run with monitoring and Jaeger enabled:

```bash
docker compose build api lotw-vault web docs
docker compose --profile monitoring --profile dev up -d --no-build
```

## Multi-Process Deployment (API + Worker)

RadioLedger supports running the API server and the background worker as separate
processes. This allows you to scale them independently, isolate resource usage, and
restart workers without touching the API.

### Single-host (default compose stack)

The `worker` service in `docker-compose.yml` runs the same `radioledger-api` image
with the `--mode=worker` flag. It shares the same database and environment but does
not bind any external ports.

```bash
docker compose build api lotw-vault web docs
docker compose up -d --no-build # starts api + worker together
docker compose restart worker # restart only the worker (safe during live games)
docker compose logs -f worker # tail worker logs
```

The API continues to serve HTTP traffic while the worker handles async jobs in the
background. Prometheus scrapes both processes:

| Service | Metrics endpoint        |
|---------|------------------------|
| api     | `api:8080/metrics`     |
| worker  | `worker:2112/metrics`  |

### Separate-host (scaled-out workers)

If you want to run workers on a different machine from the API — for example a
dedicated job-processing box or an auto-scaled worker fleet — use the
`docker-compose.worker.yml` override file:

```bash
# On the worker host, with DATABASE_URL and RADIOLEDGER_MASTER_KEY in .env:
docker compose -f docker-compose.worker.yml up -d
```

The override file exposes `127.0.0.1:2112` so your external Prometheus can scrape
the worker. Update `prometheus.yml` on the Prometheus host to add:

```yaml
- job_name: "radioledger-worker"
  metrics_path: /metrics
  static_configs:
    - targets: ["<worker-host-ip>:2112"]
```

### When to use each mode

| Scenario | Recommendation |
|----------|---------------|
| Local development | Single host — `docker compose build api lotw-vault web docs && docker compose up -d --no-build` |
| Small self-hosted deployment | Single host — worker runs alongside API |
| High-throughput / isolated scaling | Separate host — `docker-compose.worker.yml` |
| API and worker on different VMs | Separate host override per worker VM |
