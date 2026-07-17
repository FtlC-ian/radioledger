// lotw_worker.go — River worker for LoTW (Logbook of the World) sync.
//
// LoTW sync flow is different from other services (eQSL, Club Log) because it
// involves a cryptographic signing step via the lotw-vault microservice before
// uploading to ARRL. The vault password is stored encrypted in user_service_credentials
// (service="lotw", credential_type="username_password") and is loaded by the worker
// at job execution time — it is never passed through River job args.
//
// # Job lifecycle
//
//  1. Handler creates a lotw_sync_jobs row (status='pending') with the QSO IDs.
//  2. Handler enqueues a LoTWSyncArgs River job referencing that row.
//  3. Worker fetches the job row, updates status → 'signing'.
//  4. Worker loads the vault password from user_service_credentials.
//  5. Worker fetches QSO data, generates ADIF, gets cert info from vault.
//  6. Worker calls vault /sign → receives .tq8 blob.
//  7. Worker updates status → 'uploading', uploads .tq8 to ARRL.
//  8. Worker marks QSOs with lotw_sent_at = NOW().
//  9. Worker updates status → 'completed' or 'failed'.
//
// 10. Worker upserts lotw_sync_status for the user.
package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	lotw "github.com/FtlC-ian/radioledger/api/internal/services/lotw"
)

// ──────────────────────────────────────────────────────────────────────────────
// Job args
// ──────────────────────────────────────────────────────────────────────────────

// LoTWSyncArgs is the River job payload for a LoTW signing + upload operation.
type LoTWSyncArgs struct {
	// JobID references the lotw_sync_jobs row that tracks this operation.
	JobID int64 `json:"job_id"`
	// UserID is the owning user's internal DB ID.
	UserID int64 `json:"user_id"`
	// VaultUserID is the identifier used with the lotw-vault microservice.
	// Format: "local:{user_id}" for local auth, "zitadel:{subject}" for Zitadel.
	VaultUserID string `json:"vault_user_id"`
	// VaultPassword is no longer carried in job args. The worker loads it from
	// user_service_credentials (service="lotw") at execution time.
	// Kept as an unexported blank to avoid breaking existing serialized rows during
	// a rolling deploy; can be removed in a future cleanup.
}

// Kind returns the unique River job kind string for LoTW sync.
func (LoTWSyncArgs) Kind() string { return "lotw_sync" }

// ──────────────────────────────────────────────────────────────────────────────
// Worker
// ──────────────────────────────────────────────────────────────────────────────

// LoTWSyncWorker is the River worker that signs and uploads LoTW QSOs.
type LoTWSyncWorker struct {
	river.WorkerDefaults[LoTWSyncArgs]
	Pool        *pgxpool.Pool
	VaultClient *lotw.VaultClient
	Keyring     *crypto.Keyring
}

// Work executes the LoTW sync job.
func (w *LoTWSyncWorker) Work(ctx context.Context, job *river.Job[LoTWSyncArgs]) error {
	args := job.Args
	log := slog.With(
		slog.Int64("job_id", args.JobID),
		slog.Int64("user_id", args.UserID),
		slog.String("vault_user_id", args.VaultUserID),
	)

	// ── 1. Fetch job row and QSO IDs ────────────────────────────────────────
	qsoIDs, err := w.fetchJobQSOIDs(ctx, args.JobID, args.UserID)
	if err != nil {
		return fmt.Errorf("fetch lotw job qso_ids: %w", err)
	}
	if len(qsoIDs) == 0 {
		log.WarnContext(ctx, "lotw sync job has no qso_ids, marking completed")
		return w.updateJobStatus(ctx, args.JobID, "completed", "", 0, "")
	}

	// ── 1b. Load vault password from credential store ─────────────────────────
	credentialBytes, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, args.UserID, "lotw")
	if err != nil {
		_ = w.failJob(ctx, args.JobID, fmt.Sprintf("load vault password: %s", err))
		return fmt.Errorf("load lotw vault password: %w", err)
	}
	if len(credentialBytes) == 0 {
		msg := "No LoTW vault password found. Re-import your certificate in LoTW Settings."
		_ = w.failJob(ctx, args.JobID, msg)
		log.ErrorContext(ctx, "lotw sync: no vault password in credential store")
		return nil // permanent failure — no point retrying
	}
	passwords, err := lotw.DecodeStoredPasswords(credentialBytes)
	if err != nil {
		_ = w.failJob(ctx, args.JobID, fmt.Sprintf("decode lotw credentials: %s", err))
		return fmt.Errorf("decode lotw credentials: %w", err)
	}
	if strings.TrimSpace(passwords.VaultPassword) == "" {
		msg := "No LoTW vault password found. Re-import your certificate in LoTW Settings."
		_ = w.failJob(ctx, args.JobID, msg)
		log.ErrorContext(ctx, "lotw sync: missing vault password in credential store")
		return nil
	}
	vaultPassword := passwords.VaultPassword

	// ── 2. Mark job as running ───────────────────────────────────────────────
	if err := w.updateJobStarted(ctx, args.JobID, "running"); err != nil {
		log.WarnContext(ctx, "failed to update lotw job status to running", slog.String("error", err.Error()))
	}

	// ── 3. Fetch QSO data ────────────────────────────────────────────────────
	qsos, err := w.fetchQSOsForIDs(ctx, qsoIDs, args.UserID)
	if err != nil {
		_ = w.failJob(ctx, args.JobID, fmt.Sprintf("fetch QSOs: %s", err))
		return fmt.Errorf("fetch qsos for lotw: %w", err)
	}
	if len(qsos) == 0 {
		log.WarnContext(ctx, "no eligible QSOs found for lotw sync IDs")
		_ = w.failJob(ctx, args.JobID, "no eligible QSOs found")
		return nil
	}

	log.InfoContext(ctx, "lotw sync: fetched QSOs", slog.Int("count", len(qsos)))

	// ── 4. Build ADIF ────────────────────────────────────────────────────────
	adifData, err := lotw.BuildADIF(qsos)
	if err != nil {
		_ = w.failJob(ctx, args.JobID, fmt.Sprintf("build ADIF: %s", err))
		return fmt.Errorf("build lotw ADIF: %w", err)
	}

	// ── 5. Get cert info for station metadata ────────────────────────────────
	certInfo, err := w.VaultClient.GetCertInfo(ctx, args.VaultUserID)
	if err != nil {
		msg := certInfoErrMsg(err)
		_ = w.failJob(ctx, args.JobID, msg)
		log.ErrorContext(ctx, "lotw cert-info failed", slog.String("error", err.Error()))
		return nil // permanent failure — wrong password / no cert
	}

	station := lotw.StationInfo{
		Callsign:   certInfo.Callsign,
		DXCC:       certInfo.DXCC,
		Gridsquare: certInfo.Gridsquare,
		CQZ:        certInfo.CQZ,
		ITUZ:       certInfo.ITUZ,
	}

	// ── 6. Sign via vault ────────────────────────────────────────────────────
	tq8Data, err := w.VaultClient.Sign(ctx, args.VaultUserID, vaultPassword, adifData, station)
	if err != nil {
		msg := signErrMsg(err)
		_ = w.failJob(ctx, args.JobID, msg)
		log.ErrorContext(ctx, "lotw vault sign failed", slog.String("error", err.Error()))
		// Wrong password / no cert are permanent; transient errors will be retried by River.
		if errors.Is(err, lotw.ErrWrongPassword) || errors.Is(err, lotw.ErrNoCert) {
			return nil
		}
		return fmt.Errorf("vault sign: %w", err)
	}

	log.InfoContext(ctx, "lotw vault sign succeeded", slog.Int("tq8_bytes", len(tq8Data)))

	// ── 7. Mark job as running (uploading phase) ──────────────────────────────
	if err := w.updateJobStatus(ctx, args.JobID, "running", "", len(tq8Data), ""); err != nil {
		log.WarnContext(ctx, "failed to update lotw job status to running (upload)", slog.String("error", err.Error()))
	}

	// ── 8. Upload .tq8 to ARRL ───────────────────────────────────────────────
	result, err := lotw.UploadToARRL(ctx, tq8Data)
	if err != nil {
		_ = w.failJob(ctx, args.JobID, fmt.Sprintf("ARRL upload error: %s", err))
		return fmt.Errorf("ARRL upload: %w", err)
	}

	if !result.Accepted {
		msg := fmt.Sprintf("ARRL did not accept upload: %s", truncate(result.RawResponse, 200))
		_ = w.updateJobStatus(ctx, args.JobID, "failed", msg, len(tq8Data), result.RawResponse)
		log.WarnContext(ctx, "ARRL upload not accepted", slog.String("response", truncate(result.RawResponse, 200)))
		return fmt.Errorf("ARRL upload not accepted")
	}

	log.InfoContext(ctx, "ARRL upload accepted", slog.Int("qso_count", len(qsos)))

	// ── 9. Mark QSOs as sent ─────────────────────────────────────────────────
	sentAt := time.Now().UTC()
	if err := w.markQSOsSent(ctx, qsoIDs, args.JobID, sentAt); err != nil {
		log.WarnContext(ctx, "failed to mark qsos as lotw sent", slog.String("error", err.Error()))
		// Non-fatal — the upload succeeded; best effort update.
	}

	// ── 9b. Upsert sync_status rows so the Sync Dashboard shows LoTW status ─
	if err := w.upsertSyncStatusRows(ctx, qsoIDs, sentAt); err != nil {
		log.WarnContext(ctx, "failed to upsert sync_status rows for lotw", slog.String("error", err.Error()))
	}

	// ── 10. Mark job completed ────────────────────────────────────────────────
	if err := w.updateJobStatus(ctx, args.JobID, "completed", "", len(tq8Data), result.RawResponse); err != nil {
		log.WarnContext(ctx, "failed to update lotw job to completed", slog.String("error", err.Error()))
	}
	if err := w.updateJobCompleted(ctx, args.JobID); err != nil {
		log.WarnContext(ctx, "failed to set completed_at on lotw job", slog.String("error", err.Error()))
	}

	// ── 11. Upsert lotw_sync_status ───────────────────────────────────────────
	if err := w.upsertSyncStatus(ctx, args.UserID, len(qsos), "accepted", ""); err != nil {
		log.WarnContext(ctx, "failed to upsert lotw_sync_status", slog.String("error", err.Error()))
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// DB helpers
// ──────────────────────────────────────────────────────────────────────────────

// fetchJobQSOIDs loads the qso_ids JSONB array from a lotw_sync_jobs row.
// Verifies ownership by matching user_id.
func (w *LoTWSyncWorker) fetchJobQSOIDs(ctx context.Context, jobID, userID int64) ([]int64, error) {
	row := w.Pool.QueryRow(ctx, `
		SELECT qso_ids FROM lotw_sync_jobs WHERE id = $1 AND user_id = $2
	`, jobID, userID)

	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, fmt.Errorf("scan qso_ids: %w", err)
	}
	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("unmarshal qso_ids: %w", err)
	}
	return ids, nil
}

// loTWQSORow is a local alias to LoTWQSORow for clarity — the worker maps DB
// results to the ADIF-level struct.
type loTWQSORow = lotw.LoTWQSORow

// fetchQSOsForIDs fetches QSO data for a list of IDs, filtering to rows
// owned by userID. QSOs that are soft-deleted or already sent are excluded.
func (w *LoTWSyncWorker) fetchQSOsForIDs(ctx context.Context, qsoIDs []int64, userID int64) ([]loTWQSORow, error) {
	rows, err := w.Pool.Query(ctx, `
		SELECT
			q.id,
			q.callsign,
			q.band,
			q.mode,
			q.submode,
			q.datetime_on,
			q.rst_sent,
			q.rst_rcvd,
			q.gridsquare,
			q.my_gridsquare,
			q.station_callsign,
			q.frequency_hz
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE q.id = ANY($1::bigint[])
		  AND lb.user_id = $2
		  AND q.deleted_at IS NULL
		ORDER BY q.datetime_on ASC
	`, qsoIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("query qsos: %w", err)
	}
	defer rows.Close()

	var result []loTWQSORow
	for rows.Next() {
		var r loTWQSORow
		var datetimeOn pgtype.Timestamptz
		if err := rows.Scan(
			&r.QSOID,
			&r.Callsign,
			&r.Band,
			&r.Mode,
			&r.Submode,
			&datetimeOn,
			&r.RstSent,
			&r.RstRcvd,
			&r.Gridsquare,
			&r.MyGridsquare,
			&r.StationCallsign,
			&r.FrequencyHz,
		); err != nil {
			return nil, fmt.Errorf("scan qso row: %w", err)
		}
		if datetimeOn.Valid {
			r.DatetimeOn = datetimeOn.Time
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// updateJobStarted sets the started_at timestamp and status on a lotw_sync_jobs row.
func (w *LoTWSyncWorker) updateJobStarted(ctx context.Context, jobID int64, status string) error {
	_, err := w.Pool.Exec(ctx, `
		UPDATE lotw_sync_jobs
		SET status = $2, started_at = NOW()
		WHERE id = $1
	`, jobID, status)
	return err
}

// updateJobStatus updates status, error, tq8_size, and arrl_response on a lotw_sync_jobs row.
func (w *LoTWSyncWorker) updateJobStatus(ctx context.Context, jobID int64, status, errMsg string, tq8Size int, arrlResponse string) error {
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	var arrlPtr *string
	if arrlResponse != "" {
		arrlPtr = &arrlResponse
	}
	var sizePtr *int
	if tq8Size > 0 {
		sizePtr = &tq8Size
	}
	_, err := w.Pool.Exec(ctx, `
		UPDATE lotw_sync_jobs
		SET status = $2,
		    error = COALESCE($3, error),
		    tq8_size = COALESCE($4, tq8_size),
		    arrl_response = COALESCE($5, arrl_response)
		WHERE id = $1
	`, jobID, status, errPtr, sizePtr, arrlPtr)
	return err
}

// updateJobCompleted sets the completed_at timestamp.
func (w *LoTWSyncWorker) updateJobCompleted(ctx context.Context, jobID int64) error {
	_, err := w.Pool.Exec(ctx, `
		UPDATE lotw_sync_jobs SET completed_at = NOW() WHERE id = $1
	`, jobID)
	return err
}

// failJob marks a lotw_sync_jobs row as failed with the provided error message.
func (w *LoTWSyncWorker) failJob(ctx context.Context, jobID int64, errMsg string) error {
	_, err := w.Pool.Exec(ctx, `
		UPDATE lotw_sync_jobs
		SET status = 'failed',
		    error = $2,
		    completed_at = NOW(),
		    retry_count = retry_count + 1
		WHERE id = $1
	`, jobID, errMsg)
	return err
}

// markQSOsSent sets lotw_sent_at and lotw_sync_job_id on the uploaded QSOs.
func (w *LoTWSyncWorker) markQSOsSent(ctx context.Context, qsoIDs []int64, jobID int64, sentAt time.Time) error {
	_, err := w.Pool.Exec(ctx, `
		UPDATE qsos
		SET lotw_sent_at    = $2,
		    lotw_sync_job_id = $3,
		    updated_at       = NOW()
		WHERE id = ANY($1::bigint[])
		  AND deleted_at IS NULL
	`, qsoIDs, sentAt, jobID)
	return err
}

// upsertSyncStatusRows creates or updates sync_status entries for each QSO
// so the Sync Dashboard can display LoTW sync state per-QSO.
func (w *LoTWSyncWorker) upsertSyncStatusRows(ctx context.Context, qsoIDs []int64, sentAt time.Time) error {
	_, err := w.Pool.Exec(ctx, `
		INSERT INTO sync_status (qso_id, service, status, last_synced_at, updated_at)
		SELECT unnest($1::bigint[]), 'lotw', 'uploaded', $2, NOW()
		ON CONFLICT (qso_id, service) DO UPDATE SET
		    status        = 'uploaded',
		    last_synced_at = EXCLUDED.last_synced_at,
		    error_message  = NULL,
		    last_error_code = NULL,
		    retry_count    = 0,
		    updated_at     = NOW()
	`, qsoIDs, sentAt)
	return err
}

// upsertSyncStatus upserts the lotw_sync_status row for a user.
func (w *LoTWSyncWorker) upsertSyncStatus(ctx context.Context, userID int64, qsoCount int, result, errMsg string) error {
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	_, err := w.Pool.Exec(ctx, `
		INSERT INTO lotw_sync_status (user_id, has_cert, last_sync_at, last_sync_qso_count, last_sync_result, last_sync_error, total_qsos_synced, updated_at)
		VALUES ($1, TRUE, NOW(), $2, $3, $4, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
		    has_cert            = TRUE,
		    last_sync_at        = NOW(),
		    last_sync_qso_count = EXCLUDED.last_sync_qso_count,
		    last_sync_result    = $3,
		    last_sync_error     = $4,
		    total_qsos_synced   = lotw_sync_status.total_qsos_synced + EXCLUDED.last_sync_qso_count,
		    updated_at          = NOW()
	`, userID, qsoCount, result, errPtr)
	return err
}

// fetchPendingLoTWQSOIDs returns IDs of QSOs not yet submitted to LoTW for a user.
func fetchPendingLoTWQSOIDs(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]int64, error) {
	const maxPending = 5000
	rows, err := pool.Query(ctx, `
		SELECT q.id
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND q.lotw_sent_at IS NULL
		  AND q.deleted_at IS NULL
		ORDER BY q.datetime_on ASC
		LIMIT $2
	`, userID, maxPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// EnqueueLoTWSyncForPendingQSOs creates a lotw_sync_jobs row for all currently
// unsent QSOs belonging to userID, then enqueues the LoTW upload worker.
func EnqueueLoTWSyncForPendingQSOs(ctx context.Context, pool *pgxpool.Pool, rc RiverInserter, userID int64, vaultUserID string) (int64, int, error) {
	if pool == nil {
		return 0, 0, fmt.Errorf("pool is nil")
	}
	qsoIDs, err := fetchPendingLoTWQSOIDs(ctx, pool, userID)
	if err != nil {
		return 0, 0, err
	}
	if len(qsoIDs) == 0 {
		return 0, 0, nil
	}

	qsoIDsJSON, err := json.Marshal(qsoIDs)
	if err != nil {
		return 0, 0, err
	}

	var jobID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO lotw_sync_jobs (user_id, status, qso_count, qso_ids)
		VALUES ($1, 'pending', $2, $3::jsonb)
		RETURNING id
	`, userID, len(qsoIDs), qsoIDsJSON).Scan(&jobID); err != nil {
		return 0, 0, err
	}

	if err := EnqueueLoTWSync(ctx, rc, jobID, userID, vaultUserID); err != nil {
		_, _ = pool.Exec(ctx, `
			UPDATE lotw_sync_jobs SET status = 'failed', error = 'failed to enqueue' WHERE id = $1
		`, jobID)
		return 0, 0, err
	}

	return jobID, len(qsoIDs), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Enqueue helper (used by the handler)
// ──────────────────────────────────────────────────────────────────────────────

// EnqueueLoTWSync enqueues a LoTW signing+upload job for a given DB job row.
// The vault password is loaded by the worker from user_service_credentials at
// execution time — it is not passed through job args.
func EnqueueLoTWSync(ctx context.Context, rc RiverInserter, jobID, userID int64, vaultUserID string) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, LoTWSyncArgs{
		JobID:       jobID,
		UserID:      userID,
		VaultUserID: vaultUserID,
	}, nil)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// Utility
// ──────────────────────────────────────────────────────────────────────────────

// certInfoErrMsg returns a human-readable error message for cert-info failures.
func certInfoErrMsg(err error) string {
	switch {
	case errors.Is(err, lotw.ErrNoCert):
		return "No LoTW certificate found. Upload your .p12 certificate in Settings."
	case errors.Is(err, lotw.ErrWrongPassword):
		return "Incorrect vault password. Check your LoTW vault password in Settings."
	default:
		return fmt.Sprintf("Could not reach vault to get cert info: %s", err)
	}
}

// signErrMsg returns a human-readable error message for vault sign failures.
func signErrMsg(err error) string {
	switch {
	case errors.Is(err, lotw.ErrNoCert):
		return "No LoTW certificate found. Upload your .p12 certificate in Settings."
	case errors.Is(err, lotw.ErrWrongPassword):
		return "Incorrect vault password. Check your LoTW vault password in Settings."
	case errors.Is(err, lotw.ErrInvalidCert):
		return "Certificate is invalid or expired. Re-upload your .p12 in Settings."
	default:
		return fmt.Sprintf("Vault signing failed: %s", err)
	}
}

// truncate limits a string to maxLen characters for safe logging.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// Compile-time interface check.
var _ river.Worker[LoTWSyncArgs] = (*LoTWSyncWorker)(nil)
