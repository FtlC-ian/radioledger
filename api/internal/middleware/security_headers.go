// Package middleware — security response headers.
//
// Adds defense-in-depth headers to every HTTP response:
//   - Content-Security-Policy: restricts script/style/img sources
//   - Strict-Transport-Security: enforces HTTPS for 1 year
//   - X-Content-Type-Options: prevents MIME-type sniffing
//   - X-Frame-Options: prevents clickjacking
//   - Referrer-Policy: limits referrer leakage
//   - Permissions-Policy: disables unnecessary browser features
package middleware

import "net/http"

// SecurityHeaders adds standard security headers to every response.
// This middleware should be placed early in the stack (after RequestID, before handlers).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// CSP: API returns JSON, not HTML. A strict CSP prevents any injected
		// HTML from executing scripts if a response is somehow rendered in a browser.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

		// HSTS: enforce HTTPS for 1 year, include subdomains.
		// Safe to set even for HTTP-only dev environments (browsers ignore it for non-HTTPS).
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Prevent MIME-type sniffing (IE/Edge).
		h.Set("X-Content-Type-Options", "nosniff")

		// Prevent framing (clickjacking protection).
		h.Set("X-Frame-Options", "DENY")

		// Limit referrer information leakage.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Disable browser features we don't need.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		next.ServeHTTP(w, r)
	})
}
