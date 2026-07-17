// Package sync — eqsl_worker.go: River workers for eQSL.cc sync operations.
//
// eQSL.cc is a free electronic QSL card service for amateur radio operators.
// It uses HTTP form-POST API with username/password authentication (no OAuth).
//
// # Workers
//
//   - EQSLUploadWorker: uploads pending QSOs from sync_status to eQSL.cc
//   - EQSLDownloadWorker: downloads incoming eQSLs and matches them to local QSOs
//
// # Rate Limiting
//
// eQSL enforces aggressive rate limiting (~1 request per 2 seconds). The client
// handles throttling internally. Circuit breaker trips after 10 consecutive failures.
//
// # API Reference
//
// See: docs/api-research/eQSL.md
// Official: https://www.eqsl.cc/qslcard/AgDocumentation.cfm
//
// References:
//   - api/internal/services/eqsl/client.go
//   - docs/SYNC_SERVICES.md § eQSL
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"github.com/FtlC-ian/radioledger/api/internal/services/eqsl"
)

// ──────────────────────────────────────────────────────────────────────────────
// eQSL Upload Worker
// ──────────────────────────────────────────────────────────────────────────────

// EQSLUploadArgs is the River job payload for an eQSL upload batch.
// One job is created per user when they have pending QSOs to upload.
type EQSLUploadArgs struct {
	// UserID is the owning user. The worker loads credentials and pending QSOs for this user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for eQSL uploads.
func (EQSLUploadArgs) Kind() string { return "eqsl_upload" }

// EQSLUploadWorker is the River worker that uploads pending QSOs to eQSL.
type EQSLUploadWorker struct {
	river.WorkerDefaults[EQSLUploadArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the eQSL upload job for a specific user.
//
// Flow:
//  1. Load and decrypt eQSL credentials for the user.
//  2. Fetch pending sync_status rows for this user + eqsl service.
//  3. Format QSOs as ADIF.
//  4. Upload to eQSL, respecting the global rate limiter.
//  5. Mark each QSO as uploaded or errored in sync_status.
func (w *EQSLUploadWorker) Work(ctx context.Context, job *river.Job[EQSLUploadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "eqsl"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "eqsl")
	if err != nil {
		return fmt.Errorf("eqsl circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "eqsl circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	// Load and decrypt credentials.
	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "eqsl")
	if err != nil {
		msg := "eQSL credentials could not be decrypted. Re-save in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "eqsl", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after credential error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to load eqsl credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}
	if plaintext == nil {
		msg := "No eQSL credentials configured. Add credentials in Settings to sync."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "eqsl", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows for missing credentials", slog.String("error", markErr.Error()))
		}
		log.WarnContext(ctx, "no eqsl credentials configured; marked pending as permanent failure")
		return nil
	}

	creds, err := eqsl.DecodeCredentials(plaintext)
	if err != nil {
		msg := "Invalid eQSL credentials. Re-save your username/password in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "eqsl", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after decode error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to decode eqsl credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}

	// Fetch pending QSOs.
	pending, err := fetchPendingQSOs(ctx, w.Pool, "eqsl", userID, 500)
	if err != nil {
		return fmt.Errorf("fetch pending eqsl qsos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	log.InfoContext(ctx, "eqsl upload starting", slog.Int("count", len(pending)))

	client := eqsl.New(creds, log)

	// Upload in batches of 100 to stay within eQSL limits.
	const batchSize = 100
	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]

		allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "eqsl")
		if rateErr != nil {
			return fmt.Errorf("eqsl rate limit check: %w", rateErr)
		}
		if !allowedRate {
			return river.JobSnooze(1 * time.Second)
		}

		adifData, err := qsosToADIF(batch)
		if err != nil {
			log.ErrorContext(ctx, "failed to format ADIF for eqsl batch", slog.String("error", err.Error()))
			continue
		}

		start := time.Now()
		result, uploadErr := client.UploadADIF(ctx, adifData)
		metrics.ObserveRiverJobDuration("eqsl", time.Since(start))

		if uploadErr != nil {
			errText := uploadErr.Error()

			if isEQSLAuthOrPermanentError(errText) {
				msg := "Invalid eQSL credentials. Re-save your username/password in Settings."
				if _, markErr := markAllPendingFailed(ctx, w.Pool, "eqsl", userID, msg); markErr != nil {
					log.ErrorContext(ctx, "failed to mark pending rows after auth failure", slog.String("error", markErr.Error()))
				}
				log.ErrorContext(ctx, "eqsl authentication failure; marked pending as permanent failure", slog.String("error", errText))
				return nil
			}

			if isTransientSyncError(errText) {
				tripped, cbErr := infra.RecordFailure(ctx, "eqsl", errText)
				if cbErr != nil {
					log.WarnContext(ctx, "failed to record eqsl circuit failure", slog.String("error", cbErr.Error()))
				}
				if tripped {
					log.ErrorContext(ctx, "eqsl circuit breaker tripped after consecutive failures")
				}
				metrics.IncRiverJobFailure("eqsl")
				return fmt.Errorf("eqsl transient upload failure: %w", uploadErr)
			}

			tripped, cbErr := infra.RecordFailure(ctx, "eqsl", errText)
			if cbErr != nil {
				log.WarnContext(ctx, "failed to record eqsl circuit failure", slog.String("error", cbErr.Error()))
			}
			if tripped {
				log.ErrorContext(ctx, "eqsl circuit breaker tripped after consecutive failures")
			}
			for _, row := range batch {
				_ = markSyncError(ctx, w.Pool, "eqsl", row.SyncID, row.RetryCount, errText, "upload_failed")
			}
			metrics.IncRiverJobFailure("eqsl")
			log.ErrorContext(ctx, "eqsl upload batch failed", slog.String("error", errText))
			continue
		}

		if err := infra.RecordSuccess(ctx, "eqsl"); err != nil {
			log.WarnContext(ctx, "failed to record eqsl circuit success", slog.String("error", err.Error()))
		}
		log.InfoContext(ctx, "eqsl batch uploaded",
			slog.Int("accepted", result.Accepted),
			slog.Int("submitted", len(batch)),
		)

		// Mark all in batch as uploaded.
		for _, row := range batch {
			if err := markUploaded(ctx, w.Pool, row.SyncID, ""); err != nil {
				log.WarnContext(ctx, "failed to mark sync as uploaded", slog.Int64("sync_id", row.SyncID), slog.String("error", err.Error()))
			}
		}
	}

	return nil
}

func isEQSLAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "invalid username") ||
		strings.Contains(s, "invalid user") ||
		strings.Contains(s, "invalid password") ||
		strings.Contains(s, "credentials missing") ||
		strings.Contains(s, "suspended") ||
		strings.Contains(s, "banned")
}

// ──────────────────────────────────────────────────────────────────────────────
// eQSL Download Worker
// ──────────────────────────────────────────────────────────────────────────────

// EQSLDownloadArgs is the River job payload for downloading incoming eQSLs.
type EQSLDownloadArgs struct {
	UserID int64 `json:"user_id"`
	// SinceDate, if set, limits the download to eQSLs received after this time.
	// RFC3339 format. Leave empty to download all inbox items.
	SinceDate string `json:"since_date,omitempty"`
}

// Kind returns the unique River job kind for eQSL inbox downloads.
func (EQSLDownloadArgs) Kind() string { return "eqsl_download" }

// EQSLDownloadWorker is the River worker that downloads incoming eQSLs and matches them to local QSOs.
type EQSLDownloadWorker struct {
	river.WorkerDefaults[EQSLDownloadArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work downloads the user's eQSL inbox and marks matching QSOs as qsl_rcvd.
//
// Flow:
//  1. Load and decrypt eQSL credentials.
//  2. Download inbox ADIF from eQSL.
//  3. For each incoming eQSL, find the matching QSO in our database.
//  4. Update qsl_rcvd on the matched QSO.
func (w *EQSLDownloadWorker) Work(ctx context.Context, job *river.Job[EQSLDownloadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "eqsl"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "eqsl")
	if err != nil {
		return fmt.Errorf("eqsl circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "eqsl circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "eqsl")
	if err != nil {
		log.ErrorContext(ctx, "failed to load eqsl credentials", slog.String("error", err.Error()))
		return fmt.Errorf("eqsl credentials: %w", err)
	}
	if plaintext == nil {
		log.DebugContext(ctx, "no eqsl credentials configured, skipping")
		return nil
	}

	creds, err := eqsl.DecodeCredentials(plaintext)
	if err != nil {
		log.ErrorContext(ctx, "failed to decode eqsl credentials", slog.String("error", err.Error()))
		return fmt.Errorf("decode eqsl credentials: %w", err)
	}

	var sinceDate time.Time
	if job.Args.SinceDate != "" {
		sinceDate, err = time.Parse(time.RFC3339, job.Args.SinceDate)
		if err != nil {
			log.WarnContext(ctx, "invalid since_date, ignoring", slog.String("since_date", job.Args.SinceDate))
		}
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "eqsl")
	if rateErr != nil {
		return fmt.Errorf("eqsl rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(1 * time.Second)
	}

	client := eqsl.New(creds, log)

	start := time.Now()
	inbox, err := client.DownloadInbox(ctx, sinceDate)
	metrics.ObserveRiverJobDuration("eqsl", time.Since(start))

	if err != nil {
		tripped, cbErr := infra.RecordFailure(ctx, "eqsl", err.Error())
		if cbErr != nil {
			log.WarnContext(ctx, "failed to record eqsl circuit failure", slog.String("error", cbErr.Error()))
		}
		if tripped {
			log.ErrorContext(ctx, "eqsl circuit breaker tripped after consecutive failures")
		}
		metrics.IncRiverJobFailure("eqsl")
		return fmt.Errorf("eqsl inbox download: %w", err)
	}

	if err := infra.RecordSuccess(ctx, "eqsl"); err != nil {
		log.WarnContext(ctx, "failed to record eqsl circuit success", slog.String("error", err.Error()))
	}

	log.InfoContext(ctx, "eqsl inbox downloaded", slog.Int("records", len(inbox)))

	matched := 0
	for _, rec := range inbox {
		if rec.DatetimeOn.IsZero() {
			continue
		}

		// Find matching local QSO.
		row := w.Pool.QueryRow(ctx, `
			SELECT q.id, lb.user_id
			FROM qsos q
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE upper(q.callsign) = upper($1)
			  AND q.band = $2
			  AND q.mode = $3
			  AND q.datetime_on BETWEEN ($4::timestamptz - INTERVAL '15 minutes')
			                        AND ($4::timestamptz + INTERVAL '15 minutes')
			  AND lb.user_id = $5
			  AND q.deleted_at IS NULL
			LIMIT 1
		`, rec.TheirCallsign, rec.Band, rec.Mode, rec.DatetimeOn, userID)

		var qsoID int64
		var ownerUserID int64
		if err := row.Scan(&qsoID, &ownerUserID); err != nil {
			if err.Error() != "no rows in result set" {
				log.WarnContext(ctx, "error matching eqsl record", slog.String("error", err.Error()))
			}
			continue // no match — eQSL from a QSO we didn't log
		}

		rcvdDate := rec.DatetimeOn
		if rcvdDate.IsZero() {
			rcvdDate = time.Now().UTC()
		}

		// Update eqsl_qsl_rcvd on the QSO.
		_, _ = w.Pool.Exec(ctx, `
			UPDATE qsos SET
				eqsl_qsl_rcvd = 'Y',
				eqsl_qsl_rcvd_date = COALESCE(eqsl_qsl_rcvd_date, $2::date),
				updated_at = NOW()
			WHERE id = $1
			  AND (eqsl_qsl_rcvd IS NULL OR eqsl_qsl_rcvd != 'Y')
		`, qsoID, rcvdDate)

		// Update sync_status to confirmed.
		_ = markSyncConfirmed(ctx, w.Pool, qsoID, "eqsl")

		matched++
	}

	log.InfoContext(ctx, "eqsl inbox processing complete",
		slog.Int("total", len(inbox)),
		slog.Int("matched", matched),
	)

	return nil
}
