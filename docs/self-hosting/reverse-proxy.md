# Reverse Proxy Setup

> Configure Nginx, Caddy, or Traefik as a reverse proxy for HTTPS access.

A reverse proxy handles TLS termination so RadioLedger can be accessed over HTTPS with a real domain name.

## Caddy (Recommended)

Caddy handles TLS automatically via Let's Encrypt. No certificate management needed.

### Using the Included Caddy Compose Override

```bash
docker compose -f docker-compose.yml -f docker-compose.caddy.yml up -d
```

Set `BASE_URL=https://radio.yourdomain.com` in your `.env` file first.

### Manual Caddy Configuration

```
# Caddyfile
radio.yourdomain.com {
    reverse_proxy localhost:3000

    # API
    handle /api/* {
        reverse_proxy localhost:8080
    }
}
```

Caddy automatically obtains and renews Let's Encrypt certificates.

## Nginx

### Installation

```bash
apt install nginx certbot python3-certbot-nginx
```

### Nginx Configuration

```nginx
# /etc/nginx/sites-available/radioledger
server {
    listen 80;
    server_name radio.yourdomain.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name radio.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/radio.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/radio.yourdomain.com/privkey.pem;

    # Recommended SSL settings
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers on;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:...;

    # Web UI
    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # API
    location /api/ {
        proxy_pass http://localhost:8080/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket (for real-time updates)
    location /api/ws {
        proxy_pass http://localhost:8080/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }
}
```

```bash
# Get SSL certificate
certbot --nginx -d radio.yourdomain.com

# Enable site and reload
ln -s /etc/nginx/sites-available/radioledger /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx
```

## Traefik

TODO: Traefik configuration with Docker labels.

```yaml
# Add to your docker-compose.yml services
  traefik:
    image: traefik:v3
    command:
      - "--providers.docker=true"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.email=you@example.com"
      - "--certificatesresolvers.letsencrypt.acme.storage=/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.tlschallenge=true"
```

TODO: Complete Traefik example with labels.

## Security Headers

Add these security headers in your proxy configuration:

```nginx
add_header X-Frame-Options "SAMEORIGIN" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-XSS-Protection "1; mode=block" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
add_header Content-Security-Policy "default-src 'self'; ..." always;
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
```

TODO: Complete CSP header for RadioLedger's actual resources.

## Related

- [Docker Setup](docker-setup.md)
- [Security Hardening](security.md)
- [Configuration Reference](configuration.md)
