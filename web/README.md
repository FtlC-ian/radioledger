# RadioLedger Web UI (Quasar)

Quasar + Vue 3 + TypeScript skeleton for RadioLedger.

## Tech

- Vue 3 (Composition API)
- Quasar Framework
- TypeScript (strict)
- Pinia
- Axios API client

## Development

```bash
corepack enable
corepack prepare pnpm@11.3.0 --activate
pnpm install
pnpm run dev
```

## Build

```bash
pnpm run build
```

By default the API client points to `http://localhost:9091`.
Override with `RADIOLEDGER_API_BASE_URL`.
