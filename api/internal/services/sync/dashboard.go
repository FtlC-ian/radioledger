package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncStatusFilter controls pagination and filtering for QSO sync status listing.
type SyncStatusFilter struct {
	Service  string
	Status   string
	Callsign string
	DateFrom *time.Time
	DateTo   *time.Time
	Page     int
	PageSize int
}

// QSOServiceSyncStatus is the per-service sync state for a single QSO.
type QSOServiceSyncStatus struct {
	SyncID       int64
	Service      string
	Status       string
	LastSyncedAt *time.Time
	RemoteID     *string
	ErrorMessage *string
	ErrorCode    *string
	RetryCount   int16
	NextRetryAt  *time.Time
}

// QSOSyncStatusRow is one dashboard row: QSO summary + per-service sync states.
type QSOSyncStatusRow struct {
	QSOID           int64
	QSOUUID         string
	Callsign        string
	Band            string
	Mode            string
	DatetimeOn      time.Time
	HasConflict     bool
	ConflictID      *int64
	ServiceStatuses []QSOServiceSyncStatus
}

// GetQSOSyncStatus returns paginated QSO rows with per-service sync status details.
func GetQSOSyncStatus(ctx context.Context, pool *pgxpool.Pool, userID int64, f SyncStatusFilter) ([]QSOSyncStatusRow, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 25
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	offset := (f.Page - 1) * f.PageSize

	countQuery := `
		WITH filtered AS (
			SELECT q.id
			FROM qsos q
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $1
			  AND q.deleted_at IS NULL
			  AND ($2 = '' OR q.callsign ILIKE '%' || $2 || '%')
			  AND ($3::timestamptz IS NULL OR q.datetime_on >= $3::timestamptz)
			  AND ($4::timestamptz IS NULL OR q.datetime_on <= $4::timestamptz)
			  AND ($5 = '' OR EXISTS (
				SELECT 1 FROM sync_status ss
				WHERE ss.qso_id = q.id AND ss.service = $5
			  ))
			  AND ($6 = '' OR EXISTS (
				SELECT 1 FROM sync_status ss
				WHERE ss.qso_id = q.id
				  AND ($5 = '' OR ss.service = $5)
				  AND ss.status = $6
			  ))
		)
		SELECT COUNT(*) FROM filtered
	`

	var total int64
	if err := pool.QueryRow(ctx, countQuery,
		userID,
		strings.TrimSpace(f.Callsign),
		f.DateFrom,
		f.DateTo,
		strings.TrimSpace(f.Service),
		strings.TrimSpace(f.Status),
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sync status rows: %w", err)
	}

	rowsQuery := `
		WITH filtered AS (
			SELECT
				q.id,
				q.uuid::text,
				q.callsign,
				q.band,
				q.mode,
				q.datetime_on,
				EXISTS (
					SELECT 1 FROM sync_conflicts sc
					WHERE sc.qso_id = q.id
					  AND sc.status = 'open'
				) AS has_conflict,
				(
					SELECT sc.id FROM sync_conflicts sc
					WHERE sc.qso_id = q.id
					  AND sc.status = 'open'
					ORDER BY sc.created_at DESC
					LIMIT 1
				) AS conflict_id
			FROM qsos q
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $1
			  AND q.deleted_at IS NULL
			  AND ($2 = '' OR q.callsign ILIKE '%' || $2 || '%')
			  AND ($3::timestamptz IS NULL OR q.datetime_on >= $3::timestamptz)
			  AND ($4::timestamptz IS NULL OR q.datetime_on <= $4::timestamptz)
			  AND ($5 = '' OR EXISTS (
				SELECT 1 FROM sync_status ss
				WHERE ss.qso_id = q.id AND ss.service = $5
			  ))
			  AND ($6 = '' OR EXISTS (
				SELECT 1 FROM sync_status ss
				WHERE ss.qso_id = q.id
				  AND ($5 = '' OR ss.service = $5)
				  AND ss.status = $6
			  ))
		),
		paged AS (
			SELECT *
			FROM filtered
			ORDER BY datetime_on DESC, id DESC
			LIMIT $7 OFFSET $8
		)
		SELECT
			p.id,
			p.uuid,
			p.callsign,
			p.band,
			p.mode,
			p.datetime_on,
			p.has_conflict,
			p.conflict_id,
			ss.id,
			ss.service,
			ss.status,
			ss.last_synced_at,
			ss.remote_id,
			ss.error_message,
			ss.last_error_code,
			ss.retry_count,
			ss.next_retry_at
		FROM paged p
		LEFT JOIN sync_status ss ON ss.qso_id = p.id
		ORDER BY p.datetime_on DESC, p.id DESC, ss.service ASC
	`

	rows, err := pool.Query(ctx, rowsQuery,
		userID,
		strings.TrimSpace(f.Callsign),
		f.DateFrom,
		f.DateTo,
		strings.TrimSpace(f.Service),
		strings.TrimSpace(f.Status),
		f.PageSize,
		offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query sync status rows: %w", err)
	}
	defer rows.Close()

	var result []QSOSyncStatusRow
	byQSO := map[int64]int{}

	for rows.Next() {
		var (
			qsoID                         int64
			qsoUUID, callsign, band, mode string
			datetimeOn                    pgtype.Timestamptz
			hasConflict                   bool
			conflictID                    *int64
			syncID                        *int64
			service, status               *string
			lastSyncedAt                  pgtype.Timestamptz
			remoteID, errMsg, errCode     *string
			retryCount                    *int16
			nextRetryAt                   pgtype.Timestamptz
		)

		if err := rows.Scan(
			&qsoID, &qsoUUID, &callsign, &band, &mode,
			&datetimeOn, &hasConflict, &conflictID,
			&syncID, &service, &status,
			&lastSyncedAt, &remoteID, &errMsg, &errCode,
			&retryCount, &nextRetryAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan sync status row: %w", err)
		}

		idx, exists := byQSO[qsoID]
		if !exists {
			row := QSOSyncStatusRow{
				QSOID:       qsoID,
				QSOUUID:     qsoUUID,
				Callsign:    callsign,
				Band:        band,
				Mode:        mode,
				HasConflict: hasConflict,
				ConflictID:  conflictID,
			}
			if datetimeOn.Valid {
				row.DatetimeOn = datetimeOn.Time
			}
			result = append(result, row)
			idx = len(result) - 1
			byQSO[qsoID] = idx
		}

		if syncID != nil && service != nil && status != nil {
			svc := QSOServiceSyncStatus{
				SyncID:       *syncID,
				Service:      *service,
				Status:       *status,
				RemoteID:     remoteID,
				ErrorMessage: errMsg,
				ErrorCode:    errCode,
			}
			if retryCount != nil {
				svc.RetryCount = *retryCount
			}
			if lastSyncedAt.Valid {
				t := lastSyncedAt.Time
				svc.LastSyncedAt = &t
			}
			if nextRetryAt.Valid {
				t := nextRetryAt.Time
				svc.NextRetryAt = &t
			}
			result[idx].ServiceStatuses = append(result[idx].ServiceStatuses, svc)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate sync status rows: %w", err)
	}

	return result, total, nil
}

// ServiceSyncProgress is aggregate upload progress for a service.
type ServiceSyncProgress struct {
	Service           string
	PendingCount      int64
	UploadedCount     int64
	FailedCount       int64
	TotalCount        int64
	LastActivityAt    *time.Time
	LastError         *string
	ErrorMessage      *string
	HasPermanentError bool
	IsRunning         bool
	IsStalled         bool
}

// GetSyncProgressByService returns per-service sync counts for the dashboard.
func GetSyncProgressByService(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]ServiceSyncProgress, error) {
	var hasRiverJobs bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.river_job') IS NOT NULL`).Scan(&hasRiverJobs); err != nil {
		return nil, fmt.Errorf("check river_job table: %w", err)
	}

	query := `
		WITH service_counts AS (
			SELECT
				ss.service,
				COUNT(*) FILTER (WHERE ss.status IN ('pending', 'dirty'))      AS pending_count,
				COUNT(*) FILTER (WHERE ss.status IN ('uploaded', 'confirmed')) AS uploaded_count,
				COUNT(*) FILTER (WHERE ss.status = 'error')                    AS failed_count,
				COUNT(*)                                                       AS total_count,
				MAX(ss.updated_at)                                             AS last_activity_at
			FROM sync_status ss
			JOIN qsos q ON q.id = ss.qso_id
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $1
			GROUP BY ss.service
		),
		last_errors AS (
			SELECT DISTINCT ON (ss.service)
				ss.service,
				ss.error_message,
				ss.last_error_code
			FROM sync_status ss
			JOIN qsos q ON q.id = ss.qso_id
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $1
			  AND ss.status = 'error'
			  AND COALESCE(ss.error_message, '') <> ''
			ORDER BY ss.service, ss.updated_at DESC
		)
		SELECT
			sc.service,
			sc.pending_count,
			sc.uploaded_count,
			sc.failed_count,
			sc.total_count,
			sc.last_activity_at,
			le.error_message,
			COALESCE(le.last_error_code = 'permanent_failure', false) AS has_permanent_error,
			FALSE AS is_running
		FROM service_counts sc
		LEFT JOIN last_errors le ON le.service = sc.service
		ORDER BY sc.service ASC
	`
	if hasRiverJobs {
		query = `
			WITH service_counts AS (
				SELECT
					ss.service,
					COUNT(*) FILTER (WHERE ss.status IN ('pending', 'dirty'))      AS pending_count,
					COUNT(*) FILTER (WHERE ss.status IN ('uploaded', 'confirmed')) AS uploaded_count,
					COUNT(*) FILTER (WHERE ss.status = 'error')                    AS failed_count,
					COUNT(*)                                                       AS total_count,
					MAX(ss.updated_at)                                             AS last_activity_at
				FROM sync_status ss
				JOIN qsos q ON q.id = ss.qso_id
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				GROUP BY ss.service
			),
			running_jobs AS (
				SELECT
					CASE
						WHEN kind LIKE 'eqsl_%' THEN 'eqsl'
						WHEN kind LIKE 'clublog_%' THEN 'clublog'
						WHEN kind LIKE 'lotw_%' THEN 'lotw'
						WHEN kind LIKE 'qrz_%' THEN 'qrz'
						WHEN kind LIKE 'hamqth_%' THEN 'hamqth'
						ELSE NULL
					END AS service,
					COUNT(*) AS running_count
				FROM river_job
				WHERE state = 'running'
				  AND args->>'user_id' = $1::text
				GROUP BY 1
			),
			last_errors AS (
				SELECT DISTINCT ON (ss.service)
					ss.service,
					ss.error_message,
					ss.last_error_code
				FROM sync_status ss
				JOIN qsos q ON q.id = ss.qso_id
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND ss.status = 'error'
				  AND COALESCE(ss.error_message, '') <> ''
				ORDER BY ss.service, ss.updated_at DESC
			)
			SELECT
				sc.service,
				sc.pending_count,
				sc.uploaded_count,
				sc.failed_count,
				sc.total_count,
				sc.last_activity_at,
				le.error_message,
				COALESCE(le.last_error_code = 'permanent_failure', false) AS has_permanent_error,
				COALESCE(rj.running_count, 0) > 0 AS is_running
			FROM service_counts sc
			LEFT JOIN running_jobs rj ON rj.service = sc.service
			LEFT JOIN last_errors le ON le.service = sc.service
			ORDER BY sc.service ASC
		`
	}

	rows, err := pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query sync progress rows: %w", err)
	}
	defer rows.Close()

	out := make([]ServiceSyncProgress, 0)
	now := time.Now()
	for rows.Next() {
		var row ServiceSyncProgress
		var lastActivity pgtype.Timestamptz
		if err := rows.Scan(
			&row.Service,
			&row.PendingCount,
			&row.UploadedCount,
			&row.FailedCount,
			&row.TotalCount,
			&lastActivity,
			&row.ErrorMessage,
			&row.HasPermanentError,
			&row.IsRunning,
		); err != nil {
			return nil, fmt.Errorf("scan sync progress row: %w", err)
		}
		if lastActivity.Valid {
			t := lastActivity.Time
			row.LastActivityAt = &t
		}
		row.LastError = row.ErrorMessage
		if row.PendingCount > 0 && !row.IsRunning && row.LastActivityAt != nil && now.Sub(*row.LastActivityAt) >= 60*time.Second {
			row.IsStalled = true
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync progress rows: %w", err)
	}

	// Supplement with services that have active credentials but no sync_status rows yet.
	// This handles fresh installs and the race between credential save and the async backfill,
	// ensuring sync buttons are enabled immediately when a user has pending QSOs.
	if err := appendUnsyncedCredentialServices(ctx, pool, userID, &out); err != nil {
		// Non-fatal: log and continue with what we have.
		_ = err
	}

	return out, nil
}

// appendUnsyncedCredentialServices adds ServiceSyncProgress entries for any service
// in user_service_credentials (is_active=TRUE) that is not already represented in out.
// The pending_count is the number of QSOs with no sync_status row for that service
// marked uploaded/confirmed — i.e., QSOs that still need to be synced.
func appendUnsyncedCredentialServices(ctx context.Context, pool *pgxpool.Pool, userID int64, out *[]ServiceSyncProgress) error {
	// Build a set of services already in the result.
	present := make(map[string]struct{}, len(*out))
	for _, r := range *out {
		present[r.Service] = struct{}{}
	}

	credRows, err := pool.Query(ctx, `
		SELECT service FROM user_service_credentials
		WHERE user_id = $1 AND is_active = TRUE
	`, userID)
	if err != nil {
		return fmt.Errorf("query credential services: %w", err)
	}
	defer credRows.Close()

	var missing []string
	for credRows.Next() {
		var svc string
		if err := credRows.Scan(&svc); err != nil {
			continue
		}
		if _, found := present[svc]; !found {
			missing = append(missing, svc)
		}
	}
	_ = credRows.Err()

	for _, svc := range missing {
		// Count QSOs that have not been successfully synced to this service yet.
		var pendingCount int64
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(q.id)
			FROM qsos q
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $1
			  AND q.deleted_at IS NULL
			  AND NOT EXISTS (
			      SELECT 1 FROM sync_status ss
			      WHERE ss.qso_id = q.id
			        AND ss.service = $2
			        AND ss.status IN ('uploaded', 'confirmed')
			  )
		`, userID, svc).Scan(&pendingCount); err != nil {
			continue
		}
		if pendingCount > 0 {
			*out = append(*out, ServiceSyncProgress{
				Service:      svc,
				PendingCount: pendingCount,
				TotalCount:   pendingCount,
			})
		}
	}
	return nil
}

// BulkUploadUnsynced marks unsynced QSOs for a service as pending and inserts missing rows.
func BulkUploadUnsynced(ctx context.Context, pool *pgxpool.Pool, userID int64, service string) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	insertTag, err := tx.Exec(ctx, `
		INSERT INTO sync_status (qso_id, service, status, updated_at)
		SELECT q.id, $2, 'pending', NOW()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		LEFT JOIN sync_status ss ON ss.qso_id = q.id AND ss.service = $2
		WHERE lb.user_id = $1
		  AND q.deleted_at IS NULL
		  AND ss.id IS NULL
	`, userID, service)
	if err != nil {
		return 0, fmt.Errorf("insert missing sync rows: %w", err)
	}

	updateTag, err := tx.Exec(ctx, `
		UPDATE sync_status ss
		SET status = 'pending',
			next_retry_at = NULL,
			error_message = NULL,
			last_error_code = NULL,
			updated_at = NOW()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE ss.qso_id = q.id
		  AND lb.user_id = $1
		  AND q.deleted_at IS NULL
		  AND ss.service = $2
		  AND ss.status IN ('error', 'rejected', 'skipped', 'not_applicable')
	`, userID, service)
	if err != nil {
		return 0, fmt.Errorf("mark unsynced rows pending: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	return insertTag.RowsAffected() + updateTag.RowsAffected(), nil
}

// SyncConflictRow is one conflict row from sync_conflicts.
type SyncConflictRow struct {
	ID             int64
	QSOID          int64
	QSOUUID        string
	Callsign       string
	Band           string
	Mode           string
	DatetimeOn     time.Time
	ServiceA       string
	ServiceB       string
	FieldConflicts map[string]map[string]any
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// GetSyncConflicts returns paginated conflict rows for the user.
func GetSyncConflicts(ctx context.Context, pool *pgxpool.Pool, userID int64, page, pageSize int) ([]SyncConflictRow, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM sync_conflicts sc
		JOIN qsos q ON q.id = sc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND q.deleted_at IS NULL
		  AND sc.status = 'open'
	`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count conflicts: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT
			sc.id,
			sc.qso_id,
			q.uuid::text,
			q.callsign,
			q.band,
			q.mode,
			q.datetime_on,
			sc.service_a,
			sc.service_b,
			sc.field_conflicts,
			sc.status,
			sc.created_at,
			sc.updated_at
		FROM sync_conflicts sc
		JOIN qsos q ON q.id = sc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND q.deleted_at IS NULL
		  AND sc.status = 'open'
		ORDER BY sc.created_at DESC, sc.id DESC
		LIMIT $2 OFFSET $3
	`, userID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query conflicts: %w", err)
	}
	defer rows.Close()

	var result []SyncConflictRow
	for rows.Next() {
		var (
			row       SyncConflictRow
			datetime  pgtype.Timestamptz
			createdAt pgtype.Timestamptz
			updatedAt pgtype.Timestamptz
			raw       []byte
		)
		if err := rows.Scan(
			&row.ID,
			&row.QSOID,
			&row.QSOUUID,
			&row.Callsign,
			&row.Band,
			&row.Mode,
			&datetime,
			&row.ServiceA,
			&row.ServiceB,
			&raw,
			&row.Status,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan conflict row: %w", err)
		}
		if datetime.Valid {
			row.DatetimeOn = datetime.Time
		}
		if createdAt.Valid {
			row.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			row.UpdatedAt = updatedAt.Time
		}
		row.FieldConflicts = map[string]map[string]any{}
		_ = json.Unmarshal(raw, &row.FieldConflicts)
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate conflicts: %w", err)
	}

	return result, total, nil
}

// ResolveSyncConflict marks a conflict as resolved with field-level service picks.
func ResolveSyncConflict(ctx context.Context, pool *pgxpool.Pool, userID int64, conflictID int64, resolution map[string]string) (bool, error) {
	if len(resolution) == 0 {
		return false, fmt.Errorf("resolution cannot be empty")
	}

	resolutionJSON, err := json.Marshal(resolution)
	if err != nil {
		return false, fmt.Errorf("marshal resolution: %w", err)
	}

	services := make([]string, 0, len(resolution))
	for _, svc := range resolution {
		services = append(services, svc)
	}
	sort.Strings(services)
	resolvedBy := ""
	if len(services) > 0 {
		resolvedBy = services[0]
		for _, svc := range services[1:] {
			if svc != resolvedBy {
				resolvedBy = ""
				break
			}
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var qsoID int64
	var serviceA, serviceB string
	if err := tx.QueryRow(ctx, `
		SELECT sc.qso_id, sc.service_a, sc.service_b
		FROM sync_conflicts sc
		JOIN qsos q ON q.id = sc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE sc.id = $1
		  AND lb.user_id = $2
		  AND sc.status = 'open'
	`, conflictID, userID).Scan(&qsoID, &serviceA, &serviceB); err != nil {
		return false, nil
	}

	tag, err := tx.Exec(ctx, `
		UPDATE sync_conflicts
		SET status = 'resolved',
			resolution = $2::jsonb,
			resolved_by_service = NULLIF($3, ''),
			resolved_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
	`, conflictID, string(resolutionJSON), resolvedBy)
	if err != nil {
		return false, fmt.Errorf("update conflict: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE sync_status
		SET status = 'pending',
			next_retry_at = NULL,
			error_message = NULL,
			last_error_code = NULL,
			updated_at = NOW()
		WHERE qso_id = $1
		  AND service IN ($2, $3)
		  AND status = 'error'
	`, qsoID, serviceA, serviceB)
	if err != nil {
		return false, fmt.Errorf("mark related sync rows pending: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}

	return true, nil
}

// RetryFailedSyncs retries failed sync rows for a single QSO UUID, a service, or both.
func RetryFailedSyncs(ctx context.Context, pool *pgxpool.Pool, userID int64, qsoUUID, service string) (int64, []string, error) {
	query := `
		UPDATE sync_status ss
		SET status = 'pending',
			next_retry_at = NULL,
			error_message = NULL,
			last_error_code = NULL,
			retry_count = 0,
			updated_at = NOW()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE ss.qso_id = q.id
		  AND lb.user_id = $1
		  AND q.deleted_at IS NULL
		  AND ss.status = 'error'
		  AND ($2 = '' OR q.uuid = $2::uuid)
		  AND ($3 = '' OR ss.service = $3)
		RETURNING ss.service
	`

	r, err := pool.Query(ctx, query, userID, strings.TrimSpace(qsoUUID), strings.TrimSpace(service))
	if err != nil {
		return 0, nil, fmt.Errorf("retry failed sync rows: %w", err)
	}
	defer r.Close()

	distinct := map[string]struct{}{}
	var affected int64
	for r.Next() {
		var svc string
		if err := r.Scan(&svc); err != nil {
			return 0, nil, fmt.Errorf("scan retried service: %w", err)
		}
		affected++
		distinct[svc] = struct{}{}
	}
	if err := r.Err(); err != nil {
		return 0, nil, fmt.Errorf("iterate retried services: %w", err)
	}

	services := make([]string, 0, len(distinct))
	for svc := range distinct {
		services = append(services, svc)
	}
	sort.Strings(services)

	return affected, services, nil
}
