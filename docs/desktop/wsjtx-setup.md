# WSJT-X Setup

> Configure WSJT-X to send QSOs to RadioLedger automatically via UDP.

RadioLedger's desktop client listens for UDP broadcasts from WSJT-X. When WSJT-X logs a QSO (you click "Log QSO"), the QSO appears in RadioLedger within seconds — no manual entry required.

## How It Works

WSJT-X broadcasts QSO data over UDP when you log a contact. The RadioLedger desktop client listens on UDP port 2237 (by default) on `127.0.0.1` (loopback only).

**Security note:** The UDP listener binds to `127.0.0.1` by default. This means only software on the same machine can send it data. Do not change this to `0.0.0.0` unless WSJT-X runs on a separate machine — see [UDP Listener Security](#udp-listener-security).

## Setup

### Step 1: Desktop Client Setup Wizard (Automatic)

The desktop client setup wizard detects WSJT-X on its default port and offers to configure everything automatically. If you ran the wizard and accepted, WSJT-X is already configured. Skip to [Verification](#verification).

### Step 2: Configure WSJT-X Manually

In WSJT-X: **File → Settings → Reporting** tab

| Setting | Value |
|---------|-------|
| **UDP Server** | `127.0.0.1` |
| **UDP Server port number** | `2237` |
| **Accept UDP requests** | ☑ Checked |
| **Notify on accepted UDP request** | Optional |

TODO: Screenshot of WSJT-X reporting settings dialog.

### Step 3: Configure RadioLedger Desktop Client

In the desktop client: **Settings → UDP → WSJT-X**

| Setting | Default | Description |
|---------|---------|-------------|
| **Enabled** | ✓ | Enable WSJT-X UDP listener |
| **Port** | 2237 | Must match WSJT-X setting |
| **Bind address** | 127.0.0.1 | See security note above |
| **Auto-log** | ✓ | Log QSOs automatically on receipt |
| **Confirm before log** | ✗ | Require click to confirm each QSO |

## Verification

1. Make a test QSO (or use WSJT-X's "Tx even/1st" mode to confirm the connection)
2. Log a QSO in WSJT-X (click "Log QSO")
3. The QSO should appear in RadioLedger within 1-2 seconds

If running RadioLedger web UI, refresh the logbook view to see the new QSO.

TODO: Screenshot of a QSO received from WSJT-X.

## WSJT-X Message Types

RadioLedger handles these WSJT-X UDP message types:

| Message type | When sent | What RadioLedger does |
|-------------|-----------|----------------------|
| **Type 5** (QSO Logged) | When you click "Log QSO" | Logs the QSO |
| **Type 12** (ADIF Record) | Manual log entry with full ADIF | Logs with complete data |
| **Type 1** (Heartbeat) | Periodically | Confirms connection is alive |

The sequence number in each message is stored to prevent duplicate logging.

## JTDX (Popular Fork)

JTDX uses a compatible UDP protocol. Use the same configuration — port 2237, same bind address. JTDX's message structure has minor differences that RadioLedger handles automatically.

## FT8 Deduplication

FT8 QSOs happen in 15-second protocol slots (even-second and odd-second timing). If WSJT-X sends a QSO and your network is slow, you might receive it twice. RadioLedger uses the sequence number and a 30-second deduplication window to prevent duplicate entries.

## Multiple Instances of WSJT-X

If you run multiple WSJT-X instances (e.g., SO2R), each needs a different UDP port. Configure one instance on port 2237 and another on 2238. Add both ports in RadioLedger desktop settings.

TODO: Document multi-instance WSJT-X configuration.

## UDP Listener Security

The default `127.0.0.1` binding means only local software can send QSOs. This protects against:
- LAN injection attacks (someone at a hamfest sending fake QSOs)
- Malware on your network sending false QSO data

**Only change to `0.0.0.0` if WSJT-X runs on a different machine** (e.g., a Raspberry Pi radio station on your LAN). When you do, RadioLedger shows a prominent warning. All QSO data is validated even from trusted sources.

## Troubleshooting

**QSOs not appearing?**
1. Verify the desktop client is running (system tray icon present)
2. Check the UDP port matches in both WSJT-X and desktop client settings
3. Check RadioLedger desktop client logs: **Help → Show Logs**
4. Try toggling WSJT-X's UDP server off and back on

**"Address already in use" error?**
Another application is using port 2237. Change RadioLedger to a different port (e.g., 2238) and update WSJT-X to match.

**QSOs appearing twice?**
Deduplication window may be too short. Increase it in logbook settings.

TODO: More troubleshooting scenarios — see [Desktop Troubleshooting](troubleshooting.md).

## Related

- [Desktop Client Overview](index.md)
- [JS8Call Setup](js8call-setup.md)
- [N1MM+ Setup](n1mm-setup.md)
- [Troubleshooting](troubleshooting.md)
