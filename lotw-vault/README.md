# LoTW Signing Vault

A purpose-built microservice that stores ARRL Logbook of the World (LoTW) signing
credentials and produces signed `.tq8` files on demand.

The vault is a **security boundary**: it holds encrypted private keys and is the
only service that ever touches plaintext key material. It has no internet access
by design. Uploading `.tq8` files to ARRL is the responsibility of the main
RadioLedger API.

---

## Architecture

```
  ┌─────────────────────────────┐      vault-internal Docker network
  │   RadioLedger API           │      (internal: true — no internet)
  │                             │◄────────────────────────────────────┐
  │  • Receives ADIF from user  │                                     │
  │  • POSTs to /sign           │         ┌───────────────────────┐   │
  │  • Gets back .tq8 blob      │         │   lotw-vault          │   │
  │  • Uploads .tq8 to ARRL     │         │                       │   │
  │    (see internal/lotw/)     │         │  SQLite (/data/       │   │
  └─────────────────────────────┘         │  certs.db)            │   │
                                          │                       │   │
                                          │  AES-256-GCM          │   │
                                          │  Argon2id keys        │   │
                                          └───────────────────────┘   │
                                                   ▲                  │
                                                   └──────────────────┘
```

**Key points:**

- The vault lives on a Docker network with `internal: true`. It cannot make
  outbound connections to the internet (no ARRL, no DNS, nothing).
- No ports are exposed to the host or the public network.
- The vault runs as `uid 65534` (nobody) in a `scratch` container with `read_only`
  filesystem, `cap_drop: ALL`, and `no-new-privileges`.
- `/upload` was intentionally removed. See `internal/lotw/upload.go` for the
  upload helper that lives in the main API.

---

## API

### `GET /health`
Liveness check.
```json
{"status": "ok", "time": "2024-01-01T12:00:00Z"}
```

### `POST /import-cert`
Import a `.p12` LoTW certificate. Encrypts the private key with Argon2id +
AES-256-GCM under the user's password. Stores everything in SQLite.

**Multipart form fields:**
| Field | Description |
|-------|-------------|
| `user_id` | Unique user identifier |
| `p12_file` | The `.p12` certificate file |
| `p12_password` | Password protecting the `.p12` |
| `user_password` | Password used to encrypt the stored key |

**Response:**
```json
{
  "callsign": "W1AW",
  "dxcc": "291",
  "qso_start": "2023-01-01",
  "qso_end": "2026-01-01",
  "not_before": "2023-01-01T00:00:00Z",
  "not_after": "2026-01-01T00:00:00Z",
  "message": "Certificate imported successfully"
}
```

### `POST /sign`
Sign an ADIF log and return a `.tq8` file.

**JSON body:**
```json
{
  "user_id": "user123",
  "user_password": "s3cr3t",
  "adif_data": "<CALL:4>W1AW <BAND:3>40M <MODE:2>CW <QSO_DATE:8>20240101 <TIME_ON:4>1200 <EOR>",
  "station": {
    "callsign": "W1AW",
    "dxcc": "291",
    "gridsquare": "FN31",
    "cqz": "5",
    "ituz": "8"
  }
}
```

Returns binary `.tq8` (gzip-compressed ADIF with signatures).

### `POST /rotate-password`
Re-encrypt the stored private key under a new password. Uses a fresh Argon2
salt. The old password is verified before any changes are made.

**JSON body:**
```json
{
  "user_id": "user123",
  "old_password": "current_password",
  "new_password": "new_secure_password"
}
```

**Response:**
```json
{"message": "password rotated successfully"}
```

### `GET /cert-info?user_id=xxx`
Returns public certificate metadata. **No password required.** Never returns
key material.

**Response:**
```json
{
  "user_id": "user123",
  "callsign": "W1AW",
  "dxcc": "291",
  "gridsquare": "FN31",
  "cqz": "5",
  "ituz": "8",
  "qso_start": "2023-01-01",
  "qso_end": "2026-01-01",
  "cert_not_before": "2023-01-01T00:00:00Z",
  "cert_not_after": "2026-01-01T00:00:00Z",
  "expired": false
}
```
Returns `404` if no certificate is stored for `user_id`.

### `DELETE /cert`
Delete a stored certificate. Requires password verification before deletion.

**JSON body:**
```json
{
  "user_id": "user123",
  "user_password": "s3cr3t"
}
```

---

## Storage

Certificates are stored in a SQLite database at `/data/certs.db` (configurable
with `-db`). The filesystem store (`internal/certstore/filesystem.go`) is kept
as a fallback and can be activated with the `-data` flag.

**Schema:**
```sql
CREATE TABLE certs (
  user_id         TEXT PRIMARY KEY,
  callsign        TEXT NOT NULL,
  encrypted_key   BLOB NOT NULL,    -- AES-256-GCM(Argon2id_key, pkcs8_der)
  argon2_salt     BLOB NOT NULL,
  cert_der        BLOB NOT NULL,    -- raw DER X.509 certificate
  ca_chain_der    BLOB,             -- optional CA chain DER
  dxcc            TEXT,
  gridsquare      TEXT,
  cqz             TEXT,
  ituz            TEXT,
  qso_not_before  TEXT,
  qso_not_after   TEXT,
  cert_not_before TEXT,
  cert_not_after  TEXT,
  created_at      TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
```

The SQLite driver is `modernc.org/sqlite` — pure Go, no CGo, compatible with
`scratch`-based Docker images.

---

## Security Model

| Property | Implementation |
|----------|---------------|
| Key encryption | AES-256-GCM with Argon2id-derived key (64 MB, 2 passes, 4 threads) |
| Salt | 32 bytes CSPRNG per key import (new salt on password rotate) |
| Network isolation | `internal: true` Docker network — no internet access |
| Filesystem | `read_only: true`, `/tmp` tmpfs only |
| Privileges | `cap_drop: ALL`, `no-new-privileges: true`, `uid 65534` |
| Container base | `scratch` — no shell, no package manager |
| Upload responsibility | Main RadioLedger API (vault never calls ARRL) |

---

## Running

### Docker Compose (production)
```bash
docker-compose up -d
```
The vault listens on `:8080` internally. Only services on the `vault-internal`
network can reach it.

### Local development
```bash
go run ./cmd/lotw-vault/ -addr :8080 -db /tmp/test-certs.db
```

### Test-sign mode (offline signing verification)
```bash
go run ./cmd/lotw-vault/ -test-sign \
  -p12 /path/to/your.p12 \
  -p12-password yourpassword \
  -adif /path/to/log.adi \
  -output signed.tq8 \
  -callsign W1AW \
  -grid FN31

# Inspect the result:
gunzip -c signed.tq8 | head -50
```

---

## Tests

```bash
go test ./...
```

The integration test (`cmd/lotw-vault/integration_test.go`) starts a live server,
generates a self-signed test certificate (no real ARRL cert needed), and exercises
the full flow:

1. Import cert
2. Check cert-info
3. Sign ADIF
4. Verify signature (gzip + tCONTACT + SIGN_LOTW_V2.0)
5. Rotate password
6. Sign again with new password
7. Verify old password is rejected
8. Delete cert
9. Verify cert is gone

---

## LoTW Upload (Main API Responsibility)

The vault does **not** upload to ARRL. After receiving a `.tq8` from `/sign`,
the main RadioLedger API should call `internal/lotw.Upload(tq8Data)`:

```go
import "github.com/FtlC-ian/radioledger/lotw-vault/internal/lotw"

result, err := lotw.Upload(tq8Bytes)
if err != nil {
    // network error
}
if result.Accepted {
    // log confirmed by ARRL
}
```

See `internal/lotw/upload.go` for the full implementation.
