package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// PreferencesHandler manages user preference payloads.
type PreferencesHandler struct {
	pool *pgxpool.Pool
}

func NewPreferencesHandler(pool *pgxpool.Pool) *PreferencesHandler {
	return &PreferencesHandler{pool: pool}
}

type preferencesPayload struct {
	DisplayName    *string   `json:"display_name,omitempty"`
	Timezone       *string   `json:"timezone,omitempty"`
	DefaultGrid    *string   `json:"default_grid,omitempty"`
	DefaultBand    *string   `json:"default_band,omitempty"`
	DefaultMode    *string   `json:"default_mode,omitempty"`
	DefaultPower   *float64  `json:"default_power,omitempty"`
	UITheme        *string   `json:"ui_theme,omitempty"`
	DedupWindow    *int      `json:"dedup_window,omitempty"`
	SyncEnabled    *bool     `json:"sync_enabled,omitempty"`
	DesktopUDPPort *int      `json:"desktop_udp_port,omitempty"`
	DesktopRigPort *int      `json:"desktop_rig_port,omitempty"`
	ITURegion      *int      `json:"itu_region,omitempty"`
	VisibleBands   *[]string `json:"visible_bands,omitempty"`
	VisibleModes   *[]string `json:"visible_modes,omitempty"`
}

func (h *PreferencesHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	row, err := q.GetUserPreferences(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusNotFound, "not found", "user not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "preferences: get failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	writeSuccess(w, http.StatusOK, "preferences retrieved", buildPreferencesResponse(row))
}

func (h *PreferencesHandler) Put(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var payload preferencesPayload
	if err := decodeJSONBody(r, &payload); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if err := validatePreferencesPayload(payload); err != nil {
		writeFailure(w, http.StatusBadRequest, "validation error", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := sqlc.New(tx)
	current, err := q.GetUserPreferences(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusNotFound, "not found", "user not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "preferences: pre-read failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	prefsMap := decodePreferencesMap(current.Preferences)
	mergePayloadIntoMap(prefsMap, payload)
	prefsJSON, err := json.Marshal(prefsMap)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "encoding error", "failed to encode preferences")
		return
	}

	var numeric pgtype.Numeric
	if payload.DefaultPower != nil {
		if err := numeric.Scan(fmt.Sprintf("%f", *payload.DefaultPower)); err != nil {
			writeFailure(w, http.StatusBadRequest, "validation error", "default_power must be numeric")
			return
		}
	}

	updated, err := q.UpdateUserPreferences(r.Context(), sqlc.UpdateUserPreferencesParams{
		ID:                userID,
		DisplayName:       payload.DisplayName,
		Timezone:          payload.Timezone,
		GridSquare:        payload.DefaultGrid,
		DefaultPowerWatts: numeric,
		Preferences:       prefsJSON,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "preferences: update failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	resultRow := sqlc.GetUserPreferencesRow(updated)

	writeSuccess(w, http.StatusOK, "preferences updated", buildPreferencesResponse(resultRow))
}

func validatePreferencesPayload(payload preferencesPayload) error {
	if payload.Timezone != nil {
		if strings.TrimSpace(*payload.Timezone) == "" {
			return errors.New("timezone must not be empty")
		}
		if len(*payload.Timezone) > 128 {
			return errors.New("timezone is too long")
		}
	}

	if payload.DefaultBand != nil && len(strings.TrimSpace(*payload.DefaultBand)) > 24 {
		return errors.New("default_band is too long")
	}
	if payload.DefaultMode != nil && len(strings.TrimSpace(*payload.DefaultMode)) > 24 {
		return errors.New("default_mode is too long")
	}

	if payload.UITheme != nil {
		theme := strings.ToLower(strings.TrimSpace(*payload.UITheme))
		if theme != "dark" && theme != "light" && theme != "system" {
			return errors.New("ui_theme must be one of: dark, light, system")
		}
	}

	if payload.DedupWindow != nil {
		if *payload.DedupWindow < 5 || *payload.DedupWindow > 3600 {
			return errors.New("dedup_window must be between 5 and 3600 seconds")
		}
	}

	if payload.DefaultPower != nil {
		if math.IsNaN(*payload.DefaultPower) || math.IsInf(*payload.DefaultPower, 0) {
			return errors.New("default_power must be a finite number")
		}
		if *payload.DefaultPower < 0 {
			return errors.New("default_power must be non-negative")
		}
	}

	for _, port := range []*int{payload.DesktopUDPPort, payload.DesktopRigPort} {
		if port == nil {
			continue
		}
		if *port < 1 || *port > 65535 {
			return errors.New("desktop ports must be between 1 and 65535")
		}
	}

	if payload.ITURegion != nil && (*payload.ITURegion < 1 || *payload.ITURegion > 3) {
		return errors.New("itu_region must be 1, 2, or 3")
	}
	if payload.VisibleBands != nil {
		if len(*payload.VisibleBands) == 0 {
			return errors.New("visible_bands must include at least one band")
		}
		for _, band := range *payload.VisibleBands {
			if len(strings.TrimSpace(band)) == 0 || len(strings.TrimSpace(band)) > 24 {
				return errors.New("visible_bands contains an invalid value")
			}
		}
	}
	if payload.VisibleModes != nil {
		if len(*payload.VisibleModes) == 0 {
			return errors.New("visible_modes must include at least one mode")
		}
		for _, mode := range *payload.VisibleModes {
			if len(strings.TrimSpace(mode)) == 0 || len(strings.TrimSpace(mode)) > 24 {
				return errors.New("visible_modes contains an invalid value")
			}
		}
	}

	return nil
}

func decodePreferencesMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func mergePayloadIntoMap(dest map[string]any, payload preferencesPayload) {
	if payload.DefaultBand != nil {
		dest["default_band"] = strings.ToUpper(strings.TrimSpace(*payload.DefaultBand))
	}
	if payload.DefaultMode != nil {
		dest["default_mode"] = strings.ToUpper(strings.TrimSpace(*payload.DefaultMode))
	}
	if payload.DefaultPower != nil {
		dest["default_power"] = *payload.DefaultPower
	}
	if payload.UITheme != nil {
		dest["ui_theme"] = strings.ToLower(strings.TrimSpace(*payload.UITheme))
	}
	if payload.DedupWindow != nil {
		dest["dedup_window"] = *payload.DedupWindow
	}
	if payload.SyncEnabled != nil {
		dest["sync_enabled"] = *payload.SyncEnabled
	}
	if payload.DesktopUDPPort != nil {
		dest["desktop_udp_port"] = *payload.DesktopUDPPort
	}
	if payload.DesktopRigPort != nil {
		dest["desktop_rig_port"] = *payload.DesktopRigPort
	}
	if payload.ITURegion != nil {
		dest["itu_region"] = *payload.ITURegion
	}
	if payload.VisibleBands != nil {
		dest["visible_bands"] = normalizeStringSlice(*payload.VisibleBands, false)
	}
	if payload.VisibleModes != nil {
		dest["visible_modes"] = normalizeStringSlice(*payload.VisibleModes, true)
	}
}

func buildPreferencesResponse(row sqlc.GetUserPreferencesRow) map[string]any {
	prefs := decodePreferencesMap(row.Preferences)

	response := map[string]any{
		"timezone":         row.Timezone,
		"display_name":     row.DisplayName,
		"default_grid":     row.GridSquare,
		"default_band":     stringValue(prefs, "default_band"),
		"default_mode":     stringValue(prefs, "default_mode"),
		"ui_theme":         stringValueWithDefault(prefs, "ui_theme", "dark"),
		"dedup_window":     intValueWithDefault(prefs, "dedup_window", 300),
		"sync_enabled":     boolValueWithDefault(prefs, "sync_enabled", false),
		"desktop_udp_port": intValueWithDefault(prefs, "desktop_udp_port", 2237),
		"desktop_rig_port": intValueWithDefault(prefs, "desktop_rig_port", 4532),
		"visible_bands":    stringSliceValueWithDefault(prefs, "visible_bands"),
		"visible_modes":    stringSliceValueWithDefault(prefs, "visible_modes"),
	}
	if region, ok := intValue(prefs, "itu_region"); ok && region >= 1 && region <= 3 {
		response["itu_region"] = region
	}

	if power := numericToFloat(row.DefaultPowerWatts); power != nil {
		response["default_power"] = *power
	} else if v, ok := floatValue(prefs, "default_power"); ok {
		response["default_power"] = v
	}

	return response
}

func numericToFloat(value pgtype.Numeric) *float64 {
	fv, err := value.Float64Value()
	if err != nil || !fv.Valid {
		return nil
	}
	out := fv.Float64
	return &out
}

func stringValue(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func stringValueWithDefault(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func intValueWithDefault(m map[string]any, key string, fallback int) int {
	if v, ok := intValue(m, key); ok {
		return v
	}
	return fallback
}

func intValue(m map[string]any, key string) (int, bool) {
	if v, ok := m[key]; ok {
		switch typed := v.(type) {
		case float64:
			return int(typed), true
		case int:
			return typed, true
		}
	}
	return 0, false
}

func boolValueWithDefault(m map[string]any, key string, fallback bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return fallback
}

func floatValue(m map[string]any, key string) (float64, bool) {
	if v, ok := m[key].(float64); ok {
		return v, true
	}
	return 0, false
}

func stringSliceValueWithDefault(m map[string]any, key string) []string {
	values := normalizeStringSlice(stringSlicePreferenceValue(m, key), false)
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func stringSlicePreferenceValue(m map[string]any, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}

	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		values = append(values, text)
	}
	return values
}

func normalizeStringSlice(values []string, uppercase bool) []string {
	if len(values) == 0 {
		return []string{}
	}

	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if uppercase {
			trimmed = strings.ToUpper(trimmed)
		}
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}
