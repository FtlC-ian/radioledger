# Sync Worker Infrastructure

> Technical reference for the River-based worker infrastructure introduced in #42.

## Contents

1. [Architecture Overview](#architecture-overview)
2. [Sync Services](#sync-services)
3. [Rate Limiting](#rate-limiting)
4. [Circuit Breaker](#circuit-breaker)
5. [Retry with Backoff + Jitter](#retry-with-backoff--jitter)
6. [Metrics](#metrics)
7. [Service Health Endpoint](#service-health-endpoint)
8. [Base Worker Abstraction](#base-worker-abstraction)
9. [Database Migration](#database-migration)
10. [Configuration Reference](#configuration-reference)

---

## Architecture Overview

Sync services run as [River](https://riverqueue.com/) background workers. River uses PostgreSQL as its queue backend, giving us durable job storage, reliable delivery, and transactional enqueue semantics.

```
                        ┌────────────────────────────────────────┐
                        │           RadioLedger API               │
                        │                                         │
  QSO created ──────►  │  InsertPendingSyncForQSO (tx)           │
  Manual trigger ────► │  EnqueueEQSLUpload / EnqueueClubLog...  │
                        │              │                          │
                        └──────────────┼──────────────────────────┘
                                       │
                                       ▼
                              PostgreSQL (river_job)
                                       │
                          ┌────────────┼────────────┐
                          ▼            ▼            ▼
                    EQSLUpload   ClubLogUpload  ClubLogDelete
                     Worker        Worker        Worker
                          │            │
                     ┌────┴────────────┴────┐
                     │     Infra layer      │
                     │  rate limit check    │
                     │  circuit breaker     │
                     │  retry / backoff     │
                     └──────────────────────┘
```

## Sync Services

### Callsign Sync Workers

RadioLedger maintains a local cache of global callsign databases. Sync workers run on weekly/monthly schedules to keep these records current.

| Worker | Source | Schedule |
|--------|--------|----------|
| `FCCSyncWorker` | USA (FCC) | Weekly |
| `ISEDSyncWorker` | Canada (ISED) | Monthly |
| `ACMASyncWorker` | Australia (ACMA) | Monthly |
| `ANFRSyncWorker` | France (ANFR) | Monthly |
| `IFTSyncWorker` | Mexico (IFT) | Monthly |
| `RDISyncWorker` | Netherlands (RDI) | Monthly |
| `OfcomSyncWorker` | UK (Ofcom) | Monthly |
| `BNetzASyncWorker` | Germany (BNetzA) | Monthly (PDF parsing) |
| `JJ1WTLSyncWorker` | Japan (JJ1WTL/MIC) | Monthly |

### Award Refresh Worker

The `award_progress_refresh` worker recalculates award progress based on a **dirty-flag pattern**. When a QSO is logged or updated, affected award rows are marked dirty and this worker asynchronously updates the totals.

### External Service Workers

| Worker | Job kind | Purpose |
|--------|----------|---------|
| `EQSLUploadWorker` | `eqsl_upload` | Upload pending QSOs to eQSL (batches of 100) |
| `EQSLDownloadWorker` | `eqsl_download` | Download inbox, match and confirm local QSOs |
| `ClubLogUploadWorker` | `clublog_upload` | Upload pending QSOs to Club Log (batches of 100) |
| `ClubLogDeleteWorker` | `clublog_delete` | Delete a specific QSO from Club Log |

---

## Rate Limiting

Each service has a **global, Postgres-backed rate limiter** shared across all worker replicas. Every worker acquires a Postgres advisory lock and increments a shared per-second bucket.

### Default RPS Limits

| Service | RPS |
|---------|-----|
| eqsl | 1 |
| clublog | 5 |
| qrz | 2 |
| lotw | 1 |

Override via environment variables: `SYNC_RATE_LIMIT_EQSL_RPS`, `SYNC_RATE_LIMIT_CLUBLOG_RPS`, etc.

---

## Circuit Breaker

The circuit breaker protects external services from being hammered when they are down or credentials are invalid. State is persisted in Postgres and survives process restarts.

### Default Circuit Breaker Settings

| Setting | Default |
|---------|---------|
| `CircuitFailureThreshold` | 5 consecutive failures |
| `CircuitRecoveryTimeout` | 60 seconds |

---

## Metrics

All sync metrics are Prometheus gauges/histograms/counters registered in `api/internal/metrics/metrics.go`.

| Metric | Type | Description |
|--------|------|-------------|
| `river_queue_depth` | Gauge | Current River queue depth by service |
| `river_job_duration_seconds` | Histogram | Duration of sync worker API calls |
| `river_job_failures_total` | Counter | Total sync worker failures |

---

## Configuration Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `SYNC_RATE_LIMIT_EQSL_RPS` | `1` | Max requests/second to eQSL |
| `SYNC_RATE_LIMIT_CLUBLOG_RPS` | `5` | Max requests/second to Club Log |
| `SYNC_RATE_LIMIT_LOTW_RPS` | `1` | Max requests/second to LoTW |
| `SYNC_RATE_LIMIT_QRZ_RPS` | `2` | Max requests/second to QRZ |
| `SYNC_CIRCUIT_FAILURE_THRESHOLD` | `5` | Consecutive failures before circuit opens |
| `SYNC_CIRCUIT_RECOVERY_TIMEOUT` | `60s` | How long the circuit stays open before a probe |
