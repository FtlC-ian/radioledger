# RadioLedger Desktop

Tauri v2 desktop client for RadioLedger — the local shack bridge.

## What it does

- **WSJT-X / JTDX integration** — UDP listener on `127.0.0.1:2237` captures QSO Logged messages, queues them for server sync, and can optionally best-effort relay them to FT8Battle
- **LoTW bridge** — shells out to local `tqsl` binary; private keys never leave the user's machine
- **Offline queue** — SQLite cache buffers QSOs when the server is unreachable, auto-syncs with exponential backoff
- **OAuth2 PKCE auth** — loopback redirect to `http://127.0.0.1:{random_port}/callback`; tokens stored in OS keychain (never in config files or SQLite)
- **System tray** — Open Window / Start-Stop UDP / Sync Now / Quit

## Architecture

```
desktop/
├── src-tauri/
│   ├── src/
│   │   ├── main.rs     — entry point
│   │   ├── lib.rs      — plugin registration + app setup
│   │   ├── auth.rs     — PKCE auth flow, OS keychain token storage
│   │   ├── udp.rs      — WSJT-X UDP listener + packet parser
│   │   ├── sync.rs     — background server sync with exponential backoff
│   │   ├── db.rs       — local SQLite cache (offline QSO queue)
│   │   ├── tray.rs     — system tray icon and menu
│   │   ├── config.rs   — ~/.radioledger/config.yaml loader
│   │   └── error.rs    — unified AppError type
│   ├── Cargo.toml
│   ├── tauri.conf.json  — app id: com.radioledger.desktop
│   └── icons/
├── src/                 — modular desktop UI shell (HTML + TypeScript)
│   ├── index.html
│   ├── main.ts        — thin bootstrap/composition root
│   ├── app-shell.ts   — tab/app-shell orchestration and event wiring
│   └── style.css
├── vite.config.ts
└── package.json
```

## Security constraints

- **UDP socket binds to `127.0.0.1` only** (loopback). Never `0.0.0.0` by default. Configurable in `~/.radioledger/config.yaml` with a UI warning.
- **Tokens stored in OS keychain** (macOS Keychain, Windows Credential Manager, libsecret on Linux). Never in config files or SQLite.
- **SQLite encrypted** (SQLCipher; key in keychain). On logout, the database is deleted.
- **LoTW private keys never transmitted** to the RadioLedger server. The `tqsl` binary runs locally; only upload status is reported back.

## Getting started

### Prerequisites

- Rust (stable) — install via [rustup](https://rustup.rs)
- Node.js 20+
- pnpm 11.3.0 via Corepack
- On Linux: `libwebkit2gtk-4.1-dev libssl-dev libsecret-1-dev`

### Development

```bash
corepack enable
corepack prepare pnpm@11.3.0 --activate
pnpm install
pnpm run tauri:dev
```

### cargo check only

```bash
cd src-tauri
cargo check
```

### Build

```bash
pnpm run tauri:build
```

## Configuration

Configuration lives at `~/.radioledger/config.yaml`. It is auto-generated on first run with safe defaults (all UDP listeners bound to `127.0.0.1`). Tokens and secrets are **never** stored here. FT8Battle relay remains off by default, but its UDP destination is stored in config so the published target can be changed without code changes.

## E2E Testing

End-to-end tests use [WebdriverIO](https://webdriver.io/) with Tauri's built-in WebDriver support.

### Prerequisites

```bash
# Install tauri-driver (Rust binary — required to proxy WebDriver commands)
cargo install tauri-driver

# Build the app in debug mode first
pnpm run tauri:build -- --debug --no-bundle
```

### Running tests

```bash
pnpm run test:e2e
```

Run fast web-layer tests with mocked Tauri commands (no desktop binary required):

```bash
pnpm run test:unit
```

### Test files

| File | What it tests |
|------|--------------|
| `test/app-launch.spec.ts` | App launches, main window renders |
| `test/auth-flow.spec.ts` | Auth scaffold: login/logout surfaces and self-hosted mode routing |
| `test/navigation.spec.ts` | Sidebar navigation between pages |
| `test/qso-entry.spec.ts` | QSO entry form, field input, validation |
| `test/logbook.spec.ts` | Logbook table scaffold, sorting, filtering controls |
| `test/wsjtx-integration.spec.ts` | WSJT-X listener scaffold, status indicators, toggle behavior |
| `test/settings.spec.ts` | Settings page, UDP listener and connection controls |
| `test/tray-icon.spec.ts` | Tray automation scaffold (includes TODO placeholders for tray event hooks) |

### How it works

`wdio.conf.ts` spawns `tauri-driver` (from `~/.cargo/bin/tauri-driver`) which proxies WebDriver commands to the Tauri app's internal WebDriver endpoint. The binary path is resolved automatically — release build preferred, falls back to debug.

---

## Tauri commands (frontend API)

| Command | Returns | Description |
|---------|---------|-------------|
| `get_auth_status()` | `{logged_in, callsign}` | Current auth state |
| `login()` | `{logged_in, callsign}` | Start PKCE flow in system browser |
| `logout()` | `void` | Clear tokens from keychain |
| `get_udp_status()` | `{listening, port, packets_received}` | UDP listener state |
| `start_udp_listener(port?)` | `UdpStatus` | Start listener (default port 2237) |
| `stop_udp_listener()` | `UdpStatus` | Stop listener |
| `get_sync_status()` | `{pending, last_sync, last_error}` | Sync queue state |
| `sync_now()` | `SyncStatus` | Trigger immediate sync |
