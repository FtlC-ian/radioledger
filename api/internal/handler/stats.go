package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// StatsHandler handles aggregate stats endpoints.
type StatsHandler struct {
	pool *pgxpool.Pool
}

// NewStatsHandler creates a StatsHandler with dependencies.
func NewStatsHandler(pool *pgxpool.Pool) *StatsHandler {
	return &StatsHandler{pool: pool}
}

type topCountryResponse struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type statsResponse struct {
	TotalQSOs       int64                `json:"total_qsos"`
	UniqueCallsigns int64                `json:"unique_callsigns"`
	UniqueCountries int64                `json:"unique_countries"`
	UniqueGrids     int64                `json:"unique_grids"`
	Bands           map[string]int64     `json:"bands"`
	Modes           map[string]int64     `json:"modes"`
	TopCountries    []topCountryResponse `json:"top_countries"`
	QSOsByYear      map[string]int64     `json:"qsos_by_year"`
	FirstQSO        *string              `json:"first_qso,omitempty"`
	LastQSO         *string              `json:"last_qso,omitempty"`
}

// Get handles GET /v1/stats.
func (h *StatsHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	summary, err := queries.GetStatsSummary(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "summary query failed")
		return
	}

	bandRows, err := queries.GetStatsBands(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "band query failed")
		return
	}

	modeRows, err := queries.GetStatsModes(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "mode query failed")
		return
	}

	topCountryRows, err := queries.GetStatsTopCountries(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "country query failed")
		return
	}

	yearRows, err := queries.GetStatsByYear(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "year query failed")
		return
	}

	bands := make(map[string]int64, len(bandRows))
	for _, row := range bandRows {
		bands[row.Band] = row.Count
	}

	modes := make(map[string]int64, len(modeRows))
	for _, row := range modeRows {
		modes[row.Mode] = row.Count
	}

	topCountries := make([]topCountryResponse, 0, len(topCountryRows))
	for _, row := range topCountryRows {
		topCountries = append(topCountries, topCountryResponse{
			Name:  row.Name,
			Count: row.Count,
		})
	}

	qsosByYear := make(map[string]int64, len(yearRows))
	for _, row := range yearRows {
		qsosByYear[formatInt32(row.Year)] = row.Count
	}

	resp := statsResponse{
		TotalQSOs:       summary.TotalQsos,
		UniqueCallsigns: summary.UniqueCallsigns,
		UniqueCountries: summary.UniqueCountries,
		UniqueGrids:     summary.UniqueGrids,
		Bands:           bands,
		Modes:           modes,
		TopCountries:    topCountries,
		QSOsByYear:      qsosByYear,
	}

	if summary.FirstQso.Valid {
		first := summary.FirstQso.Time.UTC().Format(time.RFC3339)
		resp.FirstQSO = &first
	}
	if summary.LastQso.Valid {
		last := summary.LastQso.Time.UTC().Format(time.RFC3339)
		resp.LastQSO = &last
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "stats retrieved", resp)
}

// overviewResponse is the response for GET /v1/stats/overview.
type overviewResponse struct {
	TotalQSOs       int64   `json:"total_qsos"`
	UniqueCallsigns int64   `json:"unique_callsigns"`
	UniqueCountries int64   `json:"unique_countries"`
	UniqueStates    int64   `json:"unique_states"`
	UniqueGrids     int64   `json:"unique_grids"`
	BandsUsed       int64   `json:"bands_used"`
	ModesUsed       int64   `json:"modes_used"`
	FirstQSO        *string `json:"first_qso,omitempty"`
	LastQSO         *string `json:"last_qso,omitempty"`
}

// Overview handles GET /v1/stats/overview.
func (h *StatsHandler) Overview(w http.ResponseWriter, r *http.Request) {
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

	row, err := queries.GetStatsOverview(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "overview query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	resp := overviewResponse{
		TotalQSOs:       row.TotalQsos,
		UniqueCallsigns: row.UniqueCallsigns,
		UniqueCountries: row.UniqueCountries,
		UniqueStates:    row.UniqueStates,
		UniqueGrids:     row.UniqueGrids,
		BandsUsed:       row.BandsUsed,
		ModesUsed:       row.ModesUsed,
	}
	if row.FirstQso.Valid {
		s := row.FirstQso.Time.UTC().Format(time.RFC3339)
		resp.FirstQSO = &s
	}
	if row.LastQso.Valid {
		s := row.LastQso.Time.UTC().Format(time.RFC3339)
		resp.LastQSO = &s
	}

	writeSuccess(w, http.StatusOK, "overview retrieved", resp)
}

// bandEntry is a single band count for the by-band response.
type bandEntry struct {
	Band  string `json:"band"`
	Count int64  `json:"count"`
}

// ByBand handles GET /v1/stats/by-band.
func (h *StatsHandler) ByBand(w http.ResponseWriter, r *http.Request) {
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

	rows, err := queries.GetStatsBands(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "band query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	entries := make([]bandEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, bandEntry{Band: row.Band, Count: row.Count})
	}
	writeSuccess(w, http.StatusOK, "by-band stats retrieved", entries)
}

// modeEntry is a single mode count for the by-mode response.
type modeEntry struct {
	Mode  string `json:"mode"`
	Count int64  `json:"count"`
}

// ByMode handles GET /v1/stats/by-mode.
func (h *StatsHandler) ByMode(w http.ResponseWriter, r *http.Request) {
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

	rows, err := queries.GetStatsModes(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "mode query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	entries := make([]modeEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, modeEntry{Mode: row.Mode, Count: row.Count})
	}
	writeSuccess(w, http.StatusOK, "by-mode stats retrieved", entries)
}

// periodEntry is a single period count for by-period responses.
type periodEntry struct {
	Period string `json:"period"`
	Count  int64  `json:"count"`
}

// ByPeriod handles GET /v1/stats/by-period?period=month|year.
// For backwards compatibility it also accepts ?group=month|year.
func (h *StatsHandler) ByPeriod(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	group := r.URL.Query().Get("period")
	if group == "" {
		group = r.URL.Query().Get("group")
	}
	if group == "" {
		group = "month"
	}
	if group != "month" && group != "year" {
		writeFailure(w, http.StatusBadRequest, "invalid period parameter", `period must be "month" or "year"`)
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	var entries []periodEntry

	if group == "year" {
		rows, err := queries.GetStatsByYear(r.Context())
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "year query failed")
			return
		}
		entries = make([]periodEntry, 0, len(rows))
		for _, row := range rows {
			entries = append(entries, periodEntry{Period: formatInt32(row.Year), Count: row.Count})
		}
	} else {
		rows, err := queries.GetStatsByMonth(r.Context())
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "month query failed")
			return
		}
		entries = make([]periodEntry, 0, len(rows))
		for _, row := range rows {
			entries = append(entries, periodEntry{Period: row.Period, Count: row.Count})
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "by-period stats retrieved", entries)
}

// countriesOverTimeEntry is a single month entry for countries-over-time.
type countriesOverTimeEntry struct {
	Period          string `json:"period"`
	UniqueCountries int64  `json:"unique_countries"`
}

// CountriesOverTime handles GET /v1/stats/countries-over-time.
func (h *StatsHandler) CountriesOverTime(w http.ResponseWriter, r *http.Request) {
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

	rows, err := queries.GetStatsCountriesOverTime(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "countries-over-time query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	entries := make([]countriesOverTimeEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, countriesOverTimeEntry{
			Period:          row.Period,
			UniqueCountries: row.UniqueCountries,
		})
	}
	writeSuccess(w, http.StatusOK, "countries-over-time retrieved", entries)
}

// callsignEntry is a single callsign count for top-callsigns.
type callsignEntry struct {
	Callsign string `json:"callsign"`
	Count    int64  `json:"count"`
}

// TopCallsigns handles GET /v1/stats/top-callsigns?limit=20.
func (h *StatsHandler) TopCallsigns(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limit := int32(20)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 1 || n > 100 {
			writeFailure(w, http.StatusBadRequest, "invalid limit parameter", "limit must be an integer between 1 and 100")
			return
		}
		limit = int32(n)
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	rows, err := queries.GetStatsTopCallsigns(r.Context(), limit)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "top-callsigns query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	entries := make([]callsignEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, callsignEntry{Callsign: row.Callsign, Count: row.Count})
	}
	writeSuccess(w, http.StatusOK, "top-callsigns retrieved", entries)
}

// TopCountries handles GET /v1/stats/top-countries?limit=20.
func (h *StatsHandler) TopCountries(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limit := int32(20)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 1 || n > 100 {
			writeFailure(w, http.StatusBadRequest, "invalid limit parameter", "limit must be an integer between 1 and 100")
			return
		}
		limit = int32(n)
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	rows, err := queries.GetStatsTopCountriesLimited(r.Context(), limit)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "top-countries query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	entries := make([]topCountryResponse, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, topCountryResponse{Name: row.Name, Count: row.Count})
	}
	writeSuccess(w, http.StatusOK, "top-countries retrieved", entries)
}

// operatingPatternEntry is a single cell in the 24h x 7d heatmap.
type operatingPatternEntry struct {
	DayOfWeek int32 `json:"day_of_week"`
	HourOfDay int32 `json:"hour_of_day"`
	Count     int64 `json:"count"`
}

// OperatingPatterns handles GET /v1/stats/operating-patterns.
// Returns 24h x 7d heatmap data: day_of_week (0=Sun..6=Sat) x hour_of_day (0-23).
func (h *StatsHandler) OperatingPatterns(w http.ResponseWriter, r *http.Request) {
	h.writeOperatingPatterns(w, r, "operating-patterns")
}

// ActivityHeatmap handles GET /v1/stats/activity-heatmap.
// This is an alias for operating patterns for dashboard compatibility.
func (h *StatsHandler) ActivityHeatmap(w http.ResponseWriter, r *http.Request) {
	h.writeOperatingPatterns(w, r, "activity-heatmap")
}

func (h *StatsHandler) writeOperatingPatterns(w http.ResponseWriter, r *http.Request, endpointName string) {
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

	rows, err := queries.GetStatsOperatingPatterns(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", endpointName+" query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute stats", "transaction failed")
		return
	}

	entries := make([]operatingPatternEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, operatingPatternEntry{
			DayOfWeek: row.DayOfWeek,
			HourOfDay: row.HourOfDay,
			Count:     row.Count,
		})
	}
	writeSuccess(w, http.StatusOK, endpointName+" retrieved", entries)
}

func formatInt32(v int32) string {
	return strconv.FormatInt(int64(v), 10)
}
