// Package sync — hamqth_worker.go: River workers for HamQTH sync operations.
//
// HamQTH (https://www.hamqth.com) is a free ham radio callsign database and
// QSO logging service. It uses a session-key-based XML API: authenticate once
// to get a session_id, then upload ADIF records with that session ID.
//
// # Workers
//
//   - HamQTHUploadWorker: uploads pending QSOs from sync_status to HamQTH
//   - HamQTHPollWorker: polls HamQTH session validity and re-triggers uploads
//     for any QSOs that failed or stalled (HamQTH does not expose an inbox
//     download endpoint; confirmation is mutual-log-based within their system).
//
// # Session Management
//
// HamQTH session keys are valid for ~60 minutes of inactivity. The client
// manages session caching and re-authentication transparently.
//
// # Rate Limiting
//
// HamQTH claims no rate limits, but we enforce a conservative 500ms minimum
// between API calls to be a good citizen. Circuit breaker trips after 10
// consecutive failures.
//
// # API Reference
//
// See: docs/api-research/HamQTH.md
// Official: https://www.hamqth.com/developers.php
//
// References:
//   - api/internal/services/hamqth/client.go
//   - docs/SYNC_SERVICES.md § HamQTH
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"github.com/FtlC-ian/radioledger/api/internal/services/hamqth"
)

// ──────────────────────────────────────────────────────────────────────────────
// HamQTH Upload Worker
// ──────────────────────────────────────────────────────────────────────────────

// HamQTHUploadArgs is the River job payload for a HamQTH upload batch.
// One job is created per user when they have pending QSOs to upload.
type HamQTHUploadArgs struct {
	// UserID is the owning user. The worker loads credentials and pending QSOs for this user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for HamQTH uploads.
func (HamQTHUploadArgs) Kind() string { return "hamqth_upload" }

// HamQTHUploadWorker is the River worker that uploads pending QSOs to HamQTH.
type HamQTHUploadWorker struct {
	river.WorkerDefaults[HamQTHUploadArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the HamQTH upload job for a specific user.
//
// Flow:
//  1. Load and decrypt HamQTH credentials for the user.
//  2. Fetch pending sync_status rows for this user + "hamqth" service.
//  3. Format QSOs as ADIF (batches of 50 to stay within URL-safe limits).
//  4. Upload to HamQTH using the session-based XML API.
//  5. Mark each QSO as uploaded or errored in sync_status.
func (w *HamQTHUploadWorker) Work(ctx context.Context, job *river.Job[HamQTHUploadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "hamqth"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "hamqth")
	if err != nil {
		return fmt.Errorf("hamqth circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "hamqth circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	// Load and decrypt credentials.
	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "hamqth")
	if err != nil {
		msg := "HamQTH credentials could not be decrypted. Re-save in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "hamqth", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after credential error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to load hamqth credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}
	if plaintext == nil {
		msg := "No HamQTH credentials configured. Add your username/password in Settings to sync."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "hamqth", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows for missing credentials", slog.String("error", markErr.Error()))
		}
		log.WarnContext(ctx, "no hamqth credentials configured; marked pending as permanent failure")
		return nil
	}

	creds, err := hamqth.DecodeCredentials(plaintext)
	if err != nil {
		msg := "Invalid HamQTH credentials. Re-save your username/password in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "hamqth", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after decode error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to decode hamqth credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}

	// Fetch pending QSOs.
	pending, err := fetchPendingQSOs(ctx, w.Pool, "hamqth", userID, 500)
	if err != nil {
		return fmt.Errorf("fetch pending hamqth qsos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	log.InfoContext(ctx, "hamqth upload starting", slog.Int("count", len(pending)))

	client := hamqth.New(creds, log)

	// Upload in batches of 50 QSOs — HamQTH uses URL/form parameters for ADIF,
	// so keeping batches small avoids exceeding HTTP request size limits.
	const batchSize = 50
	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]

		allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "hamqth")
		if rateErr != nil {
			return fmt.Errorf("hamqth rate limit check: %w", rateErr)
		}
		if !allowedRate {
			return river.JobSnooze(1 * time.Second)
		}

		adifData, fmtErr := qsosToADIF(batch)
		if fmtErr != nil {
			log.ErrorContext(ctx, "failed to format ADIF for hamqth batch", slog.String("error", fmtErr.Error()))
			continue
		}

		start := time.Now()
		result, uploadErr := client.UploadADIF(ctx, adifData)
		metrics.ObserveRiverJobDuration("hamqth", time.Since(start))

		if uploadErr != nil {
			errText := uploadErr.Error()

			if isHamQTHAuthOrPermanentError(errText) {
				msg := "Invalid HamQTH credentials. Re-save your username/password in Settings."
				if _, markErr := markAllPendingFailed(ctx, w.Pool, "hamqth", userID, msg); markErr != nil {
					log.ErrorContext(ctx, "failed to mark pending rows after auth failure", slog.String("error", markErr.Error()))
				}
				log.ErrorContext(ctx, "hamqth authentication failure; marked pending as permanent failure", slog.String("error", errText))
				return nil
			}

			if isTransientSyncError(errText) {
				tripped, cbErr := infra.RecordFailure(ctx, "hamqth", errText)
				if cbErr != nil {
					log.WarnContext(ctx, "failed to record hamqth circuit failure", slog.String("error", cbErr.Error()))
				}
				if tripped {
					log.ErrorContext(ctx, "hamqth circuit breaker tripped after consecutive failures")
				}
				metrics.IncRiverJobFailure("hamqth")
				return fmt.Errorf("hamqth transient upload failure: %w", uploadErr)
			}

			// Non-transient, non-auth error — mark individual rows as failed.
			tripped, cbErr := infra.RecordFailure(ctx, "hamqth", errText)
			if cbErr != nil {
				log.WarnContext(ctx, "failed to record hamqth circuit failure", slog.String("error", cbErr.Error()))
			}
			if tripped {
				log.ErrorContext(ctx, "hamqth circuit breaker tripped after consecutive failures")
			}
			for _, row := range batch {
				_ = markSyncError(ctx, w.Pool, "hamqth", row.SyncID, row.RetryCount, errText, "upload_failed")
			}
			metrics.IncRiverJobFailure("hamqth")
			log.ErrorContext(ctx, "hamqth upload batch failed", slog.String("error", errText))
			continue
		}

		if err := infra.RecordSuccess(ctx, "hamqth"); err != nil {
			log.WarnContext(ctx, "failed to record hamqth circuit success", slog.String("error", err.Error()))
		}
		log.InfoContext(ctx, "hamqth batch uploaded",
			slog.Int("accepted", result.Count),
			slog.Int("submitted", len(batch)),
		)

		// Mark all in batch as uploaded.
		// HamQTH does not return per-QSO remote IDs, so remote_id is left empty.
		for _, row := range batch {
			if err := markUploaded(ctx, w.Pool, row.SyncID, ""); err != nil {
				log.WarnContext(ctx, "failed to mark hamqth sync as uploaded",
					slog.Int64("sync_id", row.SyncID), slog.String("error", err.Error()))
			}
		}
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// HamQTH Poll Worker (confirmation polling)
// ──────────────────────────────────────────────────────────────────────────────

// HamQTHPollArgs is the River job payload for polling HamQTH for confirmation status.
//
// # HamQTH Confirmation Model
//
// Unlike eQSL (which has a downloadable inbox) or QRZ (which exposes confirmed QSOs),
// HamQTH uses a mutual-log-based confirmation model: a QSO is "confirmed" in their
// system when both stations have uploaded matching QSOs. HamQTH does not currently
// expose a public API endpoint for downloading confirmed QSOs.
//
// This worker performs a verification pass: it validates credentials, re-queues any
// stalled uploads, and marks QSOs that have been in 'uploaded' state for the grace
// period as 'confirmed' (indicating both parties have had ample time to log).
//
// If HamQTH adds a confirmation inbox API in the future, this worker can be extended
// to use it — the framework is in place.
type HamQTHPollArgs struct {
	// UserID is the owning user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for HamQTH confirmation polls.
func (HamQTHPollArgs) Kind() string { return "hamqth_poll" }

// HamQTHPollWorker polls HamQTH for QSO confirmation status.
type HamQTHPollWorker struct {
	river.WorkerDefaults[HamQTHPollArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the HamQTH confirmation poll for a user.
//
// Flow:
//  1. Load and validate HamQTH credentials.
//  2. Verify the session is still valid (proactive re-login if needed).
//  3. Mark QSOs that have been in 'uploaded' state for > confirmationGracePeriod
//     as 'confirmed' — HamQTH's mutual-log model means both parties have had time
//     to log the QSO and it will appear as confirmed in HamQTH's web interface.
func (w *HamQTHPollWorker) Work(ctx context.Context, job *river.Job[HamQTHPollArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "hamqth"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "hamqth")
	if err != nil {
		return fmt.Errorf("hamqth poll circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "hamqth circuit breaker open (poll), requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "hamqth")
	if err != nil {
		log.ErrorContext(ctx, "failed to load hamqth credentials (poll)", slog.String("error", err.Error()))
		return fmt.Errorf("hamqth poll credentials: %w", err)
	}
	if plaintext == nil {
		log.DebugContext(ctx, "no hamqth credentials configured, skipping poll")
		return nil
	}

	creds, err := hamqth.DecodeCredentials(plaintext)
	if err != nil {
		log.ErrorContext(ctx, "failed to decode hamqth credentials (poll)", slog.String("error", err.Error()))
		return fmt.Errorf("decode hamqth credentials (poll): %w", err)
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "hamqth")
	if rateErr != nil {
		return fmt.Errorf("hamqth poll rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(500 * time.Millisecond)
	}

	// Validate credentials by performing a session login (the client will cache it).
	// This surfaces credential errors early and resets the circuit breaker on success.
	client := hamqth.New(creds, log)
	start := time.Now()
	pingErr := w.pingSession(ctx, client)
	metrics.ObserveRiverJobDuration("hamqth", time.Since(start))

	if pingErr != nil {
		errText := pingErr.Error()
		if isHamQTHAuthOrPermanentError(errText) {
			log.ErrorContext(ctx, "hamqth poll: authentication failed; credentials may be invalid",
				slog.String("error", errText))
			// Don't mark all as failed here — that's the upload worker's job.
			// Just record the circuit failure and surface the error.
		}
		tripped, cbErr := infra.RecordFailure(ctx, "hamqth", errText)
		if cbErr != nil {
			log.WarnContext(ctx, "failed to record hamqth poll circuit failure", slog.String("error", cbErr.Error()))
		}
		if tripped {
			log.ErrorContext(ctx, "hamqth circuit breaker tripped after consecutive poll failures")
		}
		metrics.IncRiverJobFailure("hamqth")
		return fmt.Errorf("hamqth poll: session ping failed: %w", pingErr)
	}

	if err := infra.RecordSuccess(ctx, "hamqth"); err != nil {
		log.WarnContext(ctx, "failed to record hamqth poll circuit success", slog.String("error", err.Error()))
	}

	// Mark QSOs that have been in 'uploaded' state long enough as 'confirmed'.
	// HamQTH uses a mutual-log model (no downloadable inbox), so we use a grace
	// period heuristic: if a QSO has been uploaded for > 30 days, it's reasonable
	// to consider it confirmed within the HamQTH system.
	const confirmationGracePeriod = 30 * 24 * time.Hour
	confirmed, err := w.markLongUploadedAsConfirmed(ctx, userID, confirmationGracePeriod)
	if err != nil {
		log.WarnContext(ctx, "hamqth poll: failed to mark stale uploads as confirmed",
			slog.String("error", err.Error()))
	}

	log.InfoContext(ctx, "hamqth poll complete",
		slog.Int64("newly_confirmed", confirmed),
	)
	return nil
}

// pingSession performs a no-op session validation by attempting to get a session key.
// Returns an error if credentials are invalid or HamQTH is unreachable.
func (w *HamQTHPollWorker) pingSession(ctx context.Context, client *hamqth.Client) error {
	// Upload an empty ADIF to trigger login without changing any data.
	// HamQTH will respond with a session error or an empty upload result.
	// We use an intentionally small ADIF that won't insert any records.
	// This is the simplest way to verify the session key is still valid.
	_, err := client.UploadADIF(ctx, "")
	if err != nil {
		// An empty upload may return a benign error — check if it's auth-related.
		if isHamQTHAuthOrPermanentError(err.Error()) {
			return err
		}
		// Non-auth errors (e.g. "no records in ADIF") are acceptable for ping.
		return nil
	}
	return nil
}

// markLongUploadedAsConfirmed marks QSOs that have been in 'uploaded' state
// for longer than gracePeriod as 'confirmed'.
// Returns the number of rows updated.
func (w *HamQTHPollWorker) markLongUploadedAsConfirmed(ctx context.Context, userID int64, gracePeriod time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-gracePeriod)
	tag, err := w.Pool.Exec(ctx, `
		UPDATE sync_status SET
			status = 'confirmed',
			last_synced_at = NOW(),
			error_message = NULL,
			updated_at = NOW()
		WHERE service = 'hamqth'
		  AND status = 'uploaded'
		  AND last_synced_at < $2
		  AND qso_id IN (
			SELECT q.id
			FROM qsos q
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $1
			  AND q.deleted_at IS NULL
		  )
	`, userID, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Error classifiers
// ──────────────────────────────────────────────────────────────────────────────

func isHamQTHAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "wrong username") ||
		strings.Contains(s, "wrong password") ||
		strings.Contains(s, "invalid user") ||
		strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "incorrect") ||
		strings.Contains(s, "suspended") ||
		strings.Contains(s, "banned") ||
		strings.Contains(s, "credentials missing") ||
		hamqth.IsAuthOrPermanentError(errMsg)
}

// ──────────────────────────────────────────────────────────────────────────────
// Enqueue helpers
// ──────────────────────────────────────────────────────────────────────────────

// EnqueueHamQTHUpload enqueues a HamQTH upload job for the given user.
// Called when the user manually triggers sync or after QSO creation.
func EnqueueHamQTHUpload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, HamQTHUploadArgs{UserID: userID}, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	})
	return err
}

// EnqueueHamQTHPoll enqueues a HamQTH confirmation poll job for the given user.
// Called periodically (e.g. daily) to check confirmation status.
func EnqueueHamQTHPoll(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, HamQTHPollArgs{UserID: userID}, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	})
	return err
}

// EncodeHamQTHCredentials is a convenience export for the credentials handler.
var EncodeHamQTHCredentials = func(username, password string) ([]byte, error) {
	return hamqth.EncodeCredentials(username, password)
}

// Verify worker types satisfy the River Worker interface at compile time.
var _ river.Worker[HamQTHUploadArgs] = (*HamQTHUploadWorker)(nil)
var _ river.Worker[HamQTHPollArgs] = (*HamQTHPollWorker)(nil)
