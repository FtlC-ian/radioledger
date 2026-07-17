# WSJT-X UDP Research (Existing Implementation)

## API Status
- **Status**: Implemented (Desktop/Tauri)
- **Source**: `desktop/src-tauri/src/udp.rs` (1,005 lines)

## Protocols Implemented
- **WSJT-X UDP**: Listens for UDP messages from the WSJT-X digital mode software.
- **Port**: Typically listens on `localhost:2237`.

## Available Operations (Internal Tauri Commands)
- **Decode Messages**: Listens for `Heartbeat`, `Status`, `Decode`, `QSOLogged`, `Close`.
- **Log QSOs**: Receives a `QSOLogged` message when the user logs a contact in WSJT-X.
- **Update Frequency/Mode**: Receives a `Status` message containing the current frequency and mode from WSJT-X.

## Implementation Details
- **UDP Listener**: Spawns a background thread or task to listen for incoming UDP packets.
- **Packet Parsing**: Parses the binary WSJT-X packet format (schema documented in `NetworkMessage.hpp`).

## Working Open-Source Implementations (Reference)
1. **WSJT-X (Joe Taylor, K1JT)**: The original software (Fortran/C++). [https://physics.princeton.edu/pulsar/k1jt/wsjtx.html](https://physics.princeton.edu/pulsar/k1jt/wsjtx.html)
2. **PyWSJTX** (Python): [https://github.com/vsergeev/pywsjtx](https://github.com/vsergeev/pywsjtx)
3. **WSJT-X UDP Go** (Go): [https://github.com/m0vfc/wsjtx-udp](https://github.com/m0vfc/wsjtx-udp)

## Code Snippet (RadioLedger Rust/Tauri Example)
```rust
// Internal Rust/Tauri UDP message handler
fn handle_wsjtx_message(msg: WsjtxMessage) {
    match msg {
        WsjtxMessage::Status(status) => {
            // Update UI with frequency and mode
            println!("WSJT-X Frequency: {}", status.freq);
        },
        WsjtxMessage::Logged(qso) => {
            // Auto-log to RadioLedger
            println!("QSO Logged: {}", qso.call);
        },
        _ => {}
    }
}
```

## Known Gotchas
- **Multiple Instances**: If the user runs multiple copies of WSJT-X, they may use different UDP ports (e.g., `2238`, `2239`). RadioLedger should support configurable ports.
- **Binary Format**: The WSJT-X binary protocol is complex and has changed across versions. Ensure the implementation handles the latest schema.
- **Firewall Issues**: Windows Firewall often blocks the UDP port; the app must request permission or guide the user to allow it.
- **Syncing with Rig Control**: If both WSJT-X and RadioLedger are trying to control the radio, they may conflict. WSJT-X is typically the "master" during FT8/FT4 sessions.
