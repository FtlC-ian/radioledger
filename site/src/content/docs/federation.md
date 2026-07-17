---
title: Federation Overview
description: A design note about a possible open confirmation protocol for self-hosted RadioLedger instances.
sidebar:
  order: 6
---

> Status: design phase (coming soon)

Federation is the long-term plan for an open QSO confirmation network across RadioLedger instances.

## Vision

- Self-hosted users participate on equal footing.
- QSO confirmations can flow across instance boundaries.
- Third-party logger participation is possible via a lightweight protocol.

## High-level model

1. Instances register with an ID + public key.
2. Instances submit privacy-preserving QSO fingerprints.
3. A hub matches fingerprints and returns confirmations.
4. Each instance updates local confirmation state.

## Why this matters

- Reduces dependence on closed confirmation systems.
- Preserves self-hoster portability and ownership.
- Creates network effects without locking users in.

## Security model summary

- Ed25519-signed instance requests
- Timestamp + nonce replay protection
- Per-instance rate limits and abuse controls
- Revocation and key rotation support

## Timeline

- **Phase 1–2:** federation-ready architecture in core platform
- **Phase 3:** protocol v1 (registration, matching, confirmations)
- **Phase 4:** third-party SDK + public spec + federation dashboard

Federation is not enabled in production today. Track updates in the project roadmap and repository discussions.
