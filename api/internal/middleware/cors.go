package middleware

import (
	"net/http"
	"strings"
)

// CORS returns an HTTP middleware that enforces strict Cross-Origin Resource Sharing policy.
// Only origins in the provided allowedOrigins list are permitted; wildcard ("*") is never used.
//
// Per the security architecture (ARCHITECTURE.md § "Security Architecture Summary"),
// CORS must use an explicit origin allowlist. Wildcard CORS would defeat cookie-based
// auth protections.
//
// allowedOrigins is a slice of exact origin strings, e.g.:
//
//	[]string{"https://radioledger.app", "https://staging.radioledger.app"}
//
// An empty allowedOrigins list blocks all cross-origin requests (same-origin only).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	// Build a lookup set for O(1) origin checks.
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" {
				if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers",
						"Content-Type, Authorization, X-Request-ID")
					w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
					w.Header().Set("Vary", "Origin")
				}
				// Origins NOT in the allowlist receive no CORS headers.
				// Browsers will block the request on the client side.
			}

			// Handle preflight requests.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
