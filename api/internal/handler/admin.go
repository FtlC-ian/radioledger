package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

type AdminHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
}

func NewAdminHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *AdminHandler {
	return &AdminHandler{pool: pool, riverClient: riverClient}
}

type AdminJob struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Args        any        `json:"args,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	AttemptedAt *time.Time `json:"attempted_at"`
	FinalizedAt *time.Time `json:"finalized_at"`
	Duration    *string    `json:"duration"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	Errors      []string   `json:"errors"`
}

type sourceOverview struct {
	Source            string     `json:"source"`
	Flag              string     `json:"flag"`
	RecordCount       int64      `json:"record_count"`
	LastSyncAt        *time.Time `json:"last_sync_at"`
	LastSyncStatus    *string    `json:"last_sync_status,omitempty"`
	NextScheduledSync *time.Time `json:"next_scheduled_sync"`
}

type serviceOverview struct {
	Service        string     `json:"service"`
	PendingCount   int64      `json:"pending_count"`
	UploadedCount  int64      `json:"uploaded_count"`
	FailedCount    int64      `json:"failed_count"`
	LastActivityAt *time.Time `json:"last_activity_at"`
}

func (h *AdminHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	stateFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)

	items, err := h.listJobsFull(r.Context(), stateFilter, kindFilter, limit, offset)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		items, err = h.listJobsFallback(r.Context(), stateFilter, kindFilter, limit, offset)
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "jobs unavailable", "query failed")
		return
	}

	writeSuccess(w, http.StatusOK, "admin jobs", map[string]any{
		"items":       items,
		"limit":       limit,
		"offset":      offset,
		"next_offset": offset + len(items),
	})
}

func (h *AdminHandler) listJobsFull(ctx context.Context, stateFilter, kindFilter string, limit, offset int) ([]AdminJob, error) {
	states := stateFilterValues(stateFilter)
	rows, err := h.pool.Query(ctx, `
		SELECT id, kind, state::text, args, created_at, attempted_at, finalized_at,
		       attempt::int, max_attempts::int,
		       COALESCE(array_to_json(errors), '[]')::text AS errors
		FROM river_job
		WHERE ($1::text[] IS NULL OR state::text = ANY($1::text[]))
		  AND ($2 = '' OR kind = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, states, kindFilter, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminJob, 0, limit)
	for rows.Next() {
		var (
			job                      AdminJob
			rawArgs, rawErrors       []byte
			createdAt                time.Time
			attemptedAt, finalizedAt *time.Time
		)
		if err := rows.Scan(&job.ID, &job.Kind, &job.State, &rawArgs, &createdAt, &attemptedAt, &finalizedAt, &job.Attempt, &job.MaxAttempts, &rawErrors); err != nil {
			return nil, err
		}
		job.CreatedAt = createdAt.UTC()
		job.AttemptedAt = normalizeTimePtr(attemptedAt)
		job.FinalizedAt = normalizeTimePtr(finalizedAt)
		job.Duration = computeDuration(job.AttemptedAt, job.FinalizedAt)
		job.Args = sanitizeArgs(rawArgs)
		job.Errors = parseErrors(rawErrors)
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (h *AdminHandler) listJobsFallback(ctx context.Context, stateFilter, kindFilter string, limit, offset int) ([]AdminJob, error) {
	states := stateFilterValues(stateFilter)
	rows, err := h.pool.Query(ctx, `
		SELECT id, kind, state::text, args, created_at
		FROM river_job
		WHERE ($1::text[] IS NULL OR state::text = ANY($1::text[]))
		  AND ($2 = '' OR kind = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, states, kindFilter, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminJob, 0, limit)
	for rows.Next() {
		var (
			job       AdminJob
			rawArgs   []byte
			createdAt time.Time
		)
		if err := rows.Scan(&job.ID, &job.Kind, &job.State, &rawArgs, &createdAt); err != nil {
			return nil, err
		}
		job.CreatedAt = createdAt.UTC()
		job.Attempt = 0
		job.MaxAttempts = 0
		job.Args = sanitizeArgs(rawArgs)
		job.Errors = []string{}
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (h *AdminHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid job id")
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE river_job
		SET state = 'available',
		    scheduled_at = NOW(),
		    attempted_at = NULL,
		    finalized_at = NULL,
		    errors = '[]'::jsonb,
		    updated_at = NOW()
		WHERE id = $1
		  AND state IN ('discarded', 'retryable', 'cancelled', 'failed', 'error')
	`, id)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		tag, err = h.pool.Exec(r.Context(), `
			UPDATE river_job
			SET state = 'available', updated_at = NOW()
			WHERE id = $1
		`, id)
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "retry failed", "could not reset job")
		return
	}
	if tag.RowsAffected() == 0 {
		writeFailure(w, http.StatusNotFound, "not found", "job not found or not retryable")
		return
	}

	writeSuccess(w, http.StatusAccepted, "job retry queued", map[string]any{"id": id})
}

type triggerSyncRequest struct {
	Kind string `json:"kind"`
}

type genericJobArgs struct {
	kind string
}

func (g genericJobArgs) Kind() string { return g.kind }

func (h *AdminHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	if h.riverClient == nil {
		writeFailure(w, http.StatusServiceUnavailable, "sync unavailable", "river client is not configured")
		return
	}

	var req triggerSyncRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "kind is required")
		return
	}

	if _, err := h.riverClient.Insert(r.Context(), genericJobArgs{kind: kind}, nil); err != nil {
		writeFailure(w, http.StatusBadRequest, "trigger failed", fmt.Sprintf("could not enqueue job kind %q", kind))
		return
	}

	writeSuccess(w, http.StatusAccepted, "sync job triggered", map[string]any{"kind": kind})
}

func (h *AdminHandler) SyncOverview(w http.ResponseWriter, r *http.Request) {
	sources, err := h.getSourceOverview(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "overview unavailable", "could not load callsign source overview")
		return
	}
	services, err := h.getServiceOverview(r.Context())
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "overview unavailable", "could not load sync service overview")
		return
	}

	writeSuccess(w, http.StatusOK, "sync overview", map[string]any{
		"sources":  sources,
		"services": services,
	})
}

func (h *AdminHandler) getSourceOverview(ctx context.Context) ([]sourceOverview, error) {
	sourceFlags := map[string]string{
		"fcc":          "🇺🇸",
		"ised":         "🇨🇦",
		"acma":         "🇦🇺",
		"anfr":         "🇫🇷",
		"ift":          "🇪🇸",
		"rdi":          "🇮🇹",
		"ofcom":        "🇬🇧",
		"bnetza":       "🇩🇪",
		"nbtc":         "🇹🇭",
		"jj1wtl":       "🇯🇵",
		"pota_parks":   "🏕️",
		"sota_summits": "⛰️",
	}
	sourceKinds := map[string][]string{
		"fcc":          {"fcc_daily_sync", "fcc_weekly_sync"},
		"ised":         {"ised_weekly_sync"},
		"acma":         {"acma_weekly_sync"},
		"anfr":         {"anfr_weekly_sync"},
		"ift":          {"ift_weekly_sync"},
		"rdi":          {"rdi_weekly_sync"},
		"ofcom":        {"ofcom_weekly_sync"},
		"bnetza":       {"bnetza_weekly_sync"},
		"nbtc":         {"nbtc_sync"},
		"jj1wtl":       {"jj1wtl_monthly_sync"},
		"pota_parks":   {"pota_park_sync"},
		"sota_summits": {"sota_summit_sync"},
	}

	sources := []string{"fcc", "ised", "acma", "anfr", "ift", "rdi", "ofcom", "bnetza", "nbtc", "jj1wtl", "pota_parks", "sota_summits"}
	out := make([]sourceOverview, 0, len(sources))
	for _, source := range sources {
		out = append(out, sourceOverview{Source: source, Flag: sourceFlags[source]})
	}

	recordCounts := map[string]int64{}
	rows, err := h.pool.Query(ctx, `SELECT source, COUNT(*) FROM callsign_records GROUP BY source`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var source string
			var count int64
			if scanErr := rows.Scan(&source, &count); scanErr == nil {
				recordCounts[strings.ToLower(source)] = count
			}
		}
	}

	// Augment record counts for POTA/SOTA reference tables (not in callsign_records).
	var potaCount int64
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pota_parks`).Scan(&potaCount); err == nil {
		recordCounts["pota_parks"] = potaCount
	}
	var sotaCount int64
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sota_summits`).Scan(&sotaCount); err == nil {
		recordCounts["sota_summits"] = sotaCount
	}

	lastRuns := map[string]struct {
		at     *time.Time
		status *string
	}{}
	rows, err = h.pool.Query(ctx, `
		SELECT DISTINCT ON (source)
			source,
			COALESCE(completed_at, started_at) AS last_sync_at,
			status
		FROM callsign_sync_runs
		ORDER BY source, started_at DESC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var source string
			var ts *time.Time
			var status *string
			if scanErr := rows.Scan(&source, &ts, &status); scanErr == nil {
				timeVal := normalizeTimePtr(ts)
				lastRuns[strings.ToLower(source)] = struct {
					at     *time.Time
					status *string
				}{at: timeVal, status: status}
			}
		}
	}

	nextByKind := map[string]*time.Time{}
	rows, err = h.pool.Query(ctx, `
		SELECT kind, MIN(scheduled_at) AS next_time
		FROM river_job
		WHERE kind = ANY($1::text[])
		  AND state IN ('scheduled', 'available', 'retryable', 'running')
		GROUP BY kind
	`, flattenKindMap(sourceKinds))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		rows, err = h.pool.Query(ctx, `
			SELECT kind, MIN(created_at) AS next_time
			FROM river_job
			WHERE kind = ANY($1::text[])
			  AND state IN ('scheduled', 'available', 'retryable', 'running')
			GROUP BY kind
		`, flattenKindMap(sourceKinds))
	}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var kind string
			var ts *time.Time
			if scanErr := rows.Scan(&kind, &ts); scanErr == nil {
				nextByKind[kind] = normalizeTimePtr(ts)
			}
		}
	}

	for i := range out {
		source := out[i].Source
		out[i].RecordCount = recordCounts[source]
		if run, ok := lastRuns[source]; ok {
			out[i].LastSyncAt = run.at
			out[i].LastSyncStatus = run.status
		}
		for _, kind := range sourceKinds[source] {
			candidate := nextByKind[kind]
			if candidate == nil {
				continue
			}
			if out[i].NextScheduledSync == nil || candidate.Before(*out[i].NextScheduledSync) {
				out[i].NextScheduledSync = candidate
			}
		}
	}

	return out, nil
}

func (h *AdminHandler) getServiceOverview(ctx context.Context) ([]serviceOverview, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT
			service,
			COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
			COUNT(*) FILTER (WHERE status IN ('uploaded', 'confirmed')) AS uploaded_count,
			COUNT(*) FILTER (WHERE status = 'error') AS failed_count,
			MAX(updated_at) AS last_activity_at
		FROM sync_status
		WHERE service IN ('qrz', 'eqsl', 'clublog')
		GROUP BY service
		ORDER BY service ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := map[string]serviceOverview{
		"qrz":     {Service: "qrz"},
		"eqsl":    {Service: "eqsl"},
		"clublog": {Service: "clublog"},
	}
	for rows.Next() {
		var (
			service string
			row     serviceOverview
			ts      *time.Time
		)
		if err := rows.Scan(&service, &row.PendingCount, &row.UploadedCount, &row.FailedCount, &ts); err != nil {
			return nil, err
		}
		row.Service = service
		row.LastActivityAt = normalizeTimePtr(ts)
		all[service] = row
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return []serviceOverview{all["qrz"], all["eqsl"], all["clublog"]}, nil
}

func normalizeTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func computeDuration(start, end *time.Time) *string {
	if start == nil {
		return nil
	}
	stop := time.Now().UTC()
	if end != nil {
		stop = end.UTC()
	}
	d := stop.Sub(start.UTC())
	if d < 0 {
		d = 0
	}
	s := d.Round(time.Second).String()
	return &s
}

func stateFilterValues(state string) []string {
	switch state {
	case "running":
		return []string{"running"}
	case "completed":
		return []string{"completed"}
	case "scheduled":
		return []string{"scheduled", "available", "retryable"}
	case "failed":
		return []string{"discarded", "retryable", "cancelled", "failed", "error"}
	default:
		return nil
	}
}

func sanitizeArgs(raw []byte) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return map[string]any{}
	}
	return sanitizeValue(data)
}

func sanitizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			if shouldRedact(k) {
				out[k] = "[redacted]"
				continue
			}
			out[k] = sanitizeValue(vv)
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for _, item := range val {
			out = append(out, sanitizeValue(item))
		}
		return out
	default:
		return val
	}
}

func shouldRedact(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"password", "secret", "token", "key", "credential", "auth"} {
		if strings.Contains(k, needle) {
			return true
		}
	}
	return false
}

func parseErrors(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		var one string
		if json.Unmarshal(raw, &one) == nil && strings.TrimSpace(one) != "" {
			return []string{one}
		}
		return []string{}
	}

	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				out = append(out, v)
			}
		case map[string]any:
			if msg, ok := v["message"].(string); ok && strings.TrimSpace(msg) != "" {
				out = append(out, msg)
				continue
			}
			if msg, ok := v["error"].(string); ok && strings.TrimSpace(msg) != "" {
				out = append(out, msg)
			}
		}
	}
	return out
}

func flattenKindMap(m map[string][]string) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, kinds := range m {
		for _, kind := range kinds {
			if _, ok := seen[kind]; ok {
				continue
			}
			seen[kind] = struct{}{}
			out = append(out, kind)
		}
	}
	return out
}
