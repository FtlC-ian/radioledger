# System Requirements

> Minimum and recommended hardware/software for self-hosting RadioLedger.

## Hardware Requirements

### Minimum

| Resource | Minimum | Notes |
|---------|---------|-------|
| CPU | 1 core | Any modern x86-64 or ARM64 |
| RAM | 1 GB | 512 MB for PostgreSQL + 256 MB API + 256 MB Zitadel |
| Disk | 5 GB | Database + uploads; grows with log size |
| Network | Any | Internet access for external sync services |

A Raspberry Pi 4 (4 GB RAM) runs RadioLedger comfortably.

### Recommended

| Resource | Recommended | Notes |
|---------|-------------|-------|
| CPU | 2+ cores | Better for concurrent users and large imports |
| RAM | 2–4 GB | More headroom for large logs and multiple users |
| Disk | 20+ GB | If you have many ADIF imports or plan long-term use |
| Network | Broadband | For reliable LoTW/QRZ sync |

### For Large Installations (club station, many users)

| Resource | Recommended |
|---------|-------------|
| CPU | 4+ cores |
| RAM | 8 GB+ |
| Disk | SSD for PostgreSQL data volume |

TODO: Add performance benchmarks for typical log sizes (10k, 100k, 500k QSOs).

## Software Requirements

| Requirement | Version |
|-------------|---------|
| Docker Engine | 24.0+ |
| Docker Compose | v2.20+ (the `docker compose` command, not `docker-compose`) |
| Operating System | Linux (recommended), macOS, or Windows with WSL2 |

**Docker Desktop** for macOS and Windows includes both Docker Engine and Compose.

## Operating System Notes

### Linux (Recommended)

Any modern distribution works:
- Ubuntu 22.04 / 24.04
- Debian 12
- Fedora 39+
- Rocky Linux 9

### macOS

macOS with Docker Desktop works but is primarily for development. For production self-hosting, use a Linux machine or VM.

### Windows

Use Docker Desktop with WSL2 backend. Native Windows Docker is not tested.

### Raspberry Pi

ARM64 is supported. Use a Pi 4 or Pi 5 with 4 GB+ RAM. The container images are multi-arch.

```bash
# Verify your Pi is running 64-bit OS
uname -m  # should print aarch64
```

## Domain and TLS (Optional but Recommended)

For external access:
- A domain name pointing to your server
- HTTPS via reverse proxy (Caddy, Nginx, or Traefik — see [Reverse Proxy Setup](reverse-proxy.md))

Without HTTPS, the web UI and API work fine on a local network.

## Firewall Ports

Ports to expose (if using a firewall):

| Port | Protocol | Purpose | Expose? |
|------|----------|---------|---------|
| 80 | TCP | HTTP (redirect to HTTPS) | Yes (with reverse proxy) |
| 443 | TCP | HTTPS | Yes (with reverse proxy) |
| 3000 | TCP | Web UI (direct, no TLS) | Local only |
| 8080 | TCP | API (direct, no TLS) | Local only |
| 5432 | TCP | PostgreSQL | **Never** |

The PostgreSQL port should **never** be exposed. It's inside the Docker network only.

## Related

- [Docker Setup](docker-setup.md)
- [Reverse Proxy Setup](reverse-proxy.md)
- [Security Hardening](security.md)
