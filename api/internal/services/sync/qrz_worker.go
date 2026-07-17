package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	qrzpkg "github.com/FtlC-ian/radioledger/api/internal/services/qrz"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

// ──────────────────────────────────────────────────────────────────────────────
// QRZ Logbook Upload Worker
// ──────────────────────────────────────────────────────────────────────────────

// QRZUploadArgs is the River job payload for a QRZ Logbook upload batch.
// One job is created per user when they have pending QSOs to upload to QRZ.
type QRZUploadArgs struct {
	// UserID is the owning user. The worker loads credentials and pending QSOs for this user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for QRZ Logbook uploads.
func (QRZUploadArgs) Kind() string { return "qrz_upload" }

// QRZUploadWorker uploads pending QSOs to the QRZ Logbook via the API.
//
// The QRZ Logbook API inserts/replaces one QSO at a time (unlike eQSL batch ADIF upload).
// Each successful INSERT/REPLACE returns a LOGID which is stored as remote_id in
// sync_status for later confirmation polling by QRZPollWorker.
type QRZUploadWorker struct {
	river.WorkerDefaults[QRZUploadArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the QRZ Logbook upload job for a specific user.
//
// Flow:
//  1. Load and decrypt QRZ Logbook API key for the user.
//  2. Fetch pending/dirty sync_status rows for this user + "qrz" service.
//  3. For each row, format as a single ADIF record and upload:
//     - dirty + remote_id => REPLACE
//     - otherwise => INSERT
//  4. On success: store the returned LOGID as remote_id; mark as 'uploaded'.
//  5. On failure: increment retry_count; apply exponential backoff.
func (w *QRZUploadWorker) Work(ctx context.Context, job *river.Job[QRZUploadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "qrz"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "qrz")
	if err != nil {
		return fmt.Errorf("qrz circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "qrz circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	// Load and decrypt QRZ Logbook credentials.
	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "qrz")
	if err != nil {
		msg := "QRZ credentials could not be decrypted. Re-save in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "qrz", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after credential error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to load qrz credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}
	if plaintext == nil {
		msg := "No QRZ credentials configured. Add your API key in Settings to sync."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "qrz", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows for missing credentials", slog.String("error", markErr.Error()))
		}
		log.WarnContext(ctx, "no qrz credentials configured; marked pending as permanent failure")
		return nil
	}

	creds, err := qrzpkg.DecodeLogbookCredentials(plaintext)
	if err != nil {
		msg := "Invalid QRZ credentials. Re-save your API key in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "qrz", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after decode error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to decode qrz logbook credentials; marked pending as permanent failure",
			slog.String("error", err.Error()))
		return nil
	}

	pending, err := fetchPendingQSOs(ctx, w.Pool, "qrz", userID, 200)
	if err != nil {
		return fmt.Errorf("fetch pending qrz qsos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	log.InfoContext(ctx, "qrz upload starting", slog.Int("count", len(pending)))
	client := qrzpkg.NewLogbookClient(creds.APIKey)

	uploaded := 0
	failed := 0
	for _, row := range pending {
		// Check rate limiter before each QSO (one INSERT per call).
		allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "qrz")
		if rateErr != nil {
			return fmt.Errorf("qrz rate limit check: %w", rateErr)
		}
		if !allowedRate {
			return river.JobSnooze(500 * time.Millisecond)
		}

		adif, fmtErr := qsoRowToSingleADIF(row)
		if fmtErr != nil {
			log.WarnContext(ctx, "qrz: failed to format QSO as ADIF",
				slog.Int64("qso_id", row.QSOID), slog.String("error", fmtErr.Error()))
			failed++
			continue
		}

		start := time.Now()
		var (
			result    *qrzpkg.InsertResult
			insertErr error
		)
		if row.SyncStatus == "dirty" && row.RemoteID != nil && strings.TrimSpace(*row.RemoteID) != "" {
			result, insertErr = client.ReplaceQSO(ctx, strings.TrimSpace(*row.RemoteID), adif)
		} else {
			result, insertErr = client.InsertQSO(ctx, adif)
		}
		metrics.ObserveRiverJobDuration("qrz", time.Since(start))

		if insertErr != nil {
			errText := insertErr.Error()

			// Duplicate = already in QRZ logbook = sync goal achieved.
			if strings.Contains(strings.ToLower(errText), "duplicate") {
				log.InfoContext(ctx, "qrz: QSO already exists (duplicate), marking as uploaded",
					slog.Int64("qso_id", row.QSOID))
				_ = markUploaded(ctx, w.Pool, row.SyncID, "")
				uploaded++
				continue
			}

			if isQRZAuthOrPermanentError(errText) {
				msg := "Invalid QRZ credentials. Re-save your API key in Settings."
				if _, markErr := markAllPendingFailed(ctx, w.Pool, "qrz", userID, msg); markErr != nil {
					log.ErrorContext(ctx, "failed to mark pending rows after auth failure", slog.String("error", markErr.Error()))
				}
				log.ErrorContext(ctx, "qrz authentication failure; marked pending as permanent failure",
					slog.Int64("qso_id", row.QSOID),
					slog.String("error", errText))
				return nil
			}

			if isTransientSyncError(errText) {
				tripped, cbErr := infra.RecordFailure(ctx, "qrz", errText)
				if cbErr != nil {
					log.WarnContext(ctx, "failed to record qrz circuit failure", slog.String("error", cbErr.Error()))
				}
				if tripped {
					log.ErrorContext(ctx, "qrz circuit breaker tripped after consecutive failures")
				}
				metrics.IncRiverJobFailure("qrz")
				return fmt.Errorf("qrz transient insert failure: %w", insertErr)
			}

			tripped, cbErr := infra.RecordFailure(ctx, "qrz", errText)
			if cbErr != nil {
				log.WarnContext(ctx, "failed to record qrz circuit failure", slog.String("error", cbErr.Error()))
			}
			if tripped {
				log.ErrorContext(ctx, "qrz circuit breaker tripped after consecutive failures")
			}

			_ = markSyncError(ctx, w.Pool, "qrz", row.SyncID, row.RetryCount, errText, "insert_failed")
			metrics.IncRiverJobFailure("qrz")
			log.WarnContext(ctx, "qrz insert failed",
				slog.Int64("qso_id", row.QSOID), slog.String("error", errText))
			failed++
			continue
		}

		if err := infra.RecordSuccess(ctx, "qrz"); err != nil {
			log.WarnContext(ctx, "failed to record qrz circuit success", slog.String("error", err.Error()))
		}

		// Mark uploaded and store the QRZ-assigned logid for confirmation polling.
		if err := markUploaded(ctx, w.Pool, row.SyncID, result.LogID); err != nil {
			log.WarnContext(ctx, "qrz: failed to mark sync as uploaded",
				slog.Int64("sync_id", row.SyncID), slog.String("error", err.Error()))
		}
		uploaded++
	}

	log.InfoContext(ctx, "qrz upload complete",
		slog.Int("uploaded", uploaded),
		slog.Int("failed", failed),
		slog.Int("total", len(pending)),
	)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// QRZ Logbook Poll Worker (confirmation polling)
// ──────────────────────────────────────────────────────────────────────────────

// QRZPollArgs is the River job payload for polling QRZ Logbook for QSO confirmations.
// Run periodically (e.g. every hour) per user with a QRZ integration configured.
type QRZPollArgs struct {
	// UserID is the owning user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for QRZ confirmation polls.
func (QRZPollArgs) Kind() string { return "qrz_poll" }

// QRZPollWorker polls the QRZ Logbook API for QSO confirmation status.
//
// Flow:
//  1. Load QRZ credentials.
//  2. Fetch QSOs in 'uploaded' state that have a remote_id (QRZ LOGID).
//  3. Call STATUS with those LOGIDs to check per-QSO confirmation.
//  4. For confirmed QSOs: update sync_status to 'confirmed'; update QSO.qsl_rcvd fields.
type QRZPollWorker struct {
	river.WorkerDefaults[QRZPollArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the QRZ confirmation poll for a user.
func (w *QRZPollWorker) Work(ctx context.Context, job *river.Job[QRZPollArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "qrz"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "qrz")
	if err != nil {
		return fmt.Errorf("qrz poll circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "qrz circuit breaker open (poll), requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "qrz")
	if err != nil || plaintext == nil {
		return err // nil plaintext = not configured, skip silently
	}

	creds, err := qrzpkg.DecodeLogbookCredentials(plaintext)
	if err != nil {
		return fmt.Errorf("decode qrz credentials (poll): %w", err)
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "qrz")
	if rateErr != nil {
		return fmt.Errorf("qrz poll rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(500 * time.Millisecond)
	}

	lookbackDays := 30
	if s := strings.TrimSpace(os.Getenv("QRZ_CONFIRMATION_LOOKBACK_DAYS")); s != "" {
		if n, convErr := strconv.Atoi(s); convErr == nil && n > 0 {
			lookbackDays = n
		}
	}
	since := time.Now().UTC().AddDate(0, 0, -lookbackDays)

	client := qrzpkg.NewLogbookClient(creds.APIKey)
	start := time.Now()
	fetch, err := client.FetchConfirmedQSOs(ctx, since, 250)
	metrics.ObserveRiverJobDuration("qrz", time.Since(start))
	if err != nil {
		_, _ = infra.RecordFailure(ctx, "qrz", err.Error())
		metrics.IncRiverJobFailure("qrz")
		return fmt.Errorf("qrz logbook fetch poll: %w", err)
	}
	if err := infra.RecordSuccess(ctx, "qrz"); err != nil {
		log.WarnContext(ctx, "failed to record qrz circuit success", slog.String("error", err.Error()))
	}

	records, err := parseQRZConfirmationADIF(fetch.ADIF)
	if err != nil {
		return fmt.Errorf("qrz poll parse adif: %w", err)
	}
	if len(records) == 0 {
		log.DebugContext(ctx, "qrz poll: no confirmation records returned", slog.Int("fetched_count", fetch.Count))
		return nil
	}

	matched := 0
	confirmed := 0
	for _, rec := range records {
		qsoID, err := findMatchingQSOForConfirmation(ctx, w.Pool, userID, rec.Callsign, rec.Band, rec.Mode, rec.DatetimeOn)
		if err != nil {
			log.WarnContext(ctx, "qrz poll: failed to match confirmation",
				slog.String("callsign", rec.Callsign), slog.String("error", err.Error()))
			continue
		}
		if qsoID == 0 {
			continue
		}
		matched++

		if err := applyQRZConfirmation(ctx, w.Pool, qsoID, rec); err != nil {
			log.WarnContext(ctx, "qrz poll: failed to apply confirmation",
				slog.Int64("qso_id", qsoID), slog.String("error", err.Error()))
			continue
		}
		if rec.HasAnyConfirmation() {
			if err := markSyncConfirmed(ctx, w.Pool, qsoID, "qrz"); err != nil {
				log.WarnContext(ctx, "qrz poll: failed to mark confirmed",
					slog.Int64("qso_id", qsoID), slog.String("error", err.Error()))
			} else {
				confirmed++
			}
		}
	}

	log.InfoContext(ctx, "qrz poll complete",
		slog.Int("fetched", len(records)),
		slog.Int("matched", matched),
		slog.Int("confirmed", confirmed),
	)
	return nil
}

type qrzConfirmationRecord struct {
	Callsign   string
	Band       string
	Mode       string
	DatetimeOn time.Time

	LotwQSLRcvd  string
	LotwQSLDate  *time.Time
	EQSLQSLRcvd  string
	EQSLQSLDate  *time.Time
	PaperQSLRcvd string
	PaperQSLDate *time.Time
	QRZQSLRcvd   string
}

func (r qrzConfirmationRecord) HasAnyConfirmation() bool {
	return strings.EqualFold(r.LotwQSLRcvd, "Y") ||
		strings.EqualFold(r.EQSLQSLRcvd, "Y") ||
		strings.EqualFold(r.PaperQSLRcvd, "Y") ||
		strings.EqualFold(r.QRZQSLRcvd, "Y")
}

func parseQRZConfirmationADIF(adifText string) ([]qrzConfirmationRecord, error) {
	if strings.TrimSpace(adifText) == "" {
		return nil, nil
	}
	parser := adifpkg.NewParser(bytes.NewReader([]byte(adifText)))
	if _, err := parser.Header(context.Background()); err != nil {
		return nil, err
	}

	var out []qrzConfirmationRecord
	for {
		rec, err := parser.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		callsign := strings.ToUpper(strings.TrimSpace(rec.Get("CALL")))
		band := strings.ToLower(strings.TrimSpace(rec.Get("BAND")))
		mode := strings.ToUpper(strings.TrimSpace(rec.Get("MODE")))
		if callsign == "" || band == "" || mode == "" {
			continue
		}

		dt := parseADIFDateTime(rec.Get("QSO_DATE"), rec.Get("TIME_ON"))
		if dt.IsZero() {
			continue
		}

		lotwDate := parseADIFDate(rec.Get("LOTW_QSLRDATE"), rec.Get("LOTW_QSL_RCVD_DATE"))
		eqslDate := parseADIFDate(rec.Get("EQSL_QSLRDATE"), rec.Get("EQSL_QSL_RCVD_DATE"))
		paperDate := parseADIFDate(rec.Get("QSLRDATE"), rec.Get("QSL_RCVD_DATE"))

		out = append(out, qrzConfirmationRecord{
			Callsign:     callsign,
			Band:         band,
			Mode:         mode,
			DatetimeOn:   dt,
			LotwQSLRcvd:  strings.ToUpper(strings.TrimSpace(rec.Get("LOTW_QSL_RCVD"))),
			LotwQSLDate:  lotwDate,
			EQSLQSLRcvd:  strings.ToUpper(strings.TrimSpace(rec.Get("EQSL_QSL_RCVD"))),
			EQSLQSLDate:  eqslDate,
			PaperQSLRcvd: strings.ToUpper(strings.TrimSpace(rec.Get("QSL_RCVD"))),
			PaperQSLDate: paperDate,
			QRZQSLRcvd:   strings.ToUpper(strings.TrimSpace(rec.Get("QRZ_QSL_RCVD"))),
		})
	}
	return out, nil
}

func parseADIFDateTime(qsoDate, timeOn string) time.Time {
	qsoDate = strings.TrimSpace(qsoDate)
	timeOn = strings.TrimSpace(timeOn)
	if len(qsoDate) != 8 {
		return time.Time{}
	}
	if len(timeOn) < 4 {
		timeOn = timeOn + strings.Repeat("0", 4-len(timeOn))
	}
	timePart := timeOn[:4]
	t, err := time.Parse("200601021504", qsoDate+timePart)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func parseADIFDate(values ...string) *time.Time {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) != 8 {
			continue
		}
		t, err := time.Parse("20060102", v)
		if err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func findMatchingQSOForConfirmation(ctx context.Context, pool *pgxpool.Pool, userID int64, callsign, band, mode string, dt time.Time) (int64, error) {
	row := pool.QueryRow(ctx, `
		SELECT q.id
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE upper(q.callsign) = upper($1)
		  AND lower(q.band) = lower($2)
		  AND upper(q.mode) = upper($3)
		  AND q.datetime_on BETWEEN ($4::timestamptz - INTERVAL '15 minutes')
		                        AND ($4::timestamptz + INTERVAL '15 minutes')
		  AND lb.user_id = $5
		  AND q.deleted_at IS NULL
		ORDER BY ABS(EXTRACT(EPOCH FROM (q.datetime_on - $4::timestamptz)))
		LIMIT 1
	`, callsign, band, mode, dt, userID)
	var qsoID int64
	if err := row.Scan(&qsoID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return qsoID, nil
}

func applyQRZConfirmation(ctx context.Context, pool *pgxpool.Pool, qsoID int64, rec qrzConfirmationRecord) error {
	_, err := pool.Exec(ctx, `
		UPDATE qsos SET
			lotw_qsl_rcvd = CASE WHEN $2 = 'Y' THEN 'Y' ELSE lotw_qsl_rcvd END,
			lotw_qsl_rcvd_date = CASE
				WHEN $2 = 'Y' AND lotw_qsl_rcvd_date IS NULL THEN COALESCE($3::date, CURRENT_DATE)
				ELSE lotw_qsl_rcvd_date
			END,
			eqsl_qsl_rcvd = CASE WHEN $4 = 'Y' THEN 'Y' ELSE eqsl_qsl_rcvd END,
			eqsl_qsl_rcvd_date = CASE
				WHEN $4 = 'Y' AND eqsl_qsl_rcvd_date IS NULL THEN COALESCE($5::date, CURRENT_DATE)
				ELSE eqsl_qsl_rcvd_date
			END,
			qsl_rcvd = CASE WHEN $6 = 'Y' THEN 'Y' ELSE qsl_rcvd END,
			qsl_rcvd_date = CASE
				WHEN $6 = 'Y' AND qsl_rcvd_date IS NULL THEN COALESCE($7::date, CURRENT_DATE)
				ELSE qsl_rcvd_date
			END,
			qrz_qsl_rcvd = CASE WHEN $8 = 'Y' THEN 'Y' ELSE qrz_qsl_rcvd END,
			qrz_qsl_rcvd_date = CASE
				WHEN $8 = 'Y' AND qrz_qsl_rcvd_date IS NULL THEN CURRENT_DATE
				WHEN ($2 = 'Y' OR $4 = 'Y' OR $6 = 'Y') AND qrz_qsl_rcvd_date IS NULL THEN CURRENT_DATE
				ELSE qrz_qsl_rcvd_date
			END,
			updated_at = NOW()
		WHERE id = $1
	`, qsoID,
		strings.ToUpper(strings.TrimSpace(rec.LotwQSLRcvd)), rec.LotwQSLDate,
		strings.ToUpper(strings.TrimSpace(rec.EQSLQSLRcvd)), rec.EQSLQSLDate,
		strings.ToUpper(strings.TrimSpace(rec.PaperQSLRcvd)), rec.PaperQSLDate,
		strings.ToUpper(strings.TrimSpace(rec.QRZQSLRcvd)),
	)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// QRZ ADIF formatter (single-record, no header)
// ──────────────────────────────────────────────────────────────────────────────

// qsoRowToSingleADIF formats a single pendingQSORow as a QRZ Logbook ADIF record.
// The QRZ API INSERT action expects one record per call with no ADIF header.
// Format: <field:length>value...<eor>
func qsoRowToSingleADIF(row pendingQSORow) (string, error) {
	var sb strings.Builder

	adifField := func(name, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&sb, "<%s:%d>%s", name, len(value), value)
	}

	adifField("CALL", row.Callsign)
	adifField("BAND", row.Band)

	rec := &adifpkg.Record{}
	rec.Set("MODE", row.Mode)
	if row.Submode != nil {
		rec.Set("SUBMODE", *row.Submode)
	}
	adifpkg.CanonicalizeRecordMode(rec)
	adifField("MODE", rec.Get("MODE"))
	adifField("SUBMODE", rec.Get("SUBMODE"))

	adifField("QSO_DATE", row.DatetimeOn.UTC().Format("20060102"))
	adifField("TIME_ON", row.DatetimeOn.UTC().Format("150405"))

	if row.RstSent != nil {
		adifField("RST_SENT", *row.RstSent)
	}
	if row.RstRcvd != nil {
		adifField("RST_RCVD", *row.RstRcvd)
	}
	if row.Gridsquare != nil {
		adifField("GRIDSQUARE", *row.Gridsquare)
	}
	if row.MyGridsquare != nil {
		adifField("MY_GRIDSQUARE", *row.MyGridsquare)
	}
	if row.StationCallsign != nil {
		adifField("STATION_CALLSIGN", *row.StationCallsign)
	}
	if row.FrequencyHz != nil {
		mhz := fmt.Sprintf("%.6f", float64(*row.FrequencyHz)/1_000_000.0)
		adifField("FREQ", mhz)
	}

	sb.WriteString("<eor>")
	return sb.String(), nil
}

func isQRZAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "api key is invalid") ||
		strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "invalid key") ||
		strings.Contains(s, "subscription required") ||
		strings.Contains(s, "lacks insert privileges") ||
		strings.Contains(s, "credentials missing") ||
		strings.Contains(s, "suspended") ||
		strings.Contains(s, "banned")
}

func isTransientSyncError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "timeout") ||
		strings.Contains(s, "deadline exceeded") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "temporary") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, " 429") ||
		strings.Contains(s, "status 429") ||
		strings.Contains(s, "status 500") ||
		strings.Contains(s, "status 502") ||
		strings.Contains(s, "status 503") ||
		strings.Contains(s, "status 504")
}

// ──────────────────────────────────────────────────────────────────────────────
// QRZ enqueue helpers
// ──────────────────────────────────────────────────────────────────────────────

// EnqueueQRZUpload enqueues a QRZ Logbook upload job for the given user.
// Called when the user manually triggers sync or after QSO creation.
func EnqueueQRZUpload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, QRZUploadArgs{UserID: userID}, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	})
	return err
}

// EnqueueQRZPoll enqueues a QRZ confirmation poll job for the given user.
func EnqueueQRZPoll(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, QRZPollArgs{UserID: userID}, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	})
	return err
}

// EncodeQRZLogbookCredentials is a convenience export for the credentials handler.
var EncodeQRZLogbookCredentials = func(apiKey string) ([]byte, error) {
	return qrzpkg.EncodeLogbookCredentials(apiKey)
}

// Verify QRZ worker types satisfy the River Worker interface at compile time.
var _ river.Worker[QRZUploadArgs] = (*QRZUploadWorker)(nil)
var _ river.Worker[QRZPollArgs] = (*QRZPollWorker)(nil)
