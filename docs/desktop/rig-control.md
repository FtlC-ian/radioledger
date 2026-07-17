# Rig Control

> Connect RadioLedger to Flrig or Hamlib for automatic frequency and mode tracking.

When rig control is configured, RadioLedger auto-populates the frequency and mode fields when you open the QSO entry form — one less thing to type.

## Supported Interfaces

| Software | Protocol | Default port |
|---------|----------|-------------|
| **Flrig** | XML-RPC | 12345 |
| **Hamlib rigctld** | TCP socket | 4532 |

## Flrig Setup

Flrig is a rig control program that supports hundreds of radios.

**In Flrig:**
- Verify the XML-RPC server is enabled (it is by default)
- Check the port (default: 12345)

**In RadioLedger desktop client:** Settings → Rig Control → Flrig

| Setting | Default | Description |
|---------|---------|-------------|
| **Enabled** | ✗ | Enable Flrig integration |
| **Host** | localhost | Flrig host (change if Flrig runs on another machine) |
| **Port** | 12345 | Must match Flrig's XML-RPC port |
| **Poll interval** | 500ms | How often to read frequency/mode |

TODO: Screenshot of Flrig rig control settings.

## Hamlib rigctld Setup

If you use Hamlib's `rigctld` daemon directly:

```bash
# Start rigctld for a Yaesu FT-991A on /dev/ttyUSB0
rigctld -m 1044 -r /dev/ttyUSB0 -s 38400 -t 4532
```

**In RadioLedger desktop client:** Settings → Rig Control → Hamlib

| Setting | Default | Description |
|---------|---------|-------------|
| **Enabled** | ✗ | Enable Hamlib integration |
| **Host** | localhost | rigctld host |
| **Port** | 4532 | Must match rigctld port |

## What Gets Auto-Populated

When rig control is active, the QSO entry form auto-fills:

| Field | Source |
|-------|--------|
| **Frequency** | Current VFO frequency from rig |
| **Mode** | Current mode from rig |
| **Band** | Derived from frequency |

These values update in real time as you tune. When you save the QSO, the values at the time of saving are recorded.

## Power Reading

TODO: Document whether/how TX power is read from the rig (rig-dependent capability).

## Multi-Radio Setup (SO2R)

TODO: Document multi-radio/SO2R rig control configuration.

## Troubleshooting

**"Cannot connect to Flrig"**
- Is Flrig running?
- Check the port matches (default: 12345)
- Check for firewall blocking localhost connections

**Frequency showing wrong value**
- Verify the rig is in VFO mode
- Some rigs report split frequency differently — try toggling split mode

**Mode not updating**
- Verify your radio model is supported by Flrig/Hamlib
- Check Flrig logs for mode polling errors

## Related

- [Desktop Client Overview](index.md)
- [WSJT-X Setup](wsjtx-setup.md) (WSJT-X also provides frequency/mode)
- [Troubleshooting](troubleshooting.md)
