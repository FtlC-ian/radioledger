package sync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"github.com/FtlC-ian/radioledger/api/internal/services/pota"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

type POTAUploadArgs struct {
	UserID int64 `json:"user_id"`
}

func (POTAUploadArgs) Kind() string { return "pota_upload" }

type POTAUploadWorker struct {
	river.WorkerDefaults[POTAUploadArgs]
	Pool       *pgxpool.Pool
	Keyring    *crypto.Keyring
	APIBaseURL string
	AuthURL    string
}

type potaPendingQSORow struct {
	SyncID          int64
	QSOID           int64
	RetryCount      int16
	Callsign        string
	Band            string
	Mode            string
	DatetimeOn      time.Time
	RstSent         *string
	RstRcvd         *string
	Gridsquare      *string
	MyGridsquare    *string
	StationCallsign *string
	FrequencyHz     *int64
	Sig             *string
	SigInfo         *string
	PotaRefs        []string
	MyPotaRefs      []string
}

func (w *POTAUploadWorker) Work(ctx context.Context, job *river.Job[POTAUploadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "pota"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "pota")
	if err != nil {
		return fmt.Errorf("pota circuit check: %w", err)
	}
	if !allowed {
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "pota")
	if err != nil {
		msg := "POTA credentials could not be decrypted. Re-save in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "pota", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after credential error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to load pota credentials", slog.String("error", err.Error()))
		return nil
	}
	if plaintext == nil {
		msg := "No POTA credentials configured. Add your POTA JWT token in Settings to sync."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "pota", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows for missing credentials", slog.String("error", markErr.Error()))
		}
		return nil
	}

	creds, err := pota.DecodeCredentials(plaintext)
	if err != nil {
		msg := "Invalid POTA credentials. Re-save your POTA JWT token in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "pota", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after decode error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to decode pota credentials", slog.String("error", err.Error()))
		return nil
	}

	pending, err := fetchPendingPOTAQSOs(ctx, w.Pool, userID, 1000)
	if err != nil {
		return fmt.Errorf("fetch pending pota qsos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "pota")
	if rateErr != nil {
		return fmt.Errorf("pota rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(1 * time.Second)
	}

	adifData, err := potaQSOsToADIF(pending)
	if err != nil {
		return fmt.Errorf("format pota adif: %w", err)
	}

	client := pota.NewWithConfig(pota.ClientConfig{
		APIBaseURL: w.APIBaseURL,
		AuthURL:    w.AuthURL,
	})
	start := time.Now()
	_, uploadErr := client.UploadADIF(ctx, creds, adifData)
	metrics.ObserveRiverJobDuration("pota", time.Since(start))
	if uploadErr != nil {
		errText := uploadErr.Error()
		if isPOTAAuthOrPermanentError(errText) {
			msg := "POTA authentication failed. Re-save your POTA JWT token in Settings."
			if _, markErr := markAllPendingFailed(ctx, w.Pool, "pota", userID, msg); markErr != nil {
				log.ErrorContext(ctx, "failed to mark pending rows after auth failure", slog.String("error", markErr.Error()))
			}
			log.ErrorContext(ctx, "pota authentication failure", slog.String("error", errText))
			return nil
		}

		if isTransientSyncError(errText) {
			_, _ = infra.RecordFailure(ctx, "pota", errText)
			metrics.IncRiverJobFailure("pota")
			return fmt.Errorf("pota transient upload failure: %w", uploadErr)
		}

		_, _ = infra.RecordFailure(ctx, "pota", errText)
		for _, row := range pending {
			_ = markSyncError(ctx, w.Pool, "pota", row.SyncID, row.RetryCount, errText, "upload_failed")
		}
		metrics.IncRiverJobFailure("pota")
		return nil
	}

	if err := infra.RecordSuccess(ctx, "pota"); err != nil {
		log.WarnContext(ctx, "failed to record pota circuit success", slog.String("error", err.Error()))
	}
	for _, row := range pending {
		if err := markUploaded(ctx, w.Pool, row.SyncID, ""); err != nil {
			log.WarnContext(ctx, "failed to mark pota sync as uploaded", slog.Int64("sync_id", row.SyncID), slog.String("error", err.Error()))
		}
	}
	return nil
}

func fetchPendingPOTAQSOs(ctx context.Context, pool *pgxpool.Pool, userID int64, limit int) ([]potaPendingQSORow, error) {
	rows, err := pool.Query(ctx, `
SELECT
	ss.id,
	ss.qso_id,
	ss.retry_count,
	q.callsign,
	q.band,
	q.mode,
	q.datetime_on,
	q.rst_sent,
	q.rst_rcvd,
	q.gridsquare,
	q.my_gridsquare,
	q.station_callsign,
	q.frequency_hz,
	q.sig,
	q.sig_info,
	COALESCE(q.pota_refs, ARRAY[]::text[]),
	COALESCE(q.my_pota_refs, ARRAY[]::text[])
FROM sync_status ss
JOIN qsos q ON q.id = ss.qso_id
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE ss.service = 'pota'
  AND lb.user_id = $1
  AND ss.status IN ('pending', 'dirty', 'error')
  AND (ss.next_retry_at IS NULL OR ss.next_retry_at <= NOW())
  AND q.deleted_at IS NULL
  AND CARDINALITY(COALESCE(q.my_pota_refs, ARRAY[]::text[])) > 0
ORDER BY ss.created_at ASC
LIMIT $2
`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []potaPendingQSORow
	for rows.Next() {
		var row potaPendingQSORow
		var dt pgtype.Timestamptz
		if err := rows.Scan(
			&row.SyncID,
			&row.QSOID,
			&row.RetryCount,
			&row.Callsign,
			&row.Band,
			&row.Mode,
			&dt,
			&row.RstSent,
			&row.RstRcvd,
			&row.Gridsquare,
			&row.MyGridsquare,
			&row.StationCallsign,
			&row.FrequencyHz,
			&row.Sig,
			&row.SigInfo,
			&row.PotaRefs,
			&row.MyPotaRefs,
		); err != nil {
			return nil, err
		}
		if dt.Valid {
			row.DatetimeOn = dt.Time
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func potaQSOsToADIF(rows []potaPendingQSORow) (string, error) {
	var buf bytes.Buffer
	w := adifpkg.NewWriter(&buf)
	for _, row := range rows {
		rec := adifpkg.Record{}
		rec.Fields = append(rec.Fields,
			adifpkg.Field{Name: "CALL", Value: row.Callsign},
			adifpkg.Field{Name: "BAND", Value: row.Band},
			adifpkg.Field{Name: "MODE", Value: row.Mode},
			adifpkg.Field{Name: "QSO_DATE", Value: row.DatetimeOn.UTC().Format("20060102")},
			adifpkg.Field{Name: "TIME_ON", Value: row.DatetimeOn.UTC().Format("150405")},
			adifpkg.Field{Name: "MY_SIG", Value: "POTA"},
		)

		myRef := strings.Join(row.MyPotaRefs, ",")
		if myRef != "" {
			rec.Fields = append(rec.Fields,
				adifpkg.Field{Name: "MY_SIG_INFO", Value: myRef},
				adifpkg.Field{Name: "MY_POTA_REF", Value: myRef},
			)
		}

		if len(row.PotaRefs) > 0 {
			theirRef := strings.Join(row.PotaRefs, ",")
			sig := "POTA"
			if row.Sig != nil && strings.TrimSpace(*row.Sig) != "" {
				sig = strings.TrimSpace(*row.Sig)
			}
			rec.Fields = append(rec.Fields,
				adifpkg.Field{Name: "SIG", Value: sig},
				adifpkg.Field{Name: "SIG_INFO", Value: theirRef},
				adifpkg.Field{Name: "POTA_REF", Value: theirRef},
			)
		} else if row.Sig != nil && strings.TrimSpace(*row.Sig) != "" {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "SIG", Value: strings.TrimSpace(*row.Sig)})
		}

		if row.SigInfo != nil && strings.TrimSpace(*row.SigInfo) != "" {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "SIG_INFO", Value: strings.TrimSpace(*row.SigInfo)})
		}
		if row.RstSent != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "RST_SENT", Value: *row.RstSent})
		}
		if row.RstRcvd != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "RST_RCVD", Value: *row.RstRcvd})
		}
		if row.Gridsquare != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "GRIDSQUARE", Value: *row.Gridsquare})
		}
		if row.MyGridsquare != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "MY_GRIDSQUARE", Value: *row.MyGridsquare})
		}
		if row.StationCallsign != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "STATION_CALLSIGN", Value: *row.StationCallsign})
		}
		if row.FrequencyHz != nil {
			mhz := float64(*row.FrequencyHz) / 1_000_000.0
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "FREQ", Value: fmt.Sprintf("%.6f", mhz)})
		}
		adifpkg.CanonicalizeRecordMode(&rec)
		if err := w.WriteRecord(&rec); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func isPOTAAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "http 401") ||
		strings.Contains(s, "http 403") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "forbidden") ||
		strings.Contains(s, "invalid token") ||
		strings.Contains(s, "missing token") ||
		strings.Contains(s, "pota_auth_url")
}

func EnqueuePOTAUpload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, POTAUploadArgs{UserID: userID}, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	})
	return err
}

var _ river.Worker[POTAUploadArgs] = (*POTAUploadWorker)(nil)
