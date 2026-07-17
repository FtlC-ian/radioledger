# LoTW Sync

> Connect RadioLedger to ARRL's Logbook of The World for automatic QSL confirmation tracking.

LoTW (Logbook of The World) is ARRL's QSL confirmation system. It's the gold standard for award confirmation and is required for most major ARRL awards (DXCC, WAS, etc.).

## How RadioLedger LoTW Sync Works

RadioLedger supports two LoTW signing models:

- **Desktop-local signing:** The desktop client signs QSO batches locally through tQSL. Certificate material stays on your machine.

The desktop-local flow:

1. You log a QSO → RadioLedger marks it "pending LoTW upload"
2. Desktop client picks up pending QSOs during its sync cycle
3. Desktop client signs the QSO batch locally using your tQSL certificate
4. Desktop client uploads the signed ADIF directly to LoTW servers
5. Desktop client reports the upload result to RadioLedger server
6. Later, desktop client polls LoTW for inbound confirmations
7. Confirmations flow back into RadioLedger → QSL status updates

## Prerequisites

- **The RadioLedger desktop client** — required for desktop-local signing. [Install the desktop client →](../desktop/index.md)
- **A tQSL certificate** — issued by ARRL for your callsign. [Apply at lotw.arrl.org](https://lotw.arrl.org) if you don't have one.
- **tQSL installed** — required for desktop-local signing. The desktop client uses it for signing.

## Setup

### Step 1: Install the Desktop Client

[→ Desktop Client Installation](../desktop/installation.md)

### Step 2: Run the Setup Wizard

On first launch, the desktop client setup wizard:

1. Detects your tQSL installation
2. Lists available certificates (callsign + location combinations)
3. Asks which certificate to use for LoTW uploads

If tQSL is not installed, the wizard provides a download link and setup instructions.

### Step 3: Select Station Location

tQSL requires you to select a **station location** for each upload. This matches your callsign to the correct DXCC entity and location for the QSO.

POTA activators: you may need separate station locations for each park. The desktop client supports multiple configured locations — select the right one before heading to the park.

### Step 4: Enable Auto-Upload

In the desktop client settings, enable **Auto-upload to LoTW**. New QSOs will be signed and uploaded automatically while the desktop client is running.

TODO: Screenshot of the LoTW configuration panel.

## Certificate Management

See [LoTW Certificates](../desktop/lotw-certificates.md) for certificate lifecycle management:

- Adding a new certificate
- Renewing an expiring certificate
- Using multiple callsigns / station locations
- What to do if your certificate expires

RadioLedger can monitor certificate expiry and notify you at 60, 30, and 7 days before expiry. Desktop-local signing can report expiry metadata without uploading certificate contents.

## Sync Status

In RadioLedger:

- **LoTW: Pending** — queued for next desktop client sync
- **LoTW: Uploaded** — signed and uploaded to LoTW
- **LoTW: Confirmed** — the other station has confirmed via LoTW
- **LoTW: Error** — upload failed (check desktop client logs)

## Rate Limits

LoTW has undocumented rate limits. RadioLedger is conservative: one request every 30 seconds. During contest weekends, LoTW may be slow or unavailable. RadioLedger uses a circuit breaker to stop retrying when LoTW is down, and resumes automatically when it recovers.

## Troubleshooting

**QSOs not uploading?**
- Is the desktop client running?
- Check Settings → Sync → LoTW for error details
- Is tQSL installed and the path configured correctly in the desktop client?

**tQSL not found?**
- Check the tQSL path in desktop client Settings → LoTW → tQSL Path
- Default paths: `/usr/local/bin/tqsl` (macOS/Linux), `C:\Program Files (x86)\tQSL\tqsl.exe` (Windows)

**Certificate invalid?**
- Check certificate expiry in tQSL
- Expired certificates cannot be used for signing — you must renew at lotw.arrl.org

**LoTW server errors?**
- LoTW can be unreliable during contest weekends. Wait and retry later.

TODO: Add more specific LoTW error codes and their meanings.

## Related

- [Desktop Client](../desktop/index.md)
- [LoTW Certificates](../desktop/lotw-certificates.md)
- [QSL Management](../user-guide/qsl-management.md)
- [Awards Tracking](../user-guide/awards-tracking.md)
