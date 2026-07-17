# mobile/

Cross-platform mobile app for RadioLedger.

## What Goes Here

iOS and Android application built with Flutter.

Key features:
- Offline-first QSO entry with sync to server
- OAuth2 PKCE via system browser; tokens stored in iOS Keychain / Android Keystore
- Local SQLite via Drift ORM + SQLCipher encryption
- Callsign lookup (online via QRZ/HamDB; cached for offline)
- **POTA Activation Mode** — real-time QSO count, P2P detection, multi-park support, self-spot
- **SOTA Activation Mode** — summit selection, points, self-spot to SOTAwatch
- GPS data minimization: send 4-char grid to server; exact GPS stays on device (opt-in)

## Why Flutter

- Drift ORM + offline SQLite story is excellent
- POTA community has proven Flutter apps
- Single codebase for iOS and Android
- Strong typing helps with ADIF field safety
