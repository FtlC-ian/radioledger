package adminaccess

import (
	"strings"
)

// IsAdminEmail reports whether the provided email is listed in the adminEmails string.
// adminEmails is a comma-separated list of email addresses (typically cfg.AdminEmails).
func IsAdminEmail(email string, adminEmails string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}

	raw := strings.TrimSpace(adminEmails)
	if raw == "" {
		return false
	}

	for _, part := range strings.Split(raw, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == email {
			return true
		}
	}
	return false
}
