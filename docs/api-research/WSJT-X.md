# WSJT-X UDP Protocol Research

## Official Documentation
- **UDP Protocol Spec**: [WSJT-X UDP Protocol v1.0 (v2.1 and later)](https://sourceforge.net/p/wsjt/wsjt/ci/master/tree/Network/NetworkProtocol.md)
- **Docs**: [WSJT-X User Guide](https://physics.princeton.edu/pulsar/k1jt/wsjtx-doc/wsjtx-main-2.6.1.html)

## WSJT-X UDP Interface
WSJT-X broadcasts various messages (spots, status, heartbeats) over a configurable UDP port.
- **Port**: `2237` (default UDP).
- **Format**: Custom binary format.
- **Operations**:
  - `Heartbeat`: Sent every few seconds to identify the WSJT-X instance.
  - `Status`: Current frequency, mode, and state of WSJT-X.
  - `Decode`: Signals decoded from the waterfall.
  - `QSOLog`: Sent when the user logs a QSO in WSJT-X.
  - `Clear`: Clear the waterfall.
  - `Reply`: Send a message to WSJT-X to trigger a transmission.

## Binary Packet Structure
All WSJT-X UDP packets share a common header:
- `Magic Number`: `0xadbccbda` (4 bytes).
- `Version`: `0x00000002` (4 bytes).
- `Packet Type`: (4 bytes).
- `ID`: Opaque string identifier for the WSJT-X instance.

### Packet Types
- `0`: Heartbeat (sent by WSJT-X and clients).
- `1`: Status update.
- `2`: Decode message.
- `5`: QSO Logged (most important for RadioLedger).
- `12`: Logged ADIF (alternate format for QSO logging).

## Operations

### 1. Automatic QSO Logging
- **Packet Type**: `5` (QSOLogged).
- **Response**: Parse the packet to extract `CALL`, `BAND`, `MODE`, `REPORT_SENT`, `REPORT_RCVD`, `TX_POWER`, etc.
- **Note**: RadioLedger should listen for these packets and automatically prompt the user to "Save" or "Auto-sync" the QSO.

### 2. Spot Monitoring
- **Packet Type**: `2` (Decode).
- **Response**: Monitor the stream for "needed" callsigns or grid squares.

## Rate Limits & Guidelines
- **Throttling**: Since it's local UDP, no numerical rate limit applies, but decoding complex packets (Type 2) can be CPU intensive.
- **Feedback**: WSJT-X can be controlled by sending `Reply` (Type 6) messages.

## Working Implementations
1.  **[k0swe/wsjtx-go](https://github.com/k0swe/wsjtx-go)** (Go)
    - Active (2024) library for parsing and sending WSJT-X UDP packets.
2.  **[RadioLedger Desktop (UDP)](https://github.com/RadioLedger/RadioLedger/blob/main/desktop/src-tauri/src/udp.rs)** (Rust)
    - Existing Rust implementation in the project codebase.

## Code Snippet (Go - Listener)
```go
// Using k0swe/wsjtx-go
listener, _ := wsjtx.NewListener(":2237")
listener.Listen(func(msg interface{}) {
    switch m := msg.(type) {
    case *wsjtx.QSOLogged:
        fmt.Printf("Logged QSO with %s\n", m.Call)
    case *wsjtx.Status:
        fmt.Printf("WSJT-X is on %d Hz\n", m.DialFrequency)
    }
})
```

## Gotchas
- **Endianness**: The protocol uses Big-Endian for multi-byte values.
- **Multiple Instances**: If the user runs multiple copies of WSJT-X, they will all use unique `ID` strings but share the same UDP port.
- **Firewall**: Ensure the OS firewall allows incoming UDP traffic on port 2237.
