# Frequently Asked Questions

> Common questions about RadioLedger.

## General

### What is RadioLedger?

RadioLedger is an open-source ham radio logging platform. It lets you log QSOs, track awards (DXCC, WAS, VUCC, POTA, SOTA), sync with LoTW/QRZ/eQSL, and integrate with shack software like WSJT-X, JS8Call, and N1MM+.

RadioLedger is distributed for self-hosting with Docker Compose.

### Is RadioLedger free?

The software is free and open-source (AGPLv3). You can self-host it at no
software license cost; you remain responsible for your own infrastructure and
any third-party service accounts you choose to connect.

### Can I import my existing logs?

Yes. RadioLedger imports ADIF files from any logger that can export ADIF. That includes WSJT-X, HRD Logbook, Log4OM, N1MM+, MacLoggerDX, DXKeeper, Logger32, and many others.

See [Import Your Existing Log](getting-started/import-existing-log.md).

### What callsigns can I use?

You can have multiple callsigns in RadioLedger — one per logbook, or by creating multiple logbooks. This supports:
- Home callsign + club callsign
- License upgrades (old and new callsign)
- POTA portable callsigns
- Multi-op contest callsigns

## Logging

### Are all times in UTC?

Yes. All timestamps are stored in UTC. The display can optionally show your local time, but the database always stores UTC. This follows the universal ham radio convention.

See [UTC Discipline](reference/glossary.md#u) in the glossary.

### Can I log QSOs with suffixes like /P or /MM?

Yes. Enter the callsign exactly as used (e.g., `W1AW/P`, `W1AW/MM`). RadioLedger handles DXCC entity resolution correctly for suffixes.

See [Callsign Parsing](reference/callsign-parsing.md).

### How does duplicate detection work?

By default, RadioLedger considers two QSOs duplicates if they have the same callsign, band, mode, and datetime within a 30-second window. This window handles slight timing differences between loggers.

You can configure the dedup window and strategy in logbook settings.

### Can I log for a club callsign?

Yes. Create a logbook with the club's callsign as the station callsign. Multiple operators can be configured to log to the same logbook. Each QSO records both the station callsign and the individual operator.

## Sync Services

### Does RadioLedger require the desktop client for LoTW?

The desktop client is required for desktop-local LoTW signing. It keeps tQSL
certificate material on the computer where you run the desktop client.

See [LoTW Setup](sync/lotw.md).

### Can I use RadioLedger without LoTW?

Yes. LoTW is optional. You can use RadioLedger with QRZ, eQSL, ClubLog, POTA, SOTA, or none of these services.

### How often does sync run?

Default intervals:
- LoTW: Continuous (via desktop client)
- QRZ: Every 5 minutes (outbound), hourly (inbound)
- eQSL: Every 15 minutes (outbound), 4x daily (inbound)
- ClubLog: Every 5 minutes (outbound)

Configurable in Settings.

## Desktop Client

### Do I need the desktop client?

No — unless you want:
- LoTW sync (requires desktop client for tQSL signing)
- Auto-logging from WSJT-X / JS8Call / N1MM+
- Rig control (frequency/mode auto-fill)

The web UI works without the desktop client.

### Can the desktop client run on a Raspberry Pi?

The desktop client has a GUI — it requires a graphical environment. For headless operation, see if your use case can be met by the API instead.

## Mobile App

### Does the mobile app work without internet?

Yes. The mobile app is offline-first. Log QSOs in the field without connectivity. Everything syncs when you reconnect.

See [Offline Logging](mobile/offline-logging.md).

### Is the mobile app available on iOS and Android?

Yes. Both iOS (16+) and Android (12+) are supported.

TODO: Add App Store / Google Play links when available.

## Self-Hosting

### What are the server requirements?

Minimum: 1 CPU core, 1 GB RAM, 5 GB disk, Docker + Docker Compose. A Raspberry Pi 4 works.

See [System Requirements](self-hosting/requirements.md).

### How do I back up my data?

Back up the PostgreSQL `pgdata` Docker volume and your `RADIOLEDGER_MASTER_KEY` separately. See [Backup and Restore](self-hosting/backup-restore.md).

### The database port isn't exposed — how do I connect to it?

Use `docker compose exec`:

```bash
docker compose exec db psql -U radioledger radioledger
```

### Can I use my own domain?

Yes. Set up a reverse proxy (Nginx, Caddy, or Traefik) and set `BASE_URL` to your domain. See [Reverse Proxy Setup](self-hosting/reverse-proxy.md).

## Contributing

### How do I contribute?

Read [AGENTS.md](../AGENTS.md) first (project rules), then see the [Contributing Guide](contributing/index.md).

### I found a security issue. Who do I tell?

Do not post vulnerability details in public issues. Use the repository's private security-advisory channel, or contact the maintainers through the project forge.

## Getting Help

- GitHub Issues: [github.com/FtlC-ian/radioledger/issues](https://github.com/FtlC-ian/radioledger/issues)
- For bugs: include your RadioLedger version, OS, and steps to reproduce
- For self-hosting issues: include `docker compose logs` output

## Related

- [Getting Started](getting-started/index.md)
- [Security Posture](security.md)
- [Glossary](reference/glossary.md)
- [Changelog](changelog/index.md)
