package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
)

var (
	trustedProxyMu    sync.RWMutex
	trustedProxyCIDRs []netip.Prefix
)

// SetTrustedProxyCIDRs configures which reverse proxy CIDRs are allowed to set
// X-Forwarded-For / X-Real-IP. Empty means headers are not trusted.
func SetTrustedProxyCIDRs(cidrs []netip.Prefix) {
	trustedProxyMu.Lock()
	defer trustedProxyMu.Unlock()

	if len(cidrs) == 0 {
		trustedProxyCIDRs = nil
		return
	}

	trustedProxyCIDRs = make([]netip.Prefix, len(cidrs))
	copy(trustedProxyCIDRs, cidrs)
}

// ClientIP returns the client IP for a request.
//
// Safe default: if TRUSTED_PROXIES is empty or RemoteAddr is not in a trusted
// proxy CIDR, only RemoteAddr is used.
func ClientIP(r *http.Request) string {
	remoteIP := remoteIPFromAddr(r.RemoteAddr)
	if remoteIP == "" {
		return strings.TrimSpace(r.RemoteAddr)
	}

	if !isTrustedProxy(remoteIP) {
		return remoteIP
	}

	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		for _, part := range parts {
			candidate := strings.TrimSpace(part)
			if candidate == "" {
				continue
			}
			if parsed, err := netip.ParseAddr(candidate); err == nil {
				return parsed.String()
			}
		}
	}

	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		if parsed, err := netip.ParseAddr(xrip); err == nil {
			return parsed.String()
		}
	}

	return remoteIP
}

func remoteIPFromAddr(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
			return parsed.String()
		}
		return host
	}

	if parsed, parseErr := netip.ParseAddr(remoteAddr); parseErr == nil {
		return parsed.String()
	}
	return remoteAddr
}

func isTrustedProxy(ip string) bool {
	trustedProxyMu.RLock()
	prefixes := trustedProxyCIDRs
	trustedProxyMu.RUnlock()

	if len(prefixes) == 0 {
		return false
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
