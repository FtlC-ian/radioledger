# Desktop Client

**Last updated:** 2026-02-28

---

## Role

The desktop client is the local face of RadioLedger. It is not a standalone logger — the server is the source of truth — but it is what makes RadioLedger feel like a native desktop app. Install it, log in, and everything syncs down. Your logbook, settings, and sync service status. Then it acts as the local bridge between your shack software and the server.

Key insight: the desktop client enables desktop-local LoTW sync without uploading certificate material to RadioLedger. The ARRL cert stays on the user\'s machine, tQSL signing happens locally, and the client uploads directly to LoTW. Confirmation status flows back through the server.

**Signing mode constraint:** Desktop-local signing and hosted LoTW Vault signing are different trust models. Desktop-local signing keeps certificate material on the user's machine. Hosted vault signing stores uploaded certificate material encrypted for users who choose automatic server-side signing.

---

## What It Does

1. **Local shack integration** — UDP listeners for WSJT-X, JS8Call, N1MM+, etc.
2. **LoTW bridge** — signs and uploads QSOs to LoTW using the user\'s local tQSL cert
3. **Rig control awareness** — reads frequency/mode from Flrig/Hamlib for auto-population
4. **Offline buffer** — queues QSOs when server is unreachable, syncs when back
5. **Setup wizard** — helps configure WSJT-X, JS8Call, etc. to broadcast UDP to us
6. **Local log view** — cached copy of your logbook for quick reference when offline

---

## Authentication: OAuth2 PKCE via Zitadel

The desktop client authenticates using the **Authorization Code + PKCE flow** via the system browser. No embedded browser; the OS browser handles all credential input.

### How it works

1. Desktop client generates a PKCE code verifier (43+ chars of crypto random) and code challenge (S256)
2. Opens the system browser to the Zitadel authorization endpoint with:
   - `response_type=code`
   - `redirect_uri=http://127.0.0.1:{random_port}/callback` (loopback)
   - `code_challenge=...` (S256)
   - `state=...` (cryptographically random; prevents CSRF on the OAuth flow)
3. Client binds an ephemeral local HTTP listener on the random port
4. User authenticates in the browser (Zitadel handles MFA, social login, etc.)
5. Browser redirects to `http://127.0.0.1:{port}/callback?code=...&state=...`
6. Client validates `state` matches what it sent (CSRF check)
7. Client exchanges code + code verifier for tokens via Zitadel token endpoint
8. Listener closes immediately after receiving the callback

**Why loopback redirect, not custom URI scheme:**
Custom URI schemes (`radioledger://callback`) are interceptable on macOS and Windows — any application can register the same scheme. A loopback redirect binds a specific port at the moment of auth; the OAuth server validates the redirect_uri against a registered pattern `http://127.0.0.1:*/callback`. This is the IETF-recommended approach (RFC 8252 §7.3).

### Token scope minimization

The desktop client\'s refresh token is scoped to:
- `qsos:read`
- `qsos:write`
- `sync:status`
- `logbooks:read`

It cannot change account settings, billing, or admin functions. A stolen token has limited blast radius.

---

## Token Storage: OS Keychain

Tokens are stored in the OS native credential store. **Never** in config files, SQLite, or environment variables.

| Platform | Storage | API |
|----------|---------|-----|
| macOS | Keychain | `Security.framework` (Tauri has bindings via `tauri-plugin-store` + keychain) |
| Windows | Credential Manager | DPAPI via Tauri |
| Linux | libsecret (GNOME Keyring / KDE Wallet) | `libsecret` or `keytar` |

The configuration file (`~/.radioledger/config.yaml`) stores settings like server URL, tQSL path, and UDP port configs. It does **not** store tokens, passwords, or API keys.

---

## Local Data Security

The local SQLite cache holds a copy of the logbook for offline use.

- **Encrypted:** Use SQLCipher (encrypted SQLite). Encryption key stored in the OS keychain alongside OAuth tokens.
- **Logout behavior:** On sign-out, delete the local SQLite database. Offer a "Keep local copy" option for users who want offline access post-logout.
- **No credentials in SQLite:** Sync service credentials (QRZ key, eQSL password) are server-side only. The client never has plaintext credentials.

---

## UDP Listener Design

The desktop client listens on UDP ports for incoming QSO data from logging software running on the same machine.

### Default binding: 127.0.0.1 (loopback only)

**This is the single most important security decision for the UDP listener.**

WSJT-X, JS8Call, and N1MM+ all run on the same machine as the desktop client and send to localhost. Binding to `0.0.0.0` exposes the UDP listener to the entire network — anyone on the LAN (coffee shop Wi-Fi, hamfest table, hotel) could inject fake QSOs.

```yaml
udp:
  wsjtx:
    enabled: true
    port: 2237
    bind: "127.0.0.1"    # DEFAULT. Change only if WSJT-X runs on a different machine.
    auto_log: true
  js8call:
    enabled: false
    port: 2242
    bind: "127.0.0.1"
  n1mm:
    enabled: false
    port: 12060
    bind: "127.0.0.1"
```

If the user runs WSJT-X on a separate machine (uncommon but valid — e.g., a Raspberry Pi radio station), they can set `bind: "0.0.0.0"` and `multicast: true` manually in config. A prominent warning appears in the UI when anything other than `127.0.0.1` is configured.

### Multicast support (advanced)

For setups where multiple logging programs on the same LAN need to share QSO data, the client supports multicast group membership. Configurable in `config.yaml`; disabled by default. The UI warns when multicast is enabled that all devices on the LAN segment can send data.

### Packet validation

The WSJT-X binary protocol has defined message types and field lengths. Reject malformed packets strictly:

- Magic number check: `0xADBCCBDA` at bytes 0-3
- Explicit length bounds on every field — no pointer arithmetic
- Only accept recognized message types (5 = QSO Logged, 12 = ADIF Record)
- Rate limit: if receiving more than 10 QSO-logged messages per second, drop excess and log a warning (injection attack or runaway script)

### QSO sanity checks before forwarding to server

Even from trusted sources, validate before sending to the API:
- Callsign format is plausible (basic alphanumeric + /)
- Frequency is in amateur band allocations
- Date/time is within the last 24 hours (with timezone tolerance)
- Mode is a recognized amateur mode

This catches both injection attacks and occasional WSJT-X bugs that produce garbage output.

---

## UDP Sources

### WSJT-X / JTDX

- Protocol: Custom binary UDP (documented in WSJT-X source)
- Default port: 2237
- Key messages:
  - **Type 5** (QSO Logged) — primary auto-log trigger
  - **Type 12** (ADIF Record) — more complete data, only on manual log entry
- Sequence number (in each message) stored as `source_id` for deduplication
- JTDX (popular European fork) uses compatible but slightly different message structure — test both
- FT8 note: QSOs happen in 15-second protocol slots (even :00/:30, odd :15/:45). Set logbook `dedup_window_seconds = 30` for FT8 logbooks.

### JS8Call

- Compatible with WSJT-X UDP protocol (port typically 2242)
- Additional JS8-specific message types

### N1MM+ (Contest Logging)

- UDP broadcast, default port 12060
- XML format: ContactInfo, ContactReplace, RadioInfo
- Full contest integration (scores, multipliers, dupe checking)

### Flrig / Hamlib

- CAT control data: current frequency, mode, power
- Auto-populates operating parameters for new QSOs
- Flrig: XML-RPC (port 12345 by default)
- Hamlib rigctld: TCP socket (port 4532 by default)

---

## LoTW Integration (Local Signing)

### Why local signing

- ARRL requires QSOs be signed with a tQSL certificate tied to callsign + location
- The private key should never leave the user\'s machine
- RadioLedger takes no liability for ARRL signing credentials

### Flow

```
QSO logged → API server marks QSO as "pending LoTW upload"
                ↓
           Desktop client sees pending QSOs on next sync poll
                ↓
           Client signs QSO batch locally using tQSL binary
                ↓
           Client uploads signed ADIF directly to LoTW servers
                ↓
           Client reports upload result to API server
                ↓
           Server updates sync_status: uploaded | error
                ↓
           (Later) Client polls LoTW for inbound confirmations
                ↓
           Confirmations reported back to server → notifications fired
```

### Certificate management

- Client detects existing tQSL installation and available certs
- Setup wizard helps users who haven\'t set up tQSL yet
- Multiple callsign/location combos supported (common for POTA activators — each park may need a different tQSL location)
- Desktop-local cert expiry: client reads expiry from tQSL and pushes the DATE ONLY (not cert content) to the server. Server fires notification at 60, 30, 7 days before expiry.
- Hosted LoTW Vault mode stores uploaded certificate material encrypted for users who choose automatic server-side signing. Users who do not want RadioLedger to store certificate material should use desktop-local signing.

### tQSL integration strategy

- Phase 1: shell out to `tqsl` binary (simpler, user already has it, proven)
- Phase 2+: explore native Rust signing using the p12 cert directly (no dependency, but more complex key handling)

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     Desktop Client                            │
│                                                              │
│  ┌────────────────┐  ┌──────────────┐  ┌─────────────────┐  │
│  │ UDP Listeners  │  │ REST/WS      │  │ LoTW Signer     │  │
│  │ (WSJT-X, JS8, │  │ Client       │  │ (local tQSL)    │  │
│  │  N1MM+)        │  │ (↔ server)   │  │                 │  │
│  │ Bind: 127.0.0.1│  │             │  │ cert stays here │  │
│  └───────┬────────┘  └──────┬───────┘  └────────┬────────┘  │
│          │                  │                    │           │
│          ▼                  ▼                    ▼           │
│  ┌──────────────────────────────────────────────────────┐    │
│  │            Local Cache (SQLCipher-encrypted SQLite)   │    │
│  │  - Synced logbook copy                                │    │
│  │  - Offline QSO queue                                  │    │
│  │  - Operating preferences                              │    │
│  │  - Callsign lookup cache                              │    │
│  └──────────────────────────────────────────────────────┘    │
│                                                              │
│  ┌────────────┐   ┌──────────────┐   ┌───────────────────┐  │
│  │ CAT/Rig    │   │ System Tray  │   │ Setup Wizard      │  │
│  │ Interface  │   │ + Mini UI    │   │                   │  │
│  └────────────┘   └──────────────┘   └───────────────────┘  │
│                                                              │
│  Tokens: OS Keychain (not config files, not SQLite)          │
└──────────────────────────────────────────────────────────────┘
```

---

## Tech Stack: Tauri

- **Rust backend:** UDP sockets, tQSL subprocess integration, rig control, OS keychain access
- **Web frontend:** Svelte or Vue for settings UI and local log view
- **Why Tauri over Electron:** ~5MB binary vs 100MB+; native system tray on all platforms; Rust safety properties help with the UDP parser; hams often run older hardware — footprint matters
- **Auto-updates:** Tauri built-in updater with Ed25519 signatures. Updates served over HTTPS only. Public key embedded in binary (not replaceable by MITM). Platform code signing: macOS notarization, Windows Authenticode, Linux AppImage.

---

## Setup Wizard

First-run experience:

1. **Login** — OAuth via system browser (PKCE flow above); token stored in OS keychain
2. **Sync** — pull down existing logbook from server
3. **Detect software** — scan for WSJT-X, JS8Call, Flrig on common ports
4. **Configure UDP** — if WSJT-X detected, offer to configure its UDP output to `127.0.0.1:2237`
5. **LoTW** — detect tQSL installation, select cert and station location for auto-signing
6. **Rig control** — detect Flrig/Hamlib, configure for frequency/mode tracking
7. **Done** — show system tray icon, summarize connections

---

## Configuration

```yaml
# ~/.radioledger/config.yaml (auto-generated; editable)
# IMPORTANT: this file does NOT contain tokens, passwords, or API keys.
# All credentials are stored in the OS keychain.

server:
  url: "http://localhost:8080"         # or your self-hosted URL

udp:
  wsjtx:
    enabled: true
    port: 2237
    bind: "127.0.0.1"            # SECURITY: do not change without understanding the risk
    auto_log: true
    confirm_before_log: false    # set true to require user click for each QSO
  js8call:
    enabled: false
    port: 2242
    bind: "127.0.0.1"
  n1mm:
    enabled: false
    port: 12060
    bind: "127.0.0.1"

rig:
  flrig:
    enabled: true
    host: "localhost"
    port: 12345

lotw:
  tqsl_path: "/usr/local/bin/tqsl"   # auto-detected
  auto_sign: true
  cert_callsign: "W5XXX"
  station_locations:
    - name: "Home"
      grid: "EM35"
    - name: "POTA Portable"
      grid: ""                        # set per-activation

sync:
  interval_seconds: 300
  offline_queue: true

display:
  show_decodes: false
  notifications: true
```

---

## Open Questions

- [ ] Should the client have a full log entry form or show the synced log read-only with edit links to web UI?
- [ ] tQSL integration: shell out to tQSL binary (Phase 1) or implement native Rust signing (Phase 2+)?
- [ ] Auto-update release channel: stable only, or stable + beta opt-in?
- [ ] Windows: integrate migration wizard for HRD Logbook users?
- [ ] Contest mode: desktop client contest UI or defer entirely to N1MM+/web UI?
