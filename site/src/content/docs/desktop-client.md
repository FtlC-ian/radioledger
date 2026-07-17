---
title: Desktop Client Setup
description: Connect WSJT-X and LoTW workflows to RadioLedger from your local shack machine.
sidebar:
  order: 4
---

The desktop client connects your shack software to the RadioLedger server.

## What the desktop client handles

- WSJT-X / FT8 UDP auto-logging
- JS8Call and N1MM+ UDP ingestion
- LoTW signing and upload using local tQSL certs
- Rig frequency/mode reads via Flrig/Hamlib
- Offline buffering when the server is temporarily unreachable

## Security model (important)

LoTW private keys never leave your machine.

- Certificates stay local
- Signing happens locally
- Tokens are stored in OS keychain
- Local cache can be encrypted

## Quick start

1. Install desktop client for your OS.
2. Sign in via OAuth flow.
3. Confirm RadioLedger API endpoint.
4. Enable WSJT-X UDP listener.
5. Configure LoTW certificate if you use LoTW.
6. Keep the client running in tray/background.

## WSJT-X basics

- Set WSJT-X UDP output to localhost.
- Confirm client listener port matches configuration.
- Make one test contact and verify ingestion appears in your logbook.

## LoTW basics

- Import/select your local tQSL cert.
- Enable upload policy (manual or automatic).
- Confirm signed upload status in sync queue.

## Troubleshooting

- **No incoming UDP contacts:** check firewall + localhost binding + port mismatch.
- **LoTW upload errors:** verify cert validity and station location alignment.
- **Token errors after long idle:** re-authenticate to refresh local credentials.
