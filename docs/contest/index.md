# Contest Logging

> RadioLedger supports contest logging natively — not as an afterthought.

Contest logging in RadioLedger provides real-time dupe checking, exchange tracking, multiplier counting, and Cabrillo export. It integrates with N1MM+ for those who prefer a dedicated contest logger.

## Overview

Contest logging is modeled in the database as first-class entities (not just ADIF fields):

- **Contest session** — your operating session with Cabrillo metadata
- **Contest exchange** — sent/received serial number and exchange per QSO
- **Multipliers** — normalized multiplier ledger for live scoring
- **Roster** — multi-operator positions and operating windows

## Supported Workflows

| Workflow | Description |
|----------|-------------|
| **Native contest logging** | Use RadioLedger's web or desktop UI for the contest |
| **N1MM+ integration** | Use N1MM+ for the contest, receive QSOs in RadioLedger |
| **Post-contest import** | Log the contest in any logger, import ADIF afterward |
| **Cabrillo export** | Export for any standard contest |

## In This Section

| Guide | What it covers |
|-------|---------------|
| [setup.md](setup.md) | Setting up a contest session |
| [multi-op.md](multi-op.md) | Multi-operator configuration |
| [cabrillo-export.md](cabrillo-export.md) | Exporting Cabrillo files for submission |
| [n1mm-integration.md](n1mm-integration.md) | Using N1MM+ with RadioLedger |

## Contest Database Model

RadioLedger's contest support is based on explicit database tables:

- `contests` — canonical contest catalog (name, exchange schema, scoring rules)
- `contest_sessions` — your specific contest operation (category, power, operators)
- `contest_qso_exchange` — per-QSO sent/received serial + exchange + dupe flag
- `contest_multipliers` — live multiplier tracking

This allows accurate Cabrillo export without reconstructing data from ad-hoc ADIF fields.

## Quick Start

1. [Create a contest session](setup.md)
2. Start logging QSOs (dupe checking and multiplier counting runs automatically)
3. Review your score in real time
4. [Export Cabrillo](cabrillo-export.md) for submission

## Related

- [N1MM+ Desktop Setup](../desktop/n1mm-setup.md)
- [Cabrillo Export](cabrillo-export.md)
- [Import/Export (ADIF)](../user-guide/import-export.md)
