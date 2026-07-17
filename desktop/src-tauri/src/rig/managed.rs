//! Managed rigctld subprocess.
//!
//! Spawns and supervises a `rigctld` child process based on a `RigProfile`.
//! Handles startup, health monitoring, clean shutdown, and binary detection.

use std::path::PathBuf;
use std::time::Duration;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use tokio::process::Child;
use tracing::{debug, info, warn};

use crate::config::{FlowControl, Parity, PttType, RigInterfaceType, RigProfile};

// ---------------------------------------------------------------------------
// Binary detection
// ---------------------------------------------------------------------------

/// Common locations to probe for the `rigctld` binary.
const RIGCTLD_SEARCH_PATHS: &[&str] = &[
    "/usr/bin/rigctld",
    "/usr/local/bin/rigctld",
    "/opt/homebrew/bin/rigctld",
    "/opt/homebrew/sbin/rigctld",
    "/usr/sbin/rigctld",
];

/// Common locations to probe for the `rigctl` binary.
const RIGCTL_SEARCH_PATHS: &[&str] = &[
    "/usr/bin/rigctl",
    "/usr/local/bin/rigctl",
    "/opt/homebrew/bin/rigctl",
    "/opt/homebrew/sbin/rigctl",
    "/usr/sbin/rigctl",
];

/// Find a binary by checking PATH first, then well-known locations.
fn detect_binary(name: &str, fallback_paths: &[&str]) -> Option<PathBuf> {
    // Check PATH via `which`-style lookup.
    if let Ok(output) = std::process::Command::new("which").arg(name).output() {
        if output.status.success() {
            let path = String::from_utf8_lossy(&output.stdout).trim().to_string();
            if !path.is_empty() {
                return Some(PathBuf::from(path));
            }
        }
    }

    // Try well-known locations.
    for path in fallback_paths {
        let pb = PathBuf::from(path);
        if pb.exists() {
            return Some(pb);
        }
    }

    None
}

/// Returns the path to the `rigctld` binary, or `None` if not found.
pub fn detect_rigctld() -> Option<PathBuf> {
    detect_binary("rigctld", RIGCTLD_SEARCH_PATHS)
}

/// Returns the path to the `rigctl` binary, or `None` if not found.
pub fn detect_rigctl() -> Option<PathBuf> {
    detect_binary("rigctl", RIGCTL_SEARCH_PATHS)
}

// ---------------------------------------------------------------------------
// Command-line builder
// ---------------------------------------------------------------------------

/// Build the `rigctld` argument list from a profile.
///
/// Returns an error if the profile is not a Hamlib profile or if required
/// fields (rig model) are missing.
pub fn build_rigctld_args(profile: &RigProfile, tcp_port: u16) -> Result<Vec<String>> {
    if profile.interface_type != RigInterfaceType::Hamlib {
        return Err(anyhow::anyhow!(
            "Profile interface type is not Hamlib; cannot build rigctld args"
        ));
    }

    let mut args = Vec::new();

    // Rig model number.
    args.push("-m".to_string());
    args.push(profile.rig_model_id.to_string());

    // Serial port (optional — if empty, rigctld uses a dummy backend).
    if !profile.serial_port.is_empty() {
        args.push("-r".to_string());
        args.push(profile.serial_port.clone());
    }

    // Baud rate.
    if profile.baud_rate > 0 {
        args.push("-s".to_string());
        args.push(profile.baud_rate.to_string());
    }

    // TCP listen port.
    args.push("-t".to_string());
    args.push(tcp_port.to_string());

    // CIV address (Icom).
    if let Some(civ) = &profile.civ_address {
        if !civ.is_empty() {
            args.push("-c".to_string());
            args.push(civ.clone());
        }
    }

    // PTT type.
    let ptt_str = match profile.ptt_type {
        PttType::Cat => Some("CAT"),
        PttType::Dtr => Some("DTR"),
        PttType::Rts => Some("RTS"),
        PttType::None => None,
    };
    if let Some(ptt) = ptt_str {
        args.push("-P".to_string());
        args.push(ptt.to_string());
    }

    // Serial parameters via -C flags.
    let mut serial_params: Vec<String> = Vec::new();

    if profile.data_bits != 8 {
        serial_params.push(format!("data_bits={}", profile.data_bits));
    }

    if profile.stop_bits != 1 {
        serial_params.push(format!("stop_bits={}", profile.stop_bits));
    }

    let parity_str = match profile.parity {
        Parity::None => None,
        Parity::Even => Some("Even"),
        Parity::Odd => Some("Odd"),
    };
    if let Some(p) = parity_str {
        serial_params.push(format!("parity={p}"));
    }

    let fc_str = match profile.flow_control {
        FlowControl::None => None,
        FlowControl::Hardware => Some("Hardware"),
        FlowControl::Software => Some("Software"),
    };
    if let Some(fc) = fc_str {
        serial_params.push(format!("serial_handshake={fc}"));
    }

    if !serial_params.is_empty() {
        args.push("-C".to_string());
        args.push(serial_params.join(","));
    }

    Ok(args)
}

// ---------------------------------------------------------------------------
// ManagedRigctld
// ---------------------------------------------------------------------------

/// A running `rigctld` subprocess managed by RadioLedger.
pub struct ManagedRigctld {
    child: Child,
    binary: PathBuf,
    profile: RigProfile,
    tcp_port: u16,
}

impl ManagedRigctld {
    /// Spawn `rigctld` for the given profile and wait briefly for it to start.
    ///
    /// Returns an error if `rigctld` is not found or fails to start.
    pub async fn start(profile: &RigProfile, tcp_port: u16) -> Result<Self> {
        let binary = detect_rigctld()
            .context("rigctld not found — install Hamlib (e.g. brew install hamlib)")?;

        let args = build_rigctld_args(profile, tcp_port)?;

        info!(
            "Spawning rigctld: {} {}",
            binary.display(),
            args.join(" ")
        );

        let child = tokio::process::Command::new(&binary)
            .args(&args)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .with_context(|| format!("Failed to spawn rigctld ({})", binary.display()))?;

        // Give rigctld a moment to initialize and start listening.
        tokio::time::sleep(Duration::from_millis(500)).await;

        Ok(ManagedRigctld {
            child,
            binary,
            profile: profile.clone(),
            tcp_port,
        })
    }

    /// Return `true` if the child process appears to still be running.
    pub fn is_running(&mut self) -> bool {
        match self.child.try_wait() {
            Ok(None) => true,   // Still running.
            Ok(Some(status)) => {
                debug!("rigctld exited with status: {status}");
                false
            }
            Err(err) => {
                warn!("rigctld try_wait error: {err}");
                false
            }
        }
    }

    /// The TCP port rigctld is listening on.
    pub fn tcp_port(&self) -> u16 {
        self.tcp_port
    }

    /// Stop the rigctld subprocess gracefully.
    pub async fn stop(&mut self) -> Result<()> {
        info!("Stopping managed rigctld (pid {:?})", self.child.id());

        #[cfg(unix)]
        {
            if let Some(pid) = self.child.id() {
                // Send SIGTERM for graceful shutdown.
                unsafe {
                    libc::kill(pid as libc::pid_t, libc::SIGTERM);
                }
            }
        }

        #[cfg(not(unix))]
        {
            let _ = self.child.kill().await;
        }

        // Wait up to 3 seconds for clean exit.
        match tokio::time::timeout(Duration::from_secs(3), self.child.wait()).await {
            Ok(Ok(status)) => {
                debug!("rigctld exited cleanly: {status}");
            }
            Ok(Err(err)) => {
                warn!("rigctld wait error: {err}");
            }
            Err(_) => {
                // Timeout — force kill.
                warn!("rigctld did not exit in time; force killing");
                let _ = self.child.kill().await;
                let _ = self.child.wait().await;
            }
        }

        Ok(())
    }

    /// Stop and restart the rigctld subprocess with the same profile.
    pub async fn restart(&mut self) -> Result<()> {
        self.stop().await?;

        let binary = &self.binary;
        let args = build_rigctld_args(&self.profile, self.tcp_port)?;

        info!("Restarting rigctld: {}", args.join(" "));

        let child = tokio::process::Command::new(binary)
            .args(&args)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .with_context(|| "Failed to restart rigctld")?;

        self.child = child;

        // Give rigctld a moment to initialize.
        tokio::time::sleep(Duration::from_millis(500)).await;

        Ok(())
    }
}

impl Drop for ManagedRigctld {
    fn drop(&mut self) {
        // Best-effort sync kill on drop (e.g. app shutdown).
        // The async stop() method is preferred during normal operation.
        let _ = self.child.start_kill();
    }
}

// ---------------------------------------------------------------------------
// Rig model list
// ---------------------------------------------------------------------------

/// A single rig model from `rigctl -l`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RigModel {
    pub id: u32,
    pub manufacturer: String,
    pub name: String,
    pub status: String,
}

/// Run `rigctl -l` and parse the output into a list of `RigModel` entries.
///
/// The output format is:
/// ```text
/// Rig#  Mfg                    Model                    Version          Status
///    1  Hamlib                 Dummy                    0.5              Beta
///    2  Hamlib                 NET rigctl               0.3              Stable
/// ```
pub fn parse_rigctl_list(output: &str) -> Vec<RigModel> {
    let mut models = Vec::new();

    for line in output.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with("Rig#") || trimmed.starts_with("---") {
            continue;
        }

        // Parse using fixed-column positions from Hamlib's `rigctl -l` output:
        // Col 0: Rig# (bytes 0–5)
        // Col 1: Mfg  (bytes 6–28)
        // Col 2: Model (bytes 29–52)
        // Col 3: Version (bytes 53–68)
        // Col 4: Status (bytes 69+)
        if line.len() < 6 {
            continue;
        }

        let id_str = line.get(0..6).unwrap_or("").trim();
        let mfg_str = line.get(6..29).unwrap_or("").trim();
        let model_str = line.get(29..53).unwrap_or("").trim();
        let status_str = line.get(69..).unwrap_or("").trim();

        let id: u32 = match id_str.parse() {
            Ok(v) => v,
            Err(_) => continue,
        };

        if mfg_str.is_empty() || model_str.is_empty() {
            continue;
        }

        models.push(RigModel {
            id,
            manufacturer: mfg_str.to_string(),
            name: model_str.to_string(),
            status: status_str.to_string(),
        });
    }

    models
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{FlowControl, Parity, PttType, RigInterfaceType, RigProfile};

    fn hamlib_profile() -> RigProfile {
        RigProfile {
            id: "test-id".to_string(),
            name: "Test".to_string(),
            interface_type: RigInterfaceType::Hamlib,
            rig_model_id: 229,
            rig_model_name: "TS-590SG".to_string(),
            serial_port: "/dev/cu.usbserial-0001".to_string(),
            baud_rate: 57600,
            data_bits: 8,
            stop_bits: 1,
            flow_control: FlowControl::None,
            parity: Parity::None,
            civ_address: None,
            ptt_type: PttType::Cat,
            host: "127.0.0.1".to_string(),
            port: 4532,
            tci_receiver: None,
            poll_interval_ms: 1000,
        }
    }

    #[test]
    fn builds_basic_rigctld_args() {
        let profile = hamlib_profile();
        let args = build_rigctld_args(&profile, 4532).unwrap();
        assert!(args.contains(&"-m".to_string()));
        assert!(args.contains(&"229".to_string()));
        assert!(args.contains(&"-r".to_string()));
        assert!(args.contains(&"/dev/cu.usbserial-0001".to_string()));
        assert!(args.contains(&"-s".to_string()));
        assert!(args.contains(&"57600".to_string()));
        assert!(args.contains(&"-t".to_string()));
        assert!(args.contains(&"4532".to_string()));
        assert!(args.contains(&"-P".to_string()));
        assert!(args.contains(&"CAT".to_string()));
    }

    #[test]
    fn builds_args_with_civ_address() {
        let mut profile = hamlib_profile();
        profile.civ_address = Some("94".to_string());
        let args = build_rigctld_args(&profile, 4532).unwrap();
        assert!(args.contains(&"-c".to_string()));
        assert!(args.contains(&"94".to_string()));
    }

    #[test]
    fn builds_args_with_serial_params() {
        let mut profile = hamlib_profile();
        profile.data_bits = 7;
        profile.stop_bits = 2;
        profile.parity = Parity::Even;
        profile.flow_control = FlowControl::Hardware;
        let args = build_rigctld_args(&profile, 4532).unwrap();
        assert!(args.contains(&"-C".to_string()));
        let c_idx = args.iter().position(|a| a == "-C").unwrap();
        let params = &args[c_idx + 1];
        assert!(params.contains("data_bits=7"));
        assert!(params.contains("stop_bits=2"));
        assert!(params.contains("parity=Even"));
        assert!(params.contains("serial_handshake=Hardware"));
    }

    #[test]
    fn rejects_non_hamlib_profile() {
        let mut profile = hamlib_profile();
        profile.interface_type = RigInterfaceType::Flrig;
        let result = build_rigctld_args(&profile, 4532);
        assert!(result.is_err());
    }

    #[test]
    fn parses_rigctl_list_output() {
        let sample = r#"Rig#  Mfg                    Model                    Version          Status
   1  Hamlib                 Dummy                    0.5              Beta
   2  Hamlib                 NET rigctl               0.3              Stable
 229  Kenwood                TS-590SG                 20200112.0       Stable
"#;
        let models = parse_rigctl_list(sample);
        assert!(!models.is_empty());
        let dummy = models.iter().find(|m| m.id == 1);
        assert!(dummy.is_some());
        let ts590 = models.iter().find(|m| m.id == 229);
        assert!(ts590.is_some());
        if let Some(m) = ts590 {
            assert_eq!(m.manufacturer, "Kenwood");
            assert!(m.name.contains("TS-590SG"));
        }
    }
}
