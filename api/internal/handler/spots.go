package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

const defaultSpotNotificationCooldownMinutes = 30

type SpotsHandler struct {
	pool *pgxpool.Pool
}

func NewSpotsHandler(pool *pgxpool.Pool) *SpotsHandler {
	return &SpotsHandler{pool: pool}
}

type spotResponse struct {
	Source       string         `json:"source"`
	Callsign     string         `json:"callsign"`
	Reference    string         `json:"reference"`
	FrequencyKHz *string        `json:"frequency_khz,omitempty"`
	Band         *string        `json:"band,omitempty"`
	Mode         *string        `json:"mode,omitempty"`
	SpottedAt    string         `json:"spotted_at"`
	Payload      map[string]any `json:"payload,omitempty"`
}

type watchRuleRequest struct {
	Source    string  `json:"source"`
	Reference string  `json:"reference"`
	Mode      *string `json:"mode"`
	Band      *string `json:"band"`
	Enabled   *bool   `json:"enabled"`
}

type watchRuleResponse struct {
	UUID           string  `json:"uuid"`
	Source         string  `json:"source"`
	Reference      string  `json:"reference"`
	Mode           *string `json:"mode,omitempty"`
	Band           *string `json:"band,omitempty"`
	Enabled        bool    `json:"enabled"`
	LastNotifiedAt *string `json:"last_notified_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type spotPreferencesRequest struct {
	Enabled         bool `json:"enabled"`
	CooldownMinutes int  `json:"cooldown_minutes"`
}

type spotPreferencesResponse struct {
	Enabled         bool   `json:"enabled"`
	CooldownMinutes int32  `json:"cooldown_minutes"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

func (h *SpotsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limit := parseSpotsLimit(r.URL.Query().Get("limit"))
	source := normalizeSpotSource(r.URL.Query().Get("source"))
	band := normalizeOptionalText(r.URL.Query().Get("band"))
	mode := normalizeOptionalText(r.URL.Query().Get("mode"))

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rows, err := queries.ListActiveSpots(r.Context(), db.ListActiveSpotsParams{
		SourceFilter: source,
		BandFilter:   band,
		ModeFilter:   mode,
		LimitCount:   limit,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not list spots")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	items := make([]spotResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, spotFromRow(row))
	}

	writeSuccess(w, http.StatusOK, "spots listed", map[string]any{"items": items, "count": len(items)})
}

func (h *SpotsHandler) Needed(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limit := parseSpotsLimit(r.URL.Query().Get("limit"))
	source := normalizeSpotSource(r.URL.Query().Get("source"))
	band := normalizeOptionalText(r.URL.Query().Get("band"))
	mode := normalizeOptionalText(r.URL.Query().Get("mode"))

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rows, err := queries.ListNeededSpotsForUser(r.Context(), db.ListNeededSpotsForUserParams{
		UserID:       userID,
		SourceFilter: source,
		BandFilter:   band,
		ModeFilter:   mode,
		LimitCount:   limit,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not list needed spots")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	items := make([]spotResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, spotFromNeededRow(row))
	}

	writeSuccess(w, http.StatusOK, "needed spots listed", map[string]any{"items": items, "count": len(items)})
}

func (h *SpotsHandler) ListWatchRules(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rows, err := queries.ListSpotWatchRules(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not list watch rules")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	items := make([]watchRuleResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, watchRuleFromRow(row))
	}
	writeSuccess(w, http.StatusOK, "watch rules listed", map[string]any{"items": items, "count": len(items)})
}

func (h *SpotsHandler) CreateWatchRule(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req watchRuleRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	source := normalizeSpotSource(req.Source)
	if source == nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "source must be pota or sota")
		return
	}
	reference := strings.ToUpper(strings.TrimSpace(req.Reference))
	if reference == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "reference is required")
		return
	}
	mode := normalizeOptionalPtr(req.Mode)
	band := normalizeOptionalPtr(req.Band)
	enabled := req.Enabled

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	row, err := queries.CreateSpotWatchRule(r.Context(), db.CreateSpotWatchRuleParams{
		UserID:    userID,
		Source:    *source,
		Reference: reference,
		Mode:      mode,
		Band:      band,
		Enabled:   enabled,
	})
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "create failed", "could not create watch rule")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "create failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "watch rule created", watchRuleFromRow(row))
}

func (h *SpotsHandler) UpdateWatchRule(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	ruleUUID, err := uuid.Parse(chi.URLParam(r, "watchRuleUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid watch rule UUID")
		return
	}

	var req watchRuleRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rules, err := queries.ListSpotWatchRules(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "could not load watch rule")
		return
	}
	var existing *db.SpotWatchRule
	for i := range rules {
		if rules[i].Uuid == ruleUUID {
			existing = &rules[i]
			break
		}
	}
	if existing == nil {
		writeFailure(w, http.StatusOK, "watch rule not found", "watch rule not found")
		return
	}

	source := &existing.Source
	if strings.TrimSpace(req.Source) != "" {
		src := normalizeSpotSource(req.Source)
		if src == nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", "source must be pota or sota")
			return
		}
		source = src
	}

	reference := &existing.Reference
	if strings.TrimSpace(req.Reference) != "" {
		v := strings.ToUpper(strings.TrimSpace(req.Reference))
		reference = &v
	}

	mode := existing.Mode
	if req.Mode != nil {
		mode = normalizeOptionalPtr(req.Mode)
	}
	band := existing.Band
	if req.Band != nil {
		band = normalizeOptionalPtr(req.Band)
	}
	enabled := &existing.Enabled
	if req.Enabled != nil {
		enabled = req.Enabled
	}

	row, err := queries.UpdateSpotWatchRuleByUUID(r.Context(), db.UpdateSpotWatchRuleByUUIDParams{
		Source:    source,
		Reference: reference,
		Mode:      mode,
		Band:      band,
		Enabled:   enabled,
		RuleUuid:  ruleUUID,
		UserID:    userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "watch rule not found", "watch rule not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "update failed", "could not update watch rule")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "watch rule updated", watchRuleFromRow(row))
}

func (h *SpotsHandler) DeleteWatchRule(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	ruleUUID, err := uuid.Parse(chi.URLParam(r, "watchRuleUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid watch rule UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	affected, err := queries.DeleteSpotWatchRuleByUUID(r.Context(), db.DeleteSpotWatchRuleByUUIDParams{
		RuleUuid: ruleUUID,
		UserID:   userID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "delete failed", "could not delete watch rule")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusOK, "watch rule not found", "watch rule not found")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "delete failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "watch rule deleted", map[string]string{"uuid": ruleUUID.String()})
}

func (h *SpotsHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	pref, err := queries.GetSpotNotificationPreference(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(r.Context()); err != nil {
			writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
			return
		}
		writeSuccess(w, http.StatusOK, "spot notification preferences retrieved", spotPreferencesResponse{
			Enabled:         true,
			CooldownMinutes: defaultSpotNotificationCooldownMinutes,
		})
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not fetch preferences")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	resp := spotPreferencesResponse{
		Enabled:         pref.Enabled,
		CooldownMinutes: pref.CooldownMinutes,
	}
	if pref.UpdatedAt.Valid {
		resp.UpdatedAt = pref.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	writeSuccess(w, http.StatusOK, "spot notification preferences retrieved", resp)
}

func (h *SpotsHandler) PutPreferences(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req spotPreferencesRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if req.CooldownMinutes < 0 || req.CooldownMinutes > 1440 {
		writeFailure(w, http.StatusBadRequest, "invalid request", "cooldown_minutes must be between 0 and 1440")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	pref, err := queries.UpsertSpotNotificationPreference(r.Context(), db.UpsertSpotNotificationPreferenceParams{
		UserID:          userID,
		Enabled:         req.Enabled,
		CooldownMinutes: int32(req.CooldownMinutes),
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "could not save preferences")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "transaction failed")
		return
	}

	resp := spotPreferencesResponse{
		Enabled:         pref.Enabled,
		CooldownMinutes: pref.CooldownMinutes,
	}
	if pref.UpdatedAt.Valid {
		resp.UpdatedAt = pref.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	writeSuccess(w, http.StatusOK, "spot notification preferences updated", resp)
}

func parseSpotsLimit(raw string) int32 {
	if strings.TrimSpace(raw) == "" {
		return 100
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 100
	}
	if n > 500 {
		n = 500
	}
	return int32(n)
}

func normalizeSpotSource(raw string) *string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return nil
	}
	if s != "pota" && s != "sota" {
		return nil
	}
	return &s
}

func normalizeOptionalText(raw string) *string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}
	v = strings.ToUpper(v)
	return &v
}

func normalizeOptionalPtr(raw *string) *string {
	if raw == nil {
		return nil
	}
	v := strings.TrimSpace(*raw)
	if v == "" {
		return nil
	}
	v = strings.ToUpper(v)
	return &v
}

func spotFromRow(row db.Spot) spotResponse {
	return spotResponseFromValues(row.Source, row.Callsign, row.Reference, row.FrequencyKhz, row.Band, row.Mode, row.SpottedAt, row.RawPayload)
}

func spotFromNeededRow(row db.Spot) spotResponse {
	return spotResponseFromValues(row.Source, row.Callsign, row.Reference, row.FrequencyKhz, row.Band, row.Mode, row.SpottedAt, row.RawPayload)
}

func spotResponseFromValues(source, callsign, reference string, freq pgtype.Numeric, band, mode *string, spottedAt pgtype.Timestamptz, rawPayload []byte) spotResponse {
	resp := spotResponse{
		Source:    source,
		Callsign:  callsign,
		Reference: reference,
		SpottedAt: spottedAt.Time.UTC().Format(time.RFC3339),
		Band:      normalizeOptionalPtr(band),
		Mode:      normalizeOptionalPtr(mode),
	}
	if freq.Valid {
		if f, err := freq.Float64Value(); err == nil && f.Valid {
			resp.FrequencyKHz = ptrString(strconv.FormatFloat(f.Float64, 'f', 3, 64))
		}
	}
	if len(rawPayload) > 0 {
		payload := map[string]any{}
		if err := json.Unmarshal(rawPayload, &payload); err == nil {
			resp.Payload = payload
		}
	}
	return resp
}

func watchRuleFromRow(row db.SpotWatchRule) watchRuleResponse {
	resp := watchRuleResponse{
		UUID:      row.Uuid.String(),
		Source:    row.Source,
		Reference: row.Reference,
		Mode:      normalizeOptionalPtr(row.Mode),
		Band:      normalizeOptionalPtr(row.Band),
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if row.LastNotifiedAt.Valid {
		v := row.LastNotifiedAt.Time.UTC().Format(time.RFC3339)
		resp.LastNotifiedAt = &v
	}
	return resp
}

func ptrString(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
