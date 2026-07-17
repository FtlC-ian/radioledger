# QSL Management

> Track QSL status for each QSO and manage paper QSL card batches for bureau and direct mail.

## QSL Overview

A QSL card (or electronic equivalent) is proof that a QSO took place — required for many awards. RadioLedger tracks QSL status for each QSO across all confirmation methods:

| Method | How it's tracked |
|--------|-----------------|
| **LoTW** | Automatic via desktop client sync |
| **QRZ Logbook** | Automatic via QRZ sync |
| **eQSL** | Automatic via eQSL sync |
| **Paper (bureau)** | Manually tracked with batch workflow |
| **Paper (direct)** | Manually tracked |

## QSL Status Per QSO

Each QSO has independent QSL status for each method:

| Status | Meaning |
|--------|---------|
| Not sent | No QSL sent via this method |
| Sent | QSL sent (you've uploaded/mailed) |
| Confirmed | The other station has confirmed the QSO |

You can see QSL status in the logbook list view (configurable columns) and in the QSO detail view.

## Electronic QSL (Automatic)

For LoTW, QRZ, and eQSL, status updates automatically when sync runs. See [Sync Services](../sync/index.md) for setup.

## Paper QSL Workflow

RadioLedger provides a paper QSL batch workflow for operators who exchange physical cards.

### Setting Up QSL Routes

Before creating batches, configure QSL routes for callsigns you contact frequently:

1. Go to **QSL → Routes**
2. Add a route for a callsign: Bureau, Direct, or Manager
3. Manager callsigns can be imported from Club Log data

TODO: Describe QSL route management UI.

### Creating an Outgoing Batch

1. Go to **QSL → Outgoing Batches**
2. Click **+ New Batch**
3. Select QSOs to include (use filters to find unconfirmed QSOs)
4. Set batch type: **Bureau** or **Direct**
5. Click **Create Batch**

The batch status progresses through: Draft → Ready to Print → Printed → Sent → Closed

TODO: Describe each batch state and what triggers transitions.

### Printing QSL Cards

TODO: Describe QSL card print layout options or integration with card printing services.

### Receiving Incoming QSL Cards

When you receive a paper QSL card:

1. Go to **QSL → Incoming**
2. Search for the QSO (by callsign and date)
3. Mark as **Received** and optionally enter the card details (via, date received)

### Bureau Workflow

If you use the ARRL bureau or another QSL bureau:

1. Cards you send go out in **Bureau** batches
2. Incoming bureau cards are marked manually when they arrive
3. Bureau wait times can be months to years — RadioLedger tracks the full lifecycle

TODO: Document bureau-specific workflow details.

## QSL Summary

The QSL summary (under Awards → QSL Status) shows:

- Total QSOs needing QSL
- QSOs confirmed by method
- Pending batches
- Recent incoming confirmations

## Related

- [Awards Tracking](awards-tracking.md)
- [LoTW Setup](../sync/lotw.md)
- [eQSL Setup](../sync/eqsl.md)
- [QRZ Setup](../sync/qrz.md)
