package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter provides per-IP request rate limiting using token buckets.
type IPRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipEntry
	rps      rate.Limit
	burst    int
}

type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewIPRateLimiter(rps float64, burst int) *IPRateLimiter {
	rl := &IPRateLimiter{
		limiters: make(map[string]*ipEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go rl.pruneLoop()
	return rl
}

func (rl *IPRateLimiter) limiterForIP(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, ok := rl.limiters[ip]
	if !ok {
		entry = &ipEntry{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (rl *IPRateLimiter) pruneLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.prune(5 * time.Minute)
	}
}

func (rl *IPRateLimiter) prune(ttl time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	for ip, entry := range rl.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(rl.limiters, ip)
		}
	}
}

// RateLimitIP enforces per-IP rate limits except for probe/observability endpoints.
func RateLimitIP(rl *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/health", "/ready", "/metrics":
				next.ServeHTTP(w, r)
				return
			}

			ip := ClientIP(r)
			if ip == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !rl.limiterForIP(ip).Allow() {
				slog.InfoContext(r.Context(), "rate_limit: IP limit exceeded",
					slog.String("ip", ip),
					slog.String("request_id", RequestIDFromContext(r.Context())),
				)
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"success":false,"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
