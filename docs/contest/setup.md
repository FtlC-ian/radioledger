# Contest Setup

> Configure a contest session in RadioLedger for real-time logging, dupe checking, and scoring.

## Creating a Contest Session

1. In RadioLedger, click **+ New Contest Session** from the Contests menu
2. Select the contest from the catalog
3. Enter your category information (see below)
4. Click **Start Contest**

TODO: Screenshot of contest session creation dialog.

## Contest Selection

RadioLedger includes a catalog of common contests:

TODO: List supported contests (CQ WW, CQ WPX, ARRL DX, ARRL Sweepstakes, Field Day, etc.)

If your contest isn't in the catalog, you can create a custom contest with manual exchange definition.

## Category Settings

| Field | Description | Example |
|-------|-------------|---------|
| **Callsign** | Your contest callsign | `W1AW` |
| **Category operator** | SINGLE-OP, MULTI-SINGLE, MULTI-MULTI | `SINGLE-OP` |
| **Category band** | ALL, SINGLE BAND | `ALL` |
| **Category power** | HIGH, LOW, QRP | `LOW` |
| **Category mode** | CW, SSB, MIXED, DIGITAL | `MIXED` |
| **Category overlay** | TB-WIRES, NOVICE-TECH, CLASSIC | (optional) |
| **Club** | Your club (for aggregate score) | `ARRL` |
| **Location** | Your ARRL/RAC section or grid | `CT` |
| **Operators** | Operator callsigns (for multi-op) | See [Multi-Op](multi-op.md) |

## Exchange Definition

Each contest has a defined exchange. RadioLedger pre-configures this from the contest catalog:

| Contest | Sent exchange | Received exchange |
|---------|--------------|-----------------|
| CQ WW | 59(9) + CQ zone | 59(9) + CQ zone |
| ARRL DX | 59(9) + Power | 59(9) + ARRL section |
| Sweepstakes | NR + PREC + CALL + CHECK + SEC | Same |

TODO: Document all supported contest exchange formats.

## During the Contest

The contest logging view shows:

- **QSO entry form** with exchange fields pre-configured
- **Dupe indicator** — callsign turns red if already worked on this band/mode
- **Multiplier indicator** — green if this QSO counts as a new multiplier
- **Running score** — updated after each QSO
- **Rate meter** — QSOs per hour
- **Band/mode breakdown** — QSOs per band

TODO: Screenshot of contest logging view.

## Dupe Checking

RadioLedger checks for duplicates using the contest-specific matching rules:
- Same callsign, same band (and mode if mode-specific contest)
- Dupe warning shows immediately as you type the callsign
- You can still log a dupe (accidental second contact) with a dupe flag

## Multiplier Tracking

Multipliers are tracked per the contest rules (DXCC entities, zones, sections, etc.). New multipliers show with a green indicator. The multiplier panel shows worked/needed by category.

TODO: Document multiplier panel features.

## Related

- [Multi-Operator Setup](multi-op.md)
- [Cabrillo Export](cabrillo-export.md)
- [N1MM+ Integration](n1mm-integration.md)
