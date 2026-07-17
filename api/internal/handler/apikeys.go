// Package handler — API key management endpoints.
package handler

import (
	"crypto/rand"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	"github.com/FtlC-ian/radioledger/api/internal/securitylog"
)

const (
	apiKeyPrefix        = "rl_"
	keyBodyLen          = 32
	keyPrefixDisplayLen = 12
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var validScopes = map[string]bool{
	"read": true, "write": true, "import": true, "export": true,
}

// scopeExpansion maps user-facing short scope names to the detailed scope names
// that the RequireAPIKeyScope middleware enforces. Short names provide a simpler
// API surface; the middleware operates on the canonical colon-separated names.
var scopeExpansion = map[string][]string{
	"read":   {middleware.ScopeLogbooksRead, middleware.ScopeQSOsRead},
	"write":  {middleware.ScopeLogbooksWrite, middleware.ScopeQSOsWrite},
	"import": {middleware.ScopeADIFImport},
	"export": {middleware.ScopeADIFExport},
}

// allDetailedScopes is the full scope set granted when no specific scopes are
// requested. Creating a key without scopes yields an unrestricted key.
var allDetailedScopes = []string{
	middleware.ScopeLogbooksRead, middleware.ScopeLogbooksWrite,
	middleware.ScopeQSOsRead, middleware.ScopeQSOsWrite, middleware.ScopeQSOsDelete,
	middleware.ScopeADIFImport, middleware.ScopeADIFExport,
	middleware.ScopeSyncStatus, middleware.ScopeSyncTrigger,
}

// APIKeyHandler handles API key management endpoints.
type APIKeyHandler struct {
	pool *pgxpool.Pool
}

// NewAPIKeyHandler creates an APIKeyHandler.
func NewAPIKeyHandler(pool *pgxpool.Pool) *APIKeyHandler {
	return &APIKeyHandler{pool: pool}
}

type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type createAPIKeyResponse struct {
	Key       string     `json:"key"`
	UUID      string     `json:"uuid"`
	KeyPrefix string     `json:"key_prefix"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type apiKeyListItem struct {
	UUID       string     `json:"uuid"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Generate handles POST /v1/api-keys.
func (h *APIKeyHandler) Generate(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createAPIKeyRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeFailure(w, http.StatusBadRequest, "validation error", "name is required")
		return
	}
	if len(req.Name) > 255 {
		writeFailure(w, http.StatusBadRequest, "validation error", "name must be 255 characters or fewer")
		return
	}
	// Validate and deduplicate the user-provided short scope names.
	seenShort := make(map[string]bool)
	var shortScopes []string
	for _, s := range req.Scopes {
		s = strings.ToLower(strings.TrimSpace(s))
		if !validScopes[s] {
			writeFailure(w, http.StatusBadRequest, "invalid scope",
				"valid scopes are: read, write, import, export")
			return
		}
		if !seenShort[s] {
			seenShort[s] = true
			shortScopes = append(shortScopes, s)
		}
	}
	// Expand short scope names to the detailed middleware scope names.
	// A key created with no scopes is granted unrestricted access (all scopes).
	var cleanScopes []string
	if len(shortScopes) == 0 {
		cleanScopes = allDetailedScopes
	} else {
		seenDetail := make(map[string]bool)
		for _, s := range shortScopes {
			for _, ds := range scopeExpansion[s] {
				if !seenDetail[ds] {
					seenDetail[ds] = true
					cleanScopes = append(cleanScopes, ds)
				}
			}
		}
	}
	plaintextKey, err := generateAPIKey()
	if err != nil {
		slog.ErrorContext(r.Context(), "api-keys: key generation failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "key generation failed", "internal error")
		return
	}
	keyHash := crypto.HashAPIKey(plaintextKey)
	keyPrefix := plaintextKey[:keyPrefixDisplayLen]
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := sqlc.New(tx)
	key, err := q.CreateAPIKey(r.Context(), sqlc.CreateAPIKeyParams{
		UserID:    userID,
		Name:      strings.TrimSpace(req.Name),
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		Scopes:    cleanScopes,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "api-keys: create failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	resp := createAPIKeyResponse{
		Key:       plaintextKey,
		UUID:      key.Uuid.String(),
		KeyPrefix: key.KeyPrefix,
		Name:      key.Name,
		Scopes:    key.Scopes,
		CreatedAt: key.CreatedAt.Time,
	}
	if key.ExpiresAt.Valid {
		t := key.ExpiresAt.Time
		resp.ExpiresAt = &t
	}

	securitylog.Event(r.Context(), "api_key_create",
		slog.Int64("user_id", userID),
		slog.String("key_uuid", key.Uuid.String()),
		slog.String("remote_ip", middleware.ClientIP(r)),
	)

	writeSuccess(w, http.StatusCreated, "API key created — save the key now, it will not be shown again", resp)
}

// List handles GET /v1/api-keys.
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := sqlc.New(tx)
	rows, err := q.ListAPIKeys(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "api-keys: list failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	items := make([]apiKeyListItem, 0, len(rows))
	for _, row := range rows {
		item := apiKeyListItem{
			UUID:      row.Uuid.String(),
			Name:      row.Name,
			KeyPrefix: row.KeyPrefix,
			Scopes:    row.Scopes,
			CreatedAt: row.CreatedAt.Time,
		}
		if row.ExpiresAt.Valid {
			t := row.ExpiresAt.Time
			item.ExpiresAt = &t
		}
		if row.LastUsedAt.Valid {
			t := row.LastUsedAt.Time
			item.LastUsedAt = &t
		}
		items = append(items, item)
	}
	writeSuccess(w, http.StatusOK, "API keys retrieved", map[string]any{
		"items": items,
		"count": len(items),
	})
}

// Revoke handles DELETE /v1/api-keys/{uuid}.
func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	keyUUIDStr := chi.URLParam(r, "uuid")
	keyUUID, err := uuid.Parse(keyUUIDStr)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid uuid", "uuid must be a valid UUID")
		return
	}
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := sqlc.New(tx)
	affected, err := q.RevokeAPIKey(r.Context(), sqlc.RevokeAPIKeyParams{
		Uuid:   keyUUID,
		UserID: userID,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "api-keys: revoke failed",
			slog.String("key_uuid", keyUUIDStr), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusNotFound, "not found", "API key not found or already revoked")
		return
	}

	securitylog.Event(r.Context(), "api_key_revoke",
		slog.Int64("user_id", userID),
		slog.String("key_uuid", keyUUID.String()),
		slog.String("remote_ip", middleware.ClientIP(r)),
	)

	writeSuccess(w, http.StatusOK, "API key revoked", nil)
}

// generateAPIKey generates a cryptographically random API key.
// Format: "rl_" + base62(32 random bytes) ~= 46 chars total.
func generateAPIKey() (string, error) {
	rawBytes := make([]byte, keyBodyLen)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", err
	}
	return apiKeyPrefix + encodeBase62(rawBytes), nil
}

// encodeBase62 encodes a byte slice to base62.
func encodeBase62(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(62)
	zero := big.NewInt(0)
	mod := new(big.Int)
	var result []byte
	for n.Cmp(zero) > 0 {
		n.DivMod(n, base, mod)
		result = append(result, base62Alphabet[mod.Int64()])
	}
	for len(result) < 43 {
		result = append(result, base62Alphabet[0])
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

// IsAPIKeyToken returns true if the token looks like a RadioLedger API key.
func IsAPIKeyToken(token string) bool {
	return strings.HasPrefix(token, apiKeyPrefix)
}
