package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/jobs"
	awardssvc "github.com/FtlC-ian/radioledger/api/internal/services/awards"
)

// AwardsHandler handles award progress endpoints (DXCC, WAS, grids).
type AwardsHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
}

// NewAwardsHandler creates an AwardsHandler with pool only (no job enqueuing).
func NewAwardsHandler(pool *pgxpool.Pool) *AwardsHandler {
	return &AwardsHandler{pool: pool}
}

// NewAwardsHandlerWithSync creates an AwardsHandler with River client so that
// POST /v1/awards/refresh immediately enqueues a background recalculation job.
func NewAwardsHandlerWithSync(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *AwardsHandler {
	return &AwardsHandler{pool: pool, riverClient: riverClient}
}

type dxccBandSummary struct {
	Worked    int64 `json:"worked"`
	Confirmed int64 `json:"confirmed"`
}

type dxccQSOInfo struct {
	UUID       string `json:"uuid"`
	Callsign   string `json:"callsign"`
	Band       string `json:"band"`
	Mode       string `json:"mode"`
	DatetimeOn string `json:"datetime_on"`
}

type dxccEntityProgress struct {
	EntityID  int32         `json:"entity_id"`
	Name      string        `json:"name"`
	Prefix    string        `json:"prefix"`
	Continent string        `json:"continent"`
	Worked    bool          `json:"worked"`
	Confirmed bool          `json:"confirmed"`
	Bands     []string      `json:"bands"`
	FirstQSO  *string       `json:"first_qso,omitempty"`
	QSOCount  int64         `json:"qso_count"`
	QSOs      []dxccQSOInfo `json:"qsos,omitempty"`
}

type dxccResponse struct {
	TotalEntities int64                      `json:"total_entities"`
	Worked        int64                      `json:"worked"`
	Confirmed     int64                      `json:"confirmed"`
	Needed        int64                      `json:"needed"`
	ByBand        map[string]dxccBandSummary `json:"by_band"`
	Entities      []dxccEntityProgress       `json:"entities"`
}

// DXCC handles GET /v1/awards/dxcc.
func (h *AwardsHandler) DXCC(w http.ResponseWriter, r *http.Request) {
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

	total, err := queries.GetDXCCTotalEntities(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute dxcc progress", "total entity query failed")
		return
	}

	summary, err := queries.GetDXCCSummary(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute dxcc progress", "summary query failed")
		return
	}

	byBandRows, err := queries.ListDXCCByBand(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute dxcc progress", "band summary query failed")
		return
	}

	entityRows, err := queries.ListDXCCEntitiesProgress(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute dxcc progress", "entity progress query failed")
		return
	}

	qsoRows, err := queries.ListDXCCEntityQSODetails(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute dxcc progress", "entity qso detail query failed")
		return
	}

	entityQSOs := make(map[int32][]dxccQSOInfo)
	for _, row := range qsoRows {
		entityQSOs[row.EntityID] = append(entityQSOs[row.EntityID], dxccQSOInfo{
			UUID:       row.Uuid.String(),
			Callsign:   row.Callsign,
			Band:       row.Band,
			Mode:       row.Mode,
			DatetimeOn: row.DatetimeOn.Time.UTC().Format(time.RFC3339),
		})
	}

	byBand := make(map[string]dxccBandSummary, len(byBandRows))
	for _, row := range byBandRows {
		byBand[row.Band] = dxccBandSummary{
			Worked:    row.Worked,
			Confirmed: row.Confirmed,
		}
	}

	entities := make([]dxccEntityProgress, 0, len(entityRows))
	for _, row := range entityRows {
		var firstQSO *string
		if row.FirstQso.Valid {
			v := row.FirstQso.Time.UTC().Format(time.RFC3339)
			firstQSO = &v
		}

		entity := dxccEntityProgress{
			EntityID:  row.EntityID,
			Name:      row.Name,
			Prefix:    row.Prefix,
			Continent: row.Continent,
			Worked:    row.Worked,
			Confirmed: row.Confirmed,
			Bands:     row.Bands,
			FirstQSO:  firstQSO,
			QSOCount:  row.QsoCount,
			QSOs:      entityQSOs[row.EntityID],
		}
		entities = append(entities, entity)
	}

	needed := total - summary.Worked
	if needed < 0 {
		needed = 0
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute dxcc progress", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "dxcc progress retrieved", dxccResponse{
		TotalEntities: total,
		Worked:        summary.Worked,
		Confirmed:     summary.Confirmed,
		Needed:        needed,
		ByBand:        byBand,
		Entities:      entities,
	})
}

type wasStateResponse struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Worked   bool    `json:"worked"`
	QSOCount int64   `json:"qso_count"`
	FirstQSO *string `json:"first_qso,omitempty"`
}

type wasResponse struct {
	TotalStates int64              `json:"total_states"`
	Worked      int64              `json:"worked"`
	Needed      int64              `json:"needed"`
	States      []wasStateResponse `json:"states"`
}

// WAS handles GET /v1/awards/was.
func (h *AwardsHandler) WAS(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.ListWorkedStates(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute was progress", "state query failed")
		return
	}

	type stateAgg struct {
		QSOCount int64
		FirstQSO *time.Time
	}

	workedByCode := make(map[string]stateAgg)
	for _, row := range rows {
		code, ok := normalizeUSState(row.StateValue)
		if !ok {
			continue
		}

		agg := workedByCode[code]
		agg.QSOCount += row.QsoCount
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			if agg.FirstQSO == nil || t.Before(*agg.FirstQSO) {
				tCopy := t
				agg.FirstQSO = &tCopy
			}
		}
		workedByCode[code] = agg
	}

	states := make([]wasStateResponse, 0, len(usStatesOrdered))
	workedCount := int64(0)
	for _, s := range usStatesOrdered {
		agg, worked := workedByCode[s.Code]
		if worked {
			workedCount++
		}

		var firstQSO *string
		if agg.FirstQSO != nil {
			v := agg.FirstQSO.UTC().Format(time.RFC3339)
			firstQSO = &v
		}

		states = append(states, wasStateResponse{
			Code:     s.Code,
			Name:     s.Name,
			Worked:   worked,
			QSOCount: agg.QSOCount,
			FirstQSO: firstQSO,
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute was progress", "transaction failed")
		return
	}

	total := int64(len(usStatesOrdered))
	writeSuccess(w, http.StatusOK, "was progress retrieved", wasResponse{
		TotalStates: total,
		Worked:      workedCount,
		Needed:      total - workedCount,
		States:      states,
	})
}

type gridSquareResponse struct {
	GridSquare string  `json:"grid_square"`
	QSOCount   int64   `json:"qso_count"`
	FirstQSO   *string `json:"first_qso,omitempty"`
	LastQSO    *string `json:"last_qso,omitempty"`
}

type gridsResponse struct {
	Target      int64                `json:"target"`
	Worked      int64                `json:"worked"`
	Needed      int64                `json:"needed"`
	ProgressPct float64              `json:"progress_pct"`
	GridSquares []gridSquareResponse `json:"grid_squares"`
}

// Grids handles GET /v1/awards/grids.
func (h *AwardsHandler) Grids(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.ListWorkedGridSquares(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute grid progress", "grid query failed")
		return
	}

	items := make([]gridSquareResponse, 0, len(rows))
	for _, row := range rows {
		var firstQSO *string
		if row.FirstQso.Valid {
			v := row.FirstQso.Time.UTC().Format(time.RFC3339)
			firstQSO = &v
		}
		var lastQSO *string
		if row.LastQso.Valid {
			v := row.LastQso.Time.UTC().Format(time.RFC3339)
			lastQSO = &v
		}

		items = append(items, gridSquareResponse{
			GridSquare: row.GridSquare,
			QSOCount:   row.QsoCount,
			FirstQSO:   firstQSO,
			LastQSO:    lastQSO,
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute grid progress", "transaction failed")
		return
	}

	const vuccTarget = int64(100)
	worked := int64(len(items))
	needed := vuccTarget - worked
	if needed < 0 {
		needed = 0
	}

	progress := 0.0
	if vuccTarget > 0 {
		progress = float64(worked) / float64(vuccTarget) * 100
	}

	writeSuccess(w, http.StatusOK, "grid progress retrieved", gridsResponse{
		Target:      vuccTarget,
		Worked:      worked,
		Needed:      needed,
		ProgressPct: progress,
		GridSquares: items,
	})
}

type potaAwardsResponse struct {
	ParksActivated   int64 `json:"parks_activated"`
	ParksHunted      int64 `json:"parks_hunted"`
	ActivationsTotal int64 `json:"activations_total"`
	ValidActivations int64 `json:"valid_activations"`
}

type sotaSummitResponse struct {
	SummitRef string  `json:"summit_ref"`
	QSOCount  int64   `json:"qso_count"`
	FirstQSO  *string `json:"first_qso,omitempty"`
	Confirmed bool    `json:"confirmed,omitempty"`
}

type sotaAwardsResponse struct {
	SummitsChased    int64                `json:"summits_chased"`
	SummitsActivated int64                `json:"summits_activated"`
	Chased           []sotaSummitResponse `json:"chased"`
	Activated        []sotaSummitResponse `json:"activated"`
}

// POTA handles GET /v1/awards/pota.
func (h *AwardsHandler) POTA(w http.ResponseWriter, r *http.Request) {
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
	summary, err := queries.GetPOTAAwardSummary(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute pota progress", "summary query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute pota progress", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "pota progress retrieved", potaAwardsResponse{
		ParksActivated:   summary.ParksActivated,
		ParksHunted:      summary.ParksHunted,
		ActivationsTotal: summary.ActivationsTotal,
		ValidActivations: summary.ValidActivations,
	})
}

// SOTA handles GET /v1/awards/sota.
func (h *AwardsHandler) SOTA(w http.ResponseWriter, r *http.Request) {
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
	chasedRows, err := queries.ListWorkedSOTASummits(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute sota progress", "chased query failed")
		return
	}
	activatedRows, err := queries.ListActivatedSOTASummits(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute sota progress", "activated query failed")
		return
	}

	chased := make([]sotaSummitResponse, 0, len(chasedRows))
	for _, row := range chasedRows {
		var firstQSO *string
		if row.FirstQso.Valid {
			v := row.FirstQso.Time.UTC().Format(time.RFC3339)
			firstQSO = &v
		}
		chased = append(chased, sotaSummitResponse{
			SummitRef: row.SummitRef,
			QSOCount:  row.QsoCount,
			FirstQSO:  firstQSO,
			Confirmed: row.Confirmed,
		})
	}

	activated := make([]sotaSummitResponse, 0, len(activatedRows))
	for _, row := range activatedRows {
		var firstQSO *string
		if row.FirstQso.Valid {
			v := row.FirstQso.Time.UTC().Format(time.RFC3339)
			firstQSO = &v
		}
		activated = append(activated, sotaSummitResponse{
			SummitRef: row.SummitRef,
			QSOCount:  row.QsoCount,
			FirstQSO:  firstQSO,
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to compute sota progress", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "sota progress retrieved", sotaAwardsResponse{
		SummitsChased:    int64(len(chased)),
		SummitsActivated: int64(len(activated)),
		Chased:           chased,
		Activated:        activated,
	})
}

type usState struct {
	Code string
	Name string
}

var usStatesOrdered = []usState{
	{Code: "AL", Name: "Alabama"},
	{Code: "AK", Name: "Alaska"},
	{Code: "AZ", Name: "Arizona"},
	{Code: "AR", Name: "Arkansas"},
	{Code: "CA", Name: "California"},
	{Code: "CO", Name: "Colorado"},
	{Code: "CT", Name: "Connecticut"},
	{Code: "DE", Name: "Delaware"},
	{Code: "FL", Name: "Florida"},
	{Code: "GA", Name: "Georgia"},
	{Code: "HI", Name: "Hawaii"},
	{Code: "ID", Name: "Idaho"},
	{Code: "IL", Name: "Illinois"},
	{Code: "IN", Name: "Indiana"},
	{Code: "IA", Name: "Iowa"},
	{Code: "KS", Name: "Kansas"},
	{Code: "KY", Name: "Kentucky"},
	{Code: "LA", Name: "Louisiana"},
	{Code: "ME", Name: "Maine"},
	{Code: "MD", Name: "Maryland"},
	{Code: "MA", Name: "Massachusetts"},
	{Code: "MI", Name: "Michigan"},
	{Code: "MN", Name: "Minnesota"},
	{Code: "MS", Name: "Mississippi"},
	{Code: "MO", Name: "Missouri"},
	{Code: "MT", Name: "Montana"},
	{Code: "NE", Name: "Nebraska"},
	{Code: "NV", Name: "Nevada"},
	{Code: "NH", Name: "New Hampshire"},
	{Code: "NJ", Name: "New Jersey"},
	{Code: "NM", Name: "New Mexico"},
	{Code: "NY", Name: "New York"},
	{Code: "NC", Name: "North Carolina"},
	{Code: "ND", Name: "North Dakota"},
	{Code: "OH", Name: "Ohio"},
	{Code: "OK", Name: "Oklahoma"},
	{Code: "OR", Name: "Oregon"},
	{Code: "PA", Name: "Pennsylvania"},
	{Code: "RI", Name: "Rhode Island"},
	{Code: "SC", Name: "South Carolina"},
	{Code: "SD", Name: "South Dakota"},
	{Code: "TN", Name: "Tennessee"},
	{Code: "TX", Name: "Texas"},
	{Code: "UT", Name: "Utah"},
	{Code: "VT", Name: "Vermont"},
	{Code: "VA", Name: "Virginia"},
	{Code: "WA", Name: "Washington"},
	{Code: "WV", Name: "West Virginia"},
	{Code: "WI", Name: "Wisconsin"},
	{Code: "WY", Name: "Wyoming"},
}

func normalizeUSState(value string) (string, bool) {
	return awardssvc.NormalizeUSState(value)
}

func init() {
	// Keep output stable and predictable in tests/UI.
	sort.SliceStable(usStatesOrdered, func(i, j int) bool {
		return usStatesOrdered[i].Code < usStatesOrdered[j].Code
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Unified award endpoints (GET /v1/awards, GET /v1/awards/:type,
// GET /v1/awards/:type/needs, POST /v1/awards/refresh)
// ─────────────────────────────────────────────────────────────────────────────

type awardSummaryItem struct {
	AwardType      string  `json:"award_type"`
	Worked         int64   `json:"worked"`
	Confirmed      int64   `json:"confirmed"`
	Target         int64   `json:"target,omitempty"`
	Needed         int64   `json:"needed,omitempty"`
	ProgressPct    float64 `json:"progress_pct,omitempty"`
	LatestQSOAt    *string `json:"latest_qso_at,omitempty"`
	CacheUpdatedAt *string `json:"cache_updated_at,omitempty"`
}

type awardListResponse struct {
	Awards []awardSummaryItem `json:"awards"`
}

// List handles GET /v1/awards.
// Returns a summary of progress for every award type from the award_progress cache.
func (h *AwardsHandler) List(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.GetAwardProgressSummary(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list awards", "summary query failed")
		return
	}

	items := make([]awardSummaryItem, 0, len(rows))
	for _, row := range rows {
		target := awardsTargetFor(row.AwardType)
		needed := int64(0)
		if target > 0 && row.WorkedCount < target {
			needed = target - row.WorkedCount
		}
		pct := 0.0
		if target > 0 {
			pct = float64(row.WorkedCount) / float64(target) * 100
		}

		item := awardSummaryItem{
			AwardType:   row.AwardType,
			Worked:      row.WorkedCount,
			Confirmed:   row.ConfirmedCount,
			Target:      target,
			Needed:      needed,
			ProgressPct: pct,
		}
		if row.LatestQsoAt.Valid {
			v := row.LatestQsoAt.Time.UTC().Format(time.RFC3339)
			item.LatestQSOAt = &v
		}
		if row.CacheUpdatedAt.Valid {
			v := row.CacheUpdatedAt.Time.UTC().Format(time.RFC3339)
			item.CacheUpdatedAt = &v
		}
		items = append(items, item)
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list awards", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "award progress retrieved", awardListResponse{Awards: items})
}

type awardDetailRow struct {
	EntityKey string  `json:"entity_key"`
	Band      *string `json:"band,omitempty"`
	Mode      *string `json:"mode,omitempty"`
	Worked    bool    `json:"worked"`
	Confirmed bool    `json:"confirmed"`
	QSOCount  int64   `json:"qso_count"`
	LastQSOAt *string `json:"last_qso_at,omitempty"`
	UpdatedAt string  `json:"updated_at"`
}

type awardDetailResponse struct {
	AwardType   string           `json:"award_type"`
	Worked      int64            `json:"worked"`
	Confirmed   int64            `json:"confirmed"`
	Target      int64            `json:"target,omitempty"`
	Needed      int64            `json:"needed,omitempty"`
	ProgressPct float64          `json:"progress_pct,omitempty"`
	Rows        []awardDetailRow `json:"rows"`
}

// ByType handles GET /v1/awards/:type.
// Returns detailed progress rows for the named award type from the cache.
func (h *AwardsHandler) ByType(w http.ResponseWriter, r *http.Request) {
	awardType := strings.ToLower(strings.TrimSpace(pathParam(r, "type")))
	if !isValidAwardType(awardType) {
		writeSuccess(w, http.StatusOK, "award type not found", nil)
		return
	}

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
	rows, err := queries.ListAwardProgressByType(r.Context(), awardType)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch award progress", "query failed")
		return
	}

	detail := make([]awardDetailRow, 0, len(rows))
	var workedCount, confirmedCount int64
	for _, row := range rows {
		if row.Worked {
			workedCount++
		}
		if row.Confirmed {
			confirmedCount++
		}
		d := awardDetailRow{
			EntityKey: row.EntityKey,
			Band:      row.Band,
			Mode:      row.Mode,
			Worked:    row.Worked,
			Confirmed: row.Confirmed,
			QSOCount:  row.QsoCount,
			UpdatedAt: row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		}
		if row.LastQsoAt.Valid {
			v := row.LastQsoAt.Time.UTC().Format(time.RFC3339)
			d.LastQSOAt = &v
		}
		detail = append(detail, d)
	}

	target := awardsTargetFor(awardType)
	needed := int64(0)
	if target > 0 && workedCount < target {
		needed = target - workedCount
	}
	pct := 0.0
	if target > 0 {
		pct = float64(workedCount) / float64(target) * 100
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch award progress", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, awardType+" progress retrieved", awardDetailResponse{
		AwardType:   awardType,
		Worked:      workedCount,
		Confirmed:   confirmedCount,
		Target:      target,
		Needed:      needed,
		ProgressPct: pct,
		Rows:        detail,
	})
}

type awardNeedsRow struct {
	EntityKey string `json:"entity_key"`
}

type awardNeedsResponse struct {
	AwardType string          `json:"award_type"`
	Needed    int64           `json:"needed"`
	Items     []awardNeedsRow `json:"items"`
}

// Needs handles GET /v1/awards/:type/needs.
// Returns a list of entity_keys not yet worked for bounded award types (DXCC, WAS, WAZ).
func (h *AwardsHandler) Needs(w http.ResponseWriter, r *http.Request) {
	awardType := strings.ToLower(strings.TrimSpace(pathParam(r, "type")))
	if !isValidAwardType(awardType) {
		writeSuccess(w, http.StatusOK, "award type not found", nil)
		return
	}

	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	// For unbounded award types, the "needs" concept is undefined.
	if awardsTargetFor(awardType) == 0 {
		writeSuccess(w, http.StatusOK, "needs list not applicable for unbounded award", awardNeedsResponse{
			AwardType: awardType,
			Needed:    0,
			Items:     []awardNeedsRow{},
		})
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rows, err := queries.ListAwardProgressByType(r.Context(), awardType)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch award needs", "query failed")
		return
	}

	// Build set of worked entity_keys.
	workedSet := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Worked {
			workedSet[row.EntityKey] = true
		}
	}

	// Build needs list from the canonical entity set for bounded award types.
	needed := awardNeedsFor(awardType, workedSet)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch award needs", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, awardType+" needs list retrieved", awardNeedsResponse{
		AwardType: awardType,
		Needed:    int64(len(needed)),
		Items:     needed,
	})
}

// Refresh handles POST /v1/awards/refresh.
// Enqueues a background AwardRefreshWorker job for the authenticated user so
// that WAZ, WPX, and all other award caches are fully recalculated.  Falls
// back to marking existing rows dirty when no River client is available.
func (h *AwardsHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	// Prefer immediate job enqueue so the recalculation runs right away even
	// when award_progress has no existing rows for the user (e.g. first load,
	// or new WAZ/WPX award types that were added after initial schema rollout).
	if h.riverClient != nil {
		_, err := h.riverClient.Insert(r.Context(), jobs.AwardRefreshArgs{UserID: userID}, nil)
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to schedule refresh", "job enqueue failed")
			return
		}
		writeSuccess(w, http.StatusOK, "award refresh scheduled", map[string]any{
			"queued": true,
			"note":   "background refresh triggered; progress will update shortly",
		})
		return
	}

	// Fallback: mark dirty so the periodic AwardProgressRefreshWorker picks it up.
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	if err := queries.MarkUserAwardsDirty(r.Context(), userID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to schedule refresh", "mark dirty failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to schedule refresh", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "award refresh scheduled", map[string]any{
		"queued": true,
		"note":   "background refresh triggered; progress will update shortly",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

var validAwardTypes = map[string]bool{
	"dxcc": true, "was": true, "vucc": true, "waz": true, "wpx": true,
	"pota_hunter": true, "pota_activator": true,
	"sota_chaser": true, "sota_activator": true,
}

func isValidAwardType(s string) bool { return validAwardTypes[s] }

func awardsTargetFor(awardType string) int64 {
	switch awardType {
	case "dxcc":
		return 340
	case "was":
		return 50
	case "vucc":
		return 100
	case "waz":
		return 40
	}
	return 0
}

// awardNeedsFor builds a "needs" list for bounded awards by diffing the
// canonical entity set against the worked set.
func awardNeedsFor(awardType string, worked map[string]bool) []awardNeedsRow {
	switch awardType {
	case "was":
		var out []awardNeedsRow
		for _, s := range usStatesOrdered {
			if !worked[s.Code] {
				out = append(out, awardNeedsRow{EntityKey: s.Code})
			}
		}
		return out
	case "waz":
		var out []awardNeedsRow
		for zone := int64(1); zone <= 40; zone++ {
			key := fmt.Sprintf("%d", zone)
			if !worked[key] {
				out = append(out, awardNeedsRow{EntityKey: key})
			}
		}
		return out
	}
	// DXCC and VUCC needs require the reference entity list from the database.
	// For those, callers can use the existing DXCC/Grids endpoints which include
	// per-entity worked status. A future enhancement will query dxcc_entities
	// directly from this handler.
	return []awardNeedsRow{}
}

// pathParam extracts a named URL parameter from a chi router context.
func pathParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
