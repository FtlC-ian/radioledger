# eQSL Sync

> Connect RadioLedger to eQSL.cc for electronic QSL card exchange and confirmation tracking.

## Overview

eQSL.cc is an electronic QSL card exchange system. RadioLedger syncs with eQSL bidirectionally: new QSOs upload automatically, and incoming eQSL confirmations update your log.

eQSL uses its own "Authenticated" status system — QSOs between eQSL authenticated members carry more weight for some awards.

## Prerequisites

- An eQSL.cc account (free)
- Your eQSL username and password

## Setup

1. In RadioLedger, go to **Settings → Connected Services → eQSL**
2. Enter your eQSL username and password
3. Click **Connect**

Your eQSL credentials are stored encrypted — see [Security](../self-hosting/security.md).

TODO: Screenshot of eQSL setup panel.

## What Gets Synced

| Direction | What |
|-----------|------|
| RadioLedger → eQSL | New QSOs uploaded as eQSL cards sent |
| eQSL → RadioLedger | Incoming eQSL confirmations (cards received) |

## Rate Limits

eQSL has aggressive rate limits. RadioLedger enforces conservative upload intervals and backs off automatically if rate limited. Expect uploads within 15 minutes of logging.

## eQSL Authenticated Status

eQSL distinguishes "Authenticated" QSOs (where both operators are eQSL members with verified callsigns) from non-authenticated. RadioLedger tracks this distinction in the QSL status.

TODO: Document authenticated vs. non-authenticated status fields.

## Troubleshooting

**"Login failed"**: Double-check your eQSL username and password. Note that eQSL uses username (not email) for login.

**Uploads taking a long time?** eQSL rate limits are aggressive. RadioLedger queues and uploads in batches.

**Missing confirmations?** eQSL inbox poll runs 4x daily. Check Settings → Sync → eQSL for last poll time.

## Related

- [Sync Overview](index.md)
- [QSL Management](../user-guide/qsl-management.md)
