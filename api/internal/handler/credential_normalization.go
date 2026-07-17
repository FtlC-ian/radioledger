package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	qrzsvc "github.com/FtlC-ian/radioledger/api/internal/services/qrz"
)

type qrzUsernamePasswordPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func normalizeCredentialValue(service, credentialType, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if service != "qrz" {
		return trimmed, nil
	}

	switch credentialType {
	case "api_key":
		return normalizeQRZAPIKeyValue(trimmed)
	case "username_password":
		return normalizeQRZUsernamePasswordValue(trimmed)
	default:
		return trimmed, nil
	}
}

func normalizeQRZAPIKeyValue(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("qrz api_key must not be empty")
	}

	if strings.HasPrefix(value, "{") {
		var payload struct {
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			return "", fmt.Errorf("qrz api_key must be raw text or JSON {\"api_key\":\"...\"}")
		}
		value = payload.APIKey
	}

	encoded, err := qrzsvc.EncodeLogbookCredentials(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeQRZUsernamePasswordValue(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("qrz username and password must not be empty")
	}

	if strings.HasPrefix(value, "{") {
		var payload qrzUsernamePasswordPayload
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			return "", fmt.Errorf("qrz username/password must be provided separately or as 'username:password'")
		}
		value = fmt.Sprintf("%s:%s", strings.TrimSpace(payload.Username), strings.TrimSpace(payload.Password))
	}

	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("qrz username/password must be provided separately or as 'username:password'")
	}

	return fmt.Sprintf("%s:%s", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])), nil
}
