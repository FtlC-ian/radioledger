package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/qsoenrich"
)

const qsoEnrichmentBatchSize = 500

type QSOEnrichmentBackfillArgs struct {
	UserID      int64      `json:"user_id"`
	LogbookUUID *uuid.UUID `json:"logbook_uuid,omitempty"`
}

func (QSOEnrichmentBackfillArgs) Kind() string { return "qso_enrichment_backfill" }

type QSOEnrichmentBackfillWorker struct {
	river.WorkerDefaults[QSOEnrichmentBackfillArgs]
	Pool *pgxpool.Pool
}

func (w *QSOEnrichmentBackfillWorker) Work(ctx context.Context, job *river.Job[QSOEnrichmentBackfillArgs]) error {
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("set worker role: %w", err)
	}

	args := job.Args
	total := 0
	for {
		changes, err := qsoenrich.EnrichAccessibleBatchFromCallsignRecords(ctx, conn, args.UserID, args.LogbookUUID, qsoEnrichmentBatchSize)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			break
		}

		if err := insertQSOEnrichmentAudit(ctx, conn, changes); err != nil {
			return err
		}
		total += len(changes)
	}

	slog.InfoContext(ctx, "qso callsign enrichment backfill complete",
		slog.Int64("user_id", args.UserID),
		slog.Int("updated", total),
	)

	queries := db.New(conn)
	if err := queries.MarkUserAwardsDirty(ctx, args.UserID); err != nil {
		slog.WarnContext(ctx, "failed to mark awards dirty after qso enrichment backfill",
			slog.Int64("user_id", args.UserID),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func insertQSOEnrichmentAudit(ctx context.Context, conn *pgxpool.Conn, changes []qsoenrich.CallsignBackfillChange) error {
	for _, c := range changes {
		oldValues := map[string]any{}
		newValues := map[string]any{}

		if changedString(c.OldState, c.NewState) {
			oldValues["state"] = nullableString(c.OldState)
			newValues["state"] = nullableString(c.NewState)
		}
		if changedString(c.OldCountry, c.NewCountry) {
			oldValues["country"] = nullableString(c.OldCountry)
			newValues["country"] = nullableString(c.NewCountry)
		}
		if changedInt16(c.OldCQZone, c.NewCQZone) {
			oldValues["cq_zone"] = nullableInt16(c.OldCQZone)
			newValues["cq_zone"] = nullableInt16(c.NewCQZone)
		}
		if changedInt16(c.OldITUZone, c.NewITUZone) {
			oldValues["itu_zone"] = nullableInt16(c.OldITUZone)
			newValues["itu_zone"] = nullableInt16(c.NewITUZone)
		}
		if changedString(c.OldGrid, c.NewGrid) {
			oldValues["gridsquare"] = nullableString(c.OldGrid)
			newValues["gridsquare"] = nullableString(c.NewGrid)
		}
		if changedString(c.OldName, c.NewName) {
			oldValues["name"] = nullableString(c.OldName)
			newValues["name"] = nullableString(c.NewName)
		}

		if len(newValues) == 0 {
			continue
		}

		oldJSON, err := json.Marshal(oldValues)
		if err != nil {
			return fmt.Errorf("marshal old enrichment values for qso %d: %w", c.QSOID, err)
		}
		newJSON, err := json.Marshal(newValues)
		if err != nil {
			return fmt.Errorf("marshal new enrichment values for qso %d: %w", c.QSOID, err)
		}

		if _, err := conn.Exec(ctx, `
			INSERT INTO audit_log (
				user_id, table_name, record_id, action, old_values, new_values, changed_by
			) VALUES ($1, 'qsos', $2, 'UPDATE', $3::jsonb, $4::jsonb, 'qso_enrichment_backfill')
		`, c.UserID, c.QSOID, oldJSON, newJSON); err != nil {
			return fmt.Errorf("insert audit_log for qso %d: %w", c.QSOID, err)
		}
	}

	return nil
}

func changedString(oldV, newV *string) bool {
	if oldV == nil && newV == nil {
		return false
	}
	if oldV == nil || newV == nil {
		return true
	}
	return *oldV != *newV
}

func changedInt16(oldV, newV *int16) bool {
	if oldV == nil && newV == nil {
		return false
	}
	if oldV == nil || newV == nil {
		return true
	}
	return *oldV != *newV
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt16(v *int16) any {
	if v == nil {
		return nil
	}
	return *v
}
