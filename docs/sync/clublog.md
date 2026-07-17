# ClubLog Sync

> Upload your log to ClubLog for DXCC tracking and propagation statistics.

## Overview

ClubLog is a popular online log storage and DXCC tracking service. It provides DXCC entity resolution, propagation data, and the "Most Wanted" DX entity list. RadioLedger uploads QSOs to ClubLog and can pull DXCC status data back.

## Prerequisites

- A ClubLog account (free)
- Your ClubLog API key

## Getting Your ClubLog API Key

1. Log in to ClubLog.org
2. Go to **Account → API Keys**
3. Create a new API key for RadioLedger

TODO: Verify current ClubLog API key location.

## Setup

1. In RadioLedger, go to **Settings → Connected Services → ClubLog**
2. Enter your ClubLog email address and API key
3. Click **Connect**

TODO: Screenshot of ClubLog setup panel.

## What Gets Synced

| Direction | What |
|-----------|------|
| RadioLedger → ClubLog | All new QSOs |
| ClubLog → RadioLedger | DXCC entity status (worked/confirmed) |

## Troubleshooting

**"Authentication failed"**: Verify your email address matches your ClubLog account and the API key is correct.

**QSOs not appearing in ClubLog?** Check Settings → Sync → ClubLog for error messages.

## Related

- [Sync Overview](index.md)
- [Awards Tracking](../user-guide/awards-tracking.md)
- [DXCC Entities Reference](../reference/dxcc-entities.md)
