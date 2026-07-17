//! Centralised error type for the RadioLedger desktop application.
//!
//! All subsystem errors are mapped to this type so they can be returned from
//! Tauri command handlers (which require `serde::Serialize` on errors).

use serde::Serialize;
use thiserror::Error;

/// Top-level application error.
#[derive(Debug, Error, Serialize)]
pub enum AppError {
    #[error("Database error: {0}")]
    Database(String),

    #[error("Auth error: {0}")]
    Auth(String),

    #[error("UDP error: {0}")]
    Udp(String),

    #[error("Sync error: {0}")]
    Sync(String),

    #[error("Rig error: {0}")]
    Rig(String),

    #[error("Keychain error: {0}")]
    Keychain(String),

    #[error("Configuration error: {0}")]
    Config(String),

    #[error("Network error: {0}")]
    Network(String),

    #[error("{0}")]
    Other(String),
}

impl From<rusqlite::Error> for AppError {
    fn from(e: rusqlite::Error) -> Self {
        AppError::Database(e.to_string())
    }
}

impl From<anyhow::Error> for AppError {
    fn from(e: anyhow::Error) -> Self {
        AppError::Other(e.to_string())
    }
}
