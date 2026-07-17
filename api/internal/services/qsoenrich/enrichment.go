package qsoenrich

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// EnrichLogbook fills missing DXCC/country/zone/continent fields for QSOs in one logbook.
// Intended for worker-side use where logbook ownership is already known/trusted.
func EnrichLogbook(ctx context.Context, db execer, logbookID int64) (int64, error) {
	tag, err := runEnrichmentExec(ctx, db, enrichmentUpdateSQL(`q0.logbook_id = $1 AND q0.deleted_at IS NULL`, true), logbookID)
	if err != nil {
		return 0, fmt.Errorf("enrich logbook qsos: %w", err)
	}
	return tag.RowsAffected(), nil
}

// EnrichAccessible fills missing metadata for all QSOs the current tenant can update.
// When logbookUUID is provided, only that logbook is processed.
func EnrichAccessible(ctx context.Context, db execer, logbookUUID *uuid.UUID) (int64, error) {
	tag, err := runEnrichmentExec(ctx, db, enrichmentUpdateSQL(`
		q0.deleted_at IS NULL
		AND l.deleted_at IS NULL
		AND app_has_logbook_min_role(l.id, app_current_user_id(), 'operator')
		AND ($1::uuid IS NULL OR l.uuid = $1::uuid)
	`, true), logbookUUID)
	if err != nil {
		return 0, fmt.Errorf("enrich accessible qsos: %w", err)
	}
	return tag.RowsAffected(), nil
}

func runEnrichmentExec(ctx context.Context, db execer, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := db.Exec(ctx, sql, args...)
	if err == nil {
		return tag, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		// Backward-compatible fallback for deployments/tests that haven't applied
		// the dxcc_prefixes migration yet.
		fallback := strings.Replace(sql, prefixJoinSQL, prefixJoinNullSQL, 1)
		tag, err2 := db.Exec(ctx, fallback, args...)
		if err2 == nil {
			return tag, nil
		}
	}

	return pgconn.CommandTag{}, err
}

const prefixJoinSQL = `LEFT JOIN LATERAL (
		SELECT d.entity_id, d.name, d.cq_zone, d.itu_zone, d.continent
		FROM dxcc_prefixes p
		JOIN dxcc_entities d ON d.entity_id = p.entity_id
		WHERE UPPER(BTRIM(q0.callsign)) LIKE p.prefix || '%'
		ORDER BY LENGTH(p.prefix) DESC, p.prefix ASC
		LIMIT 1
	) d_prefix ON TRUE`

const prefixJoinNullSQL = `LEFT JOIN LATERAL (
		SELECT
			NULL::integer AS entity_id,
			NULL::text AS name,
			NULL::smallint AS cq_zone,
			NULL::smallint AS itu_zone,
			NULL::text AS continent
	) d_prefix ON TRUE`

func enrichmentUpdateSQL(scopeWhere string, includePrefix bool) string {
	prefixJoin := ""
	if includePrefix {
		prefixJoin = prefixJoinSQL
	}
	return `
UPDATE qsos AS q
SET
	dxcc = COALESCE(q.dxcc, c.resolved_dxcc),
	country = COALESCE(NULLIF(BTRIM(q.country), ''), c.resolved_country),
	cq_zone = COALESCE(q.cq_zone, c.resolved_cq_zone),
	itu_zone = COALESCE(q.itu_zone, c.resolved_itu_zone),
	continent = COALESCE(NULLIF(BTRIM(q.continent), ''), c.resolved_continent)
FROM (
	SELECT
		q0.id,
		COALESCE(q0.dxcc, d_country.entity_id, cr.dxcc_entity_id, d_prefix.entity_id) AS resolved_dxcc,
		COALESCE(
			NULLIF(BTRIM(q0.country), ''),
			NULLIF(BTRIM(cr.country), ''),
			d_current.name,
			d_country.name,
			d_callsign.name,
			d_prefix.name
		) AS resolved_country,
		COALESCE(q0.cq_zone, d_current.cq_zone, d_country.cq_zone, d_callsign.cq_zone, d_prefix.cq_zone) AS resolved_cq_zone,
		COALESCE(q0.itu_zone, d_current.itu_zone, d_country.itu_zone, d_callsign.itu_zone, d_prefix.itu_zone) AS resolved_itu_zone,
		COALESCE(NULLIF(BTRIM(q0.continent), ''), d_current.continent, d_country.continent, d_callsign.continent, d_prefix.continent) AS resolved_continent
	FROM qsos q0
	JOIN logbooks l ON l.id = q0.logbook_id
	LEFT JOIN dxcc_entities d_current ON d_current.entity_id = q0.dxcc
	LEFT JOIN LATERAL (
		SELECT d.entity_id, d.name, d.cq_zone, d.itu_zone, d.continent
		FROM dxcc_entities d
		WHERE NULLIF(BTRIM(q0.country), '') IS NOT NULL
		  AND (
			UPPER(BTRIM(d.name)) = UPPER(BTRIM(q0.country))
			OR (
				d.lotw_entity_name IS NOT NULL
				AND UPPER(BTRIM(d.lotw_entity_name)) = UPPER(BTRIM(q0.country))
			)
		  )
		ORDER BY d.entity_id
		LIMIT 1
	) d_country ON TRUE
	LEFT JOIN LATERAL (
		SELECT cr.dxcc_entity_id, cr.country
		FROM callsign_records cr
		WHERE cr.callsign = UPPER(BTRIM(q0.callsign))
		ORDER BY cr.updated_at DESC NULLS LAST, cr.fetched_at DESC NULLS LAST, cr.id DESC
		LIMIT 1
	) cr ON TRUE
	LEFT JOIN dxcc_entities d_callsign ON d_callsign.entity_id = cr.dxcc_entity_id
	` + prefixJoin + `
	WHERE ` + scopeWhere + `
) c
WHERE q.id = c.id
  AND (
		(q.dxcc IS NULL AND c.resolved_dxcc IS NOT NULL)
		OR (NULLIF(BTRIM(q.country), '') IS NULL AND c.resolved_country IS NOT NULL)
		OR (q.cq_zone IS NULL AND c.resolved_cq_zone IS NOT NULL)
		OR (q.itu_zone IS NULL AND c.resolved_itu_zone IS NOT NULL)
		OR (NULLIF(BTRIM(q.continent), '') IS NULL AND c.resolved_continent IS NOT NULL)
  );`
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type CallsignBackfillChange struct {
	QSOID      int64
	UserID     int64
	OldState   *string
	NewState   *string
	OldCountry *string
	NewCountry *string
	OldCQZone  *int16
	NewCQZone  *int16
	OldITUZone *int16
	NewITUZone *int16
	OldGrid    *string
	NewGrid    *string
	OldName    *string
	NewName    *string
}

// EnrichOneFromCallsignRecords fills missing QSO fields from callsign_records.
// Only empty/NULL fields are populated; existing user-entered values are preserved.
func EnrichOneFromCallsignRecords(ctx context.Context, db execer, qsoID int64) (bool, error) {
	tag, err := db.Exec(ctx, callsignEnrichmentSQL(`q0.id = $1`, 1), qsoID)
	if err != nil {
		return false, fmt.Errorf("enrich qso %d from callsign records: %w", qsoID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// EnrichAccessibleBatchFromCallsignRecords backfills up to batchSize QSOs the given
// user can operate (owner/admin/operator), returning detailed before/after changes.
func EnrichAccessibleBatchFromCallsignRecords(ctx context.Context, db queryer, userID int64, logbookUUID *uuid.UUID, batchSize int) ([]CallsignBackfillChange, error) {
	if batchSize <= 0 {
		batchSize = 500
	}

	rows, err := db.Query(ctx, callsignBackfillBatchSQL(batchSize), userID, logbookUUID)
	if err != nil {
		return nil, fmt.Errorf("enrich qso batch from callsign records: %w", err)
	}
	defer rows.Close()

	changes := make([]CallsignBackfillChange, 0, batchSize)
	for rows.Next() {
		var c CallsignBackfillChange
		if scanErr := rows.Scan(
			&c.QSOID,
			&c.UserID,
			&c.OldState,
			&c.NewState,
			&c.OldCountry,
			&c.NewCountry,
			&c.OldCQZone,
			&c.NewCQZone,
			&c.OldITUZone,
			&c.NewITUZone,
			&c.OldGrid,
			&c.NewGrid,
			&c.OldName,
			&c.NewName,
		); scanErr != nil {
			return nil, fmt.Errorf("scan qso callsign enrichment result: %w", scanErr)
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate qso callsign enrichment results: %w", err)
	}

	return changes, nil
}

func callsignEnrichmentSQL(scopeWhere string, limit int) string {
	return `
WITH candidates AS (
	SELECT
		q0.id,
		q0.state AS old_state,
		q0.country AS old_country,
		q0.cq_zone AS old_cq_zone,
		q0.itu_zone AS old_itu_zone,
		q0.gridsquare AS old_grid,
		q0.name AS old_name,
		NULLIF(BTRIM(q0.state), '') AS current_state,
		NULLIF(BTRIM(q0.country), '') AS current_country,
		NULLIF(BTRIM(q0.gridsquare), '') AS current_grid,
		NULLIF(BTRIM(q0.name), '') AS current_name,
		NULLIF(BTRIM(cr.state_province), '') AS enrich_state,
		NULLIF(BTRIM(cr.country), '') AS enrich_country,
		dx.cq_zone AS enrich_cq_zone,
		dx.itu_zone AS enrich_itu_zone,
		UPPER(NULLIF(BTRIM(cr.grid_square), '')) AS enrich_grid,
		COALESCE(
			NULLIF(BTRIM(cr.full_name), ''),
			NULLIF(BTRIM(CONCAT_WS(' ', NULLIF(BTRIM(cr.first_name), ''), NULLIF(BTRIM(cr.last_name), ''))), '')
		) AS enrich_name
	FROM qsos q0
	LEFT JOIN LATERAL (
		SELECT
			cr.state_province,
			cr.country,
			cr.grid_square,
			cr.full_name,
			cr.first_name,
			cr.last_name,
			cr.dxcc_entity_id
		FROM callsign_records cr
		WHERE UPPER(BTRIM(cr.callsign)) = UPPER(BTRIM(q0.callsign))
		ORDER BY cr.updated_at DESC NULLS LAST, cr.fetched_at DESC NULLS LAST, cr.id DESC
		LIMIT 1
	) cr ON TRUE
	LEFT JOIN dxcc_entities dx ON dx.entity_id = cr.dxcc_entity_id
	WHERE ` + scopeWhere + `
), ranked AS (
	SELECT *
	FROM candidates c
	WHERE
		(c.current_state IS NULL AND c.enrich_state IS NOT NULL)
		OR (c.current_country IS NULL AND c.enrich_country IS NOT NULL)
		OR (c.old_cq_zone IS NULL AND c.enrich_cq_zone IS NOT NULL)
		OR (c.old_itu_zone IS NULL AND c.enrich_itu_zone IS NOT NULL)
		OR (c.current_grid IS NULL AND c.enrich_grid IS NOT NULL)
		OR (c.current_name IS NULL AND c.enrich_name IS NOT NULL)
	ORDER BY c.id ASC
	LIMIT ` + fmt.Sprintf("%d", limit) + `
)
UPDATE qsos q
SET
	state = COALESCE(NULLIF(BTRIM(q.state), ''), ranked.enrich_state),
	country = COALESCE(NULLIF(BTRIM(q.country), ''), ranked.enrich_country),
	cq_zone = COALESCE(q.cq_zone, ranked.enrich_cq_zone),
	itu_zone = COALESCE(q.itu_zone, ranked.enrich_itu_zone),
	gridsquare = COALESCE(NULLIF(BTRIM(q.gridsquare), ''), ranked.enrich_grid),
	name = COALESCE(NULLIF(BTRIM(q.name), ''), ranked.enrich_name),
	updated_at = NOW()
FROM ranked
WHERE q.id = ranked.id
  AND q.deleted_at IS NULL;
`
}

func callsignBackfillBatchSQL(batchSize int) string {
	return `
WITH candidates AS (
	SELECT
		q0.id,
		l.user_id,
		q0.state AS old_state,
		q0.country AS old_country,
		q0.cq_zone AS old_cq_zone,
		q0.itu_zone AS old_itu_zone,
		q0.gridsquare AS old_grid,
		q0.name AS old_name,
		NULLIF(BTRIM(q0.state), '') AS current_state,
		NULLIF(BTRIM(q0.country), '') AS current_country,
		NULLIF(BTRIM(q0.gridsquare), '') AS current_grid,
		NULLIF(BTRIM(q0.name), '') AS current_name,
		NULLIF(BTRIM(cr.state_province), '') AS enrich_state,
		NULLIF(BTRIM(cr.country), '') AS enrich_country,
		dx.cq_zone AS enrich_cq_zone,
		dx.itu_zone AS enrich_itu_zone,
		UPPER(NULLIF(BTRIM(cr.grid_square), '')) AS enrich_grid,
		COALESCE(
			NULLIF(BTRIM(cr.full_name), ''),
			NULLIF(BTRIM(CONCAT_WS(' ', NULLIF(BTRIM(cr.first_name), ''), NULLIF(BTRIM(cr.last_name), ''))), '')
		) AS enrich_name
	FROM qsos q0
	JOIN logbooks l ON l.id = q0.logbook_id
	JOIN user_roles ur ON ur.logbook_id = l.id AND ur.user_id = $1
	LEFT JOIN LATERAL (
		SELECT
			cr.state_province,
			cr.country,
			cr.grid_square,
			cr.full_name,
			cr.first_name,
			cr.last_name,
			cr.dxcc_entity_id
		FROM callsign_records cr
		WHERE UPPER(BTRIM(cr.callsign)) = UPPER(BTRIM(q0.callsign))
		ORDER BY cr.updated_at DESC NULLS LAST, cr.fetched_at DESC NULLS LAST, cr.id DESC
		LIMIT 1
	) cr ON TRUE
	LEFT JOIN dxcc_entities dx ON dx.entity_id = cr.dxcc_entity_id
	WHERE q0.deleted_at IS NULL
	  AND l.deleted_at IS NULL
	  AND ur.role IN ('owner', 'admin', 'operator')
	  AND ($2::uuid IS NULL OR l.uuid = $2::uuid)
), ranked AS (
	SELECT *
	FROM candidates c
	WHERE
		(c.current_state IS NULL AND c.enrich_state IS NOT NULL)
		OR (c.current_country IS NULL AND c.enrich_country IS NOT NULL)
		OR (c.old_cq_zone IS NULL AND c.enrich_cq_zone IS NOT NULL)
		OR (c.old_itu_zone IS NULL AND c.enrich_itu_zone IS NOT NULL)
		OR (c.current_grid IS NULL AND c.enrich_grid IS NOT NULL)
		OR (c.current_name IS NULL AND c.enrich_name IS NOT NULL)
	ORDER BY c.id ASC
	LIMIT ` + fmt.Sprintf("%d", batchSize) + `
), updated AS (
	UPDATE qsos q
	SET
		state = COALESCE(NULLIF(BTRIM(q.state), ''), ranked.enrich_state),
		country = COALESCE(NULLIF(BTRIM(q.country), ''), ranked.enrich_country),
		cq_zone = COALESCE(q.cq_zone, ranked.enrich_cq_zone),
		itu_zone = COALESCE(q.itu_zone, ranked.enrich_itu_zone),
		gridsquare = COALESCE(NULLIF(BTRIM(q.gridsquare), ''), ranked.enrich_grid),
		name = COALESCE(NULLIF(BTRIM(q.name), ''), ranked.enrich_name),
		updated_at = NOW()
	FROM ranked
	WHERE q.id = ranked.id
	  AND q.deleted_at IS NULL
	RETURNING
		q.id,
		ranked.user_id,
		ranked.old_state,
		q.state AS new_state,
		ranked.old_country,
		q.country AS new_country,
		ranked.old_cq_zone,
		q.cq_zone AS new_cq_zone,
		ranked.old_itu_zone,
		q.itu_zone AS new_itu_zone,
		ranked.old_grid,
		q.gridsquare AS new_grid,
		ranked.old_name,
		q.name AS new_name
)
SELECT
	id,
	user_id,
	old_state,
	new_state,
	old_country,
	new_country,
	old_cq_zone,
	new_cq_zone,
	old_itu_zone,
	new_itu_zone,
	old_grid,
	new_grid,
	old_name,
	new_name
FROM updated
ORDER BY id ASC;
`
}
