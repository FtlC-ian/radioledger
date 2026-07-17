package middleware

import (
	"net/http"

	"github.com/FtlC-ian/radioledger/api/internal/adminaccess"
	"github.com/FtlC-ian/radioledger/api/internal/auth"
)

// RequireAdmin returns a middleware that enforces server-level admin access
// based on the authenticated user's email. adminEmails is the comma-separated
// list from cfg.AdminEmails.
func RequireAdmin(adminEmails string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := auth.UserInfoFromContext(r.Context())
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized","error":"missing authenticated user"}`))
				return
			}

			if !adminaccess.IsAdminEmail(info.Email, adminEmails) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"message":"forbidden","error":"admin access required"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
