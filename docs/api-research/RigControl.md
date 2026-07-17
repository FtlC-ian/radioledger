# Rig Control API Research

## Official Documentation
- **Hamlib rigctld**: `https://hamlib.github.io/`
- **flrig XML-RPC**: `http://www.w1hkj.com/flrig-help/xmlrpc_commands.html`

## Rig Control Protocols

### 1. Hamlib rigctld (TCP/UDP)
Hamlib is the de-facto standard for rig control in open-source radio software.
- **Port**: `4532` (default TCP).
- **Protocol**: Simple text commands.
- **Operations**:
  - `f`: Get current frequency (Hz).
  - `F <hz>`: Set frequency.
  - `m`: Get current mode.
  - `M <mode> <width>`: Set mode (e.g., `M USB 2400`).

### 2. flrig XML-RPC (HTTP)
flrig (by W1HKJ) provides a user-friendly UI and an XML-RPC server for remote control.
- **Port**: `12345` (default HTTP).
- **Protocol**: XML-RPC.
- **Operations**:
  - `main.get_frequency`: Returns frequency as double (Hz).
  - `main.set_frequency(d)`: Sets frequency.
  - `rig.get_mode`: Returns mode string (e.g., `USB`).
  - `rig.set_mode(s)`: Sets mode.

### 3. OmniRig (Windows Only)
OmniRig is widely used in Windows-only ham apps.
- **Protocol**: COM/OLE objects.
- **Note**: Not natively supported by RadioLedger (cross-platform focus), but often requested.

## Working Implementations
1.  **[k0swe/go-hamlib](https://github.com/k0swe/go-hamlib)** (Go)
    - Active (2024) library for controlling Hamlib rigctld.
2.  **[go-flrig](https://github.com/m0vfc/go-flrig)** (Go)
    - Go client for flrig's XML-RPC interface.

## Code Snippet (Go - rigctld)
```go
// Using k0swe/go-hamlib
client := hamlib.NewRig("localhost", 4532)
freq, _ := client.GetFrequency()
client.SetFrequency(14074000)
```

## Code Snippet (Go - flrig)
```go
// Using XML-RPC directly or go-flrig
client := flrig.NewClient("http://localhost:12345")
freq, _ := client.GetFrequency()
client.SetFrequency(7074000)
```

## Rate Limits & Guidelines
- **Polling**: Limit status polling (frequency/mode) to **once every 250ms** to avoid lagging the radio's serial interface.
- **Feedback Loop**: When the user changes frequency on the radio, the app should update its UI; when the user changes frequency in the app, it should update the radio.

## Gotchas
- **Daemon vs. Direct**: RadioLedger should focus on connecting to **rigctld** or **flrig** rather than implementing individual radio serial protocols (like CI-V or CAT) directly. This offloads hardware compatibility to proven tools.
- **Latency**: High-latency connections (e.g., remote stations) may cause synchronization issues between the app and the radio.
