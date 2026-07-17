package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ensureQSOEditDirtyTrigger(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		ALTER TABLE sync_status DROP CONSTRAINT IF EXISTS sync_status_status_check;
		ALTER TABLE sync_status
		ADD CONSTRAINT sync_status_status_check CHECK (status IN (
			'pending', 'dirty', 'uploaded', 'confirmed',
			'error', 'rejected', 'not_applicable', 'skipped'
		));

		CREATE OR REPLACE FUNCTION mark_sync_dirty_on_qso_edit() RETURNS TRIGGER AS $$
		BEGIN
			IF (
				OLD.callsign IS DISTINCT FROM NEW.callsign
				OR OLD.band IS DISTINCT FROM NEW.band
				OR OLD.mode IS DISTINCT FROM NEW.mode
				OR OLD.submode IS DISTINCT FROM NEW.submode
				OR OLD.frequency_hz IS DISTINCT FROM NEW.frequency_hz
				OR OLD.datetime_on IS DISTINCT FROM NEW.datetime_on
				OR OLD.datetime_off IS DISTINCT FROM NEW.datetime_off
				OR OLD.rst_sent IS DISTINCT FROM NEW.rst_sent
				OR OLD.rst_rcvd IS DISTINCT FROM NEW.rst_rcvd
				OR OLD.tx_power IS DISTINCT FROM NEW.tx_power
				OR OLD.gridsquare IS DISTINCT FROM NEW.gridsquare
				OR OLD.my_gridsquare IS DISTINCT FROM NEW.my_gridsquare
				OR OLD.name IS DISTINCT FROM NEW.name
				OR OLD.station_callsign IS DISTINCT FROM NEW.station_callsign
				OR OLD.contest_id IS DISTINCT FROM NEW.contest_id
				OR OLD.srx IS DISTINCT FROM NEW.srx
				OR OLD.stx IS DISTINCT FROM NEW.stx
			) THEN
				UPDATE sync_status
				SET status = 'dirty', updated_at = NOW()
				WHERE qso_id = NEW.id
				  AND status IN ('uploaded', 'confirmed');
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;

		DROP TRIGGER IF EXISTS trg_qso_edit_mark_dirty ON qsos;
		CREATE TRIGGER trg_qso_edit_mark_dirty
			AFTER UPDATE ON qsos
			FOR EACH ROW
			EXECUTE FUNCTION mark_sync_dirty_on_qso_edit();
	`)
	if err != nil {
		t.Fatalf("ensure qso dirty trigger: %v", err)
	}
}

func TestIntegration_QSOEditMarksUploadedSyncRowsDirty(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureQSOEditDirtyTrigger(t, pool)
	user := createTestUser(t, pool, "qso-dirty-adif")
	logbook := createLogbookViaAPI(t, h, user.ID, "Dirty Sync Logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "K1DIRTY",
		"band":        "20m",
		"mode":        "SSB",
		"datetime_on": now.Format(time.RFC3339),
	})

	_, err := pool.Exec(context.Background(), `
		INSERT INTO sync_status (qso_id, service, status, remote_id, updated_at)
		SELECT id, 'qrz', 'uploaded', '12345', NOW()
		FROM qsos
		WHERE uuid = $1::uuid
		ON CONFLICT (qso_id, service) DO UPDATE
		SET status = EXCLUDED.status,
			remote_id = EXCLUDED.remote_id,
			updated_at = NOW()
	`, qso.UUID)
	if err != nil {
		t.Fatalf("seed sync_status: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		UPDATE qsos
		SET band = '40m', updated_at = NOW()
		WHERE uuid = $1::uuid
	`, qso.UUID)
	if err != nil {
		t.Fatalf("update qso band: %v", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(), `
		SELECT ss.status
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		WHERE q.uuid = $1::uuid
		  AND ss.service = 'qrz'
	`, qso.UUID).Scan(&status); err != nil {
		t.Fatalf("query sync status: %v", err)
	}
	if status != "dirty" {
		t.Fatalf("expected sync status to become dirty after ADIF-relevant edit, got %q", status)
	}
}

func TestIntegration_QSOEditNonADIFFieldDoesNotMarkDirty(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureQSOEditDirtyTrigger(t, pool)
	user := createTestUser(t, pool, "qso-dirty-notes")
	logbook := createLogbookViaAPI(t, h, user.ID, "Dirty Sync Logbook Notes", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "K1NOTE",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Format(time.RFC3339),
	})

	_, err := pool.Exec(context.Background(), `
		INSERT INTO sync_status (qso_id, service, status, remote_id, updated_at)
		SELECT id, 'qrz', 'uploaded', '67890', NOW()
		FROM qsos
		WHERE uuid = $1::uuid
		ON CONFLICT (qso_id, service) DO UPDATE
		SET status = EXCLUDED.status,
			remote_id = EXCLUDED.remote_id,
			updated_at = NOW()
	`, qso.UUID)
	if err != nil {
		t.Fatalf("seed sync_status: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		UPDATE qsos
		SET notes = 'updated notes only', updated_at = NOW()
		WHERE uuid = $1::uuid
	`, qso.UUID)
	if err != nil {
		t.Fatalf("update qso notes: %v", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(), `
		SELECT ss.status
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		WHERE q.uuid = $1::uuid
		  AND ss.service = 'qrz'
	`, qso.UUID).Scan(&status); err != nil {
		t.Fatalf("query sync status: %v", err)
	}
	if status != "uploaded" {
		t.Fatalf("expected sync status to stay uploaded after non-ADIF edit, got %q", status)
	}
}
