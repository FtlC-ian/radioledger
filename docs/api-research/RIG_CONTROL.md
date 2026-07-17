# Rig Control Research (Existing Implementation)

## API Status
- **Status**: Implemented (Desktop/Tauri)
- **Source**: `desktop/src-tauri/src/rig/mod.rs` (609 lines)

## Protocols Implemented
- **Flrig**: Communicates with the `flrig` software via XML-RPC.
- **Hamlib (rigctld)**: Communicates with `rigctld` via a TCP socket.

## Available Operations (Internal Tauri Commands)
- **get_freq**: Fetch the current operating frequency.
- **set_freq**: Set the radio frequency.
- **get_mode**: Fetch the current mode (USB, LSB, CW, FT8, etc.).
- **set_mode**: Set the radio mode.
- **get_ptt**: Check PTT status.
- **set_ptt**: Toggle PTT on/off.

## Implementation Details
- **Flrig XML-RPC**: Uses the `reqwest` crate for XML-RPC calls (e.g., `rig.get_vfo`, `rig.set_vfo`).
- **Hamlib TCP**: Connects to `localhost:4532` (default) and sends simple text commands like `f` (freq) and `m` (mode).

## Working Open-Source Implementations (Reference)
1. **Flrig (W1HKJ)**: The source for flrig itself (C++). [http://www.w1hkj.com/](http://www.w1hkj.com/)
2. **Hamlib (N2ADL)**: The definitive radio control library (C). [https://github.com/Hamlib/Hamlib](https://github.com/Hamlib/Hamlib)
3. **RigCtlPy** (Python): [https://github.com/vsergeev/rigctlpy](https://github.com/vsergeev/rigctlpy)

## Code Snippet (RadioLedger Rust/Tauri Example)
```rust
// Internal Rust/Tauri command to set frequency via Hamlib
#[tauri::command]
pub async fn set_frequency(freq: u64, state: tauri::State<'_, RigState>) -> Result<(), String> {
    let mut rig = state.lock().await;
    rig.set_freq(freq).await
}
```

## Known Gotchas
- **Multiple Radio Management**: Currently, RadioLedger's implementation assumes a single radio connection per session.
- **Rigctld Stability**: Hamlib's `rigctld` is powerful but can be unstable if the radio is disconnected during a session. Ensure graceful reconnects.
- **XML-RPC Overhead**: Flrig's XML-RPC is slower than Hamlib's TCP; for high-speed frequency tracking (e.g., during a contest), Hamlib is preferred.
- **Permissions (macOS/Linux)**: Serial port access may require the user to be in the `dialout` or `uucp` group.
