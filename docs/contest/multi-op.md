# Multi-Operator Contest Configuration

> Configure a contest session for multi-operator (MULTI-SINGLE or MULTI-MULTI) operations.

## Overview

For multi-op contests, RadioLedger supports:

- Multiple operators logging from a shared contest session
- Per-operator attribution for each QSO
- Operating windows (for multi-single run/mult distinctions)
- Real-time score updates visible to all operators

## Setting Up Multi-Op

### Step 1: Create the Contest Session

Create the contest session with the appropriate category:
- **MULTI-SINGLE** — multiple operators, one signal at a time
- **MULTI-MULTI** — multiple operators, multiple simultaneous signals

See [Contest Setup](setup.md).

### Step 2: Add Operators

In the contest session settings:

1. Click **Operators**
2. Add each operator by their RadioLedger account or callsign
3. Assign roles if needed (e.g., run vs. mult operator)

Each operator logs in with their own RadioLedger account and joins the shared contest session.

TODO: Document how operators join a shared session (invite link? Session code?).

### Step 3: Configure Operating Windows (MULTI-SINGLE)

For MULTI-SINGLE contests that require tracking operating time per band:

1. Click **Operating Windows**
2. Define which operators are on which band at which times

TODO: Document operating window tracking in detail.

## Real-Time Collaboration

All operators see the same QSO list and score in real time via WebSocket. Duplicate checking is global — if Operator A works W1AW on 20m SSB, Operator B sees that callsign as a dupe on 20m SSB.

## Per-QSO Operator Attribution

Each QSO records the operator who logged it. The Cabrillo export includes operator callsigns in the QSO records.

## Cabrillo Export for Multi-Op

The Cabrillo export includes:
- `CATEGORY-OPERATOR: MULTI-SINGLE` (or MULTI-MULTI)
- `OPERATORS:` list of all operator callsigns
- Per-QSO operator field

See [Cabrillo Export](cabrillo-export.md).

## Related

- [Contest Setup](setup.md)
- [Cabrillo Export](cabrillo-export.md)
- [N1MM+ Integration](n1mm-integration.md)
