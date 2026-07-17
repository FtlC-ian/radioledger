// Package jobs contains River background job workers for RadioLedger.
// Workers process long-running tasks asynchronously: ADIF imports, external sync,
// award recalculation, and partition maintenance.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/qrz"
	"github.com/FtlC-ian/radioledger/api/internal/services/qsoenrich"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

// ADIFImportArgs holds the arguments for an ADIF import River job.
// The worker uses ImportJobID to load the job record and FilePath to open the file.
type ADIFImportArgs struct {
	// ImportJobID is the internal import_jobs.id (not the UUID).
	ImportJobID int64 `json:"import_job_id"`
	// FilePath is the absolute path to the temp ADIF file on disk.
	FilePath string `json:"file_path"`
	// LogbookID is the internal logbooks.id to insert QSOs into.
	LogbookID int64 `json:"logbook_id"`
	// UserID is the owner of the import. Used to bypass RLS safely.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind identifier for ADIF imports.
func (ADIFImportArgs) Kind() string { return "adif_import" }

// ADIFImportWorker is the River worker that processes ADIF import jobs.
// It streams the ADIF file, maps records to QSO columns, deduplicates, and
// bulk-inserts via pgx COPY protocol for high throughput.
type ADIFImportWorker struct {
	river.WorkerDefaults[ADIFImportArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// progressFlushInterval controls how often running counters are flushed to the DB.
// Too frequent causes excessive write load; too infrequent means stale progress.
const progressFlushInterval = 200

// Work executes the ADIF import job.
// It performs these steps:
//  1. Load the import_jobs record and validate state
//  2. First-pass scan to count total records (cheap, no memory allocation)
//  3. Second-pass parse + map + dedup + bulk COPY insert
//  4. Flush final counters and mark job complete or errored
func (w *ADIFImportWorker) Work(ctx context.Context, job *river.Job[ADIFImportArgs]) error {
	args := job.Args
	startedAt := time.Now()

	tracer := otel.Tracer("radioledger.jobs")
	ctx, span := tracer.Start(ctx, "river.adif_import.execute",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.Int64("river.import_job_id", args.ImportJobID),
			attribute.Int64("river.logbook_id", args.LogbookID),
			attribute.String("job.kind", args.Kind()),
		),
	)
	defer span.End()

	log := slog.With(
		slog.Int64("import_job_id", args.ImportJobID),
		slog.Int64("logbook_id", args.LogbookID),
		slog.String("file_path", args.FilePath),
	)
	log.Info("adif import worker started")

	defer func() {
		metrics.ObserveADIFImportDuration(time.Since(startedAt))
		if err := os.Remove(args.FilePath); err != nil && !os.IsNotExist(err) {
			log.Warn("failed to remove temp adif file", slog.String("error", err.Error()))
		}
	}()

	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// SET ROLE (not SET LOCAL) persists for the connection lifetime, which is
	// what we need since River workers operate outside explicit transactions.
	// SET LOCAL only lasts until the current transaction ends and is a no-op
	// outside a transaction block, causing subsequent queries to run without
	// the worker role and fail RLS checks.
	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("set worker role: %w", err)
	}

	queries := db.New(conn)

	importJob, err := queries.GetImportJobByID(ctx, args.ImportJobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("load import job: %w", err)
	}
	if importJob.Status != "pending" {
		log.Warn("import job not in pending state, skipping", slog.String("status", importJob.Status))
		span.SetStatus(codes.Ok, "skipped")
		return nil
	}

	countCtx, countSpan := tracer.Start(ctx, "adif.count_records")
	totalRecords, adifVersion, err := countADIFRecords(countCtx, args.FilePath)
	if err != nil {
		countSpan.RecordError(err)
		log.Warn("could not count records (non-fatal), proceeding", slog.String("error", err.Error()))
	}
	countSpan.SetAttributes(attribute.Int("adif.total_records.estimated", totalRecords))
	countSpan.End()

	total32 := int32(totalRecords)
	var adifVer *string
	if adifVersion != "" {
		adifVer = &adifVersion
	}
	if _, err := queries.StartImportJob(ctx, db.StartImportJobParams{
		ID:           args.ImportJobID,
		TotalRecords: &total32,
		AdifVersion:  adifVer,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("start import job: %w", err)
	}
	log.Info("adif import job started", slog.Int("total_records", totalRecords))

	counters, err := w.importRecords(ctx, conn, queries, args, totalRecords, log)

	if err == nil {
		enriched, enrichErr := qsoenrich.EnrichLogbook(ctx, conn, args.LogbookID)
		if enrichErr != nil {
			log.Warn("qso enrichment post-import failed", slog.String("error", enrichErr.Error()))
		} else if enriched > 0 {
			log.Info("qso enrichment post-import complete", slog.Int64("rows_enriched", enriched))
		}
	}

	finalStatus := "complete"
	if err != nil {
		finalStatus = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Error("adif import failed", slog.String("error", err.Error()))
		_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
			ImportJobID:  args.ImportJobID,
			Severity:     "error",
			ReasonCode:   strPtr("IMPORT_FAILED"),
			ReasonDetail: err.Error(),
		})
	} else {
		span.SetStatus(codes.Ok, "")
	}

	total32Final := int32(counters.total)
	if err2 := queries.CompleteImportJob(ctx, db.CompleteImportJobParams{
		ID:           args.ImportJobID,
		Status:       finalStatus,
		TotalRecords: &total32Final,
		Imported:     int32(counters.imported),
		Skipped:      int32(counters.skipped),
		Duplicate:    int32(counters.duplicate),
		Errors:       int32(counters.errors),
		Warnings:     int32(counters.warnings),
	}); err2 != nil {
		span.RecordError(err2)
		log.Error("failed to complete import job", slog.String("error", err2.Error()))
	}

	if notifyErr := createImportNotification(ctx, queries, importJob, counters, finalStatus, err); notifyErr != nil {
		log.Warn("failed to create import notification", slog.String("error", notifyErr.Error()))
	}

	// Warm callsign cache: background-lookup uncached callsigns from this import.
	// Best-effort only; failure here does not affect import success.
	if finalStatus == "complete" && counters.imported > 0 {
		go w.warmCallsignCache(args.LogbookID, args.UserID, log)
	}

	metrics.AddADIFImportRecords("imported", counters.imported)
	metrics.AddADIFImportRecords("duplicate", counters.duplicate)
	metrics.AddADIFImportRecords("error", counters.errors)
	metrics.IncQSOsLogged(counters.imported)

	span.SetAttributes(
		attribute.String("adif.final_status", finalStatus),
		attribute.Int("adif.records.imported", counters.imported),
		attribute.Int("adif.records.duplicate", counters.duplicate),
		attribute.Int("adif.records.error", counters.errors),
	)

	log.Info("adif import complete",
		slog.String("status", finalStatus),
		slog.Int("imported", counters.imported),
		slog.Int("duplicate", counters.duplicate),
		slog.Int("skipped", counters.skipped),
		slog.Int("errors", counters.errors),
	)

	return err
}

// importCounters tracks running statistics during import.
type importCounters struct {
	total     int
	imported  int
	duplicate int
	skipped   int
	errors    int
	warnings  int
}

// importRecords performs the main parse-map-dedup-insert loop.
// It processes records in batches using pgx COPY for high throughput,
// checks duplicates with a temp table approach, and records per-record errors.
func (w *ADIFImportWorker) importRecords(
	ctx context.Context,
	conn *pgxpool.Conn,
	queries *db.Queries,
	args ADIFImportArgs,
	totalEstimate int,
	log *slog.Logger,
) (importCounters, error) {
	const batchSize = 500

	tracer := otel.Tracer("radioledger.jobs")
	ctx, parseSpan := tracer.Start(ctx, "adif.parse_records")
	defer parseSpan.End()
	parseSpan.SetAttributes(attribute.Int("adif.total_records.estimated", totalEstimate))

	f, err := os.Open(args.FilePath)
	if err != nil {
		parseSpan.RecordError(err)
		return importCounters{}, fmt.Errorf("open adif file: %w", err)
	}
	defer func() { _ = f.Close() }()

	parser := adifpkg.NewParser(f)
	if _, err := parser.Header(ctx); err != nil {
		parseSpan.RecordError(err)
		return importCounters{}, fmt.Errorf("parse adif header: %w", err)
	}

	var counters importCounters
	var batch []qsoRow

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		imported, dupes, errs, err := insertQSOBatch(ctx, conn, args.LogbookID, args.ImportJobID, batch, queries)
		if err != nil {
			return fmt.Errorf("insert batch: %w", err)
		}

		counters.imported += imported
		counters.duplicate += dupes
		counters.errors += errs
		batch = batch[:0]

		// Flush progress to the database periodically.
		if counters.total%progressFlushInterval == 0 {
			_ = queries.UpdateImportJobProgress(ctx, db.UpdateImportJobProgressParams{
				ID:        args.ImportJobID,
				Imported:  int32(counters.imported),
				Skipped:   int32(counters.skipped),
				Duplicate: int32(counters.duplicate),
				Errors:    int32(counters.errors),
				Warnings:  int32(counters.warnings),
			})
		}
		return nil
	}

	recordNum := 0
	for {
		if err := ctx.Err(); err != nil {
			return counters, fmt.Errorf("context cancelled: %w", err)
		}

		rec, err := parser.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			counters.errors++
			_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
				ImportJobID:  args.ImportJobID,
				Severity:     "error",
				RecordNumber: int32Ptr(int32(recordNum)),
				ReasonCode:   strPtr("PARSE_ERROR"),
				ReasonDetail: err.Error(),
			})
			continue
		}

		recordNum++
		counters.total++

		// Map ADIF record to a qsoRow for insertion.
		row, warnings, mapErr := mapADIFRecord(rec, recordNum, args.ImportJobID)
		if mapErr != nil {
			// Fatal mapping error — callsign missing, date invalid, etc.
			counters.errors++
			counters.skipped++
			field := ""
			if mapErr.Field != "" {
				field = mapErr.Field
			}
			afp := strPtr(field)
			if field == "" {
				afp = nil
			}
			_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
				ImportJobID:  args.ImportJobID,
				Severity:     "error",
				RecordNumber: int32Ptr(int32(recordNum)),
				AdifField:    afp,
				ReasonCode:   strPtr(mapErr.Code),
				ReasonDetail: mapErr.Error(),
			})
			continue
		}

		// Record non-fatal warnings (e.g. frequency/band mismatch).
		for _, w := range warnings {
			counters.warnings++
			_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
				ImportJobID:  args.ImportJobID,
				Severity:     "warning",
				RecordNumber: int32Ptr(int32(recordNum)),
				AdifField:    strPtr(w.Field),
				ReasonCode:   strPtr(w.Code),
				ReasonDetail: w.Message,
			})
		}

		batch = append(batch, row)

		if len(batch) >= batchSize {
			if err := flushBatch(); err != nil {
				return counters, err
			}
		}
	}

	// Flush any remaining records.
	if err := flushBatch(); err != nil {
		parseSpan.RecordError(err)
		return counters, err
	}

	parseSpan.SetAttributes(
		attribute.Int("adif.records.total", counters.total),
		attribute.Int("adif.records.imported", counters.imported),
		attribute.Int("adif.records.duplicate", counters.duplicate),
		attribute.Int("adif.records.error", counters.errors),
	)

	return counters, nil
}

// qsoRow holds the mapped values for a single QSO ready for COPY insertion.
// Only the columns we extract from ADIF are represented; others default to NULL/zero.
type qsoRow struct {
	LogbookID    int64
	Callsign     string
	Name         *string
	Qth          *string
	Band         string
	Mode         string
	Submode      *string
	FrequencyHz  *int64
	FreqRxHz     *int64
	DatetimeOn   time.Time
	DatetimeOff  *time.Time
	TimeSource   string
	RstSent      *string
	RstRcvd      *string
	TxPower      *float64
	RxPwr        *float64
	MyAntenna    *string
	MyRig        *string
	Gridsquare   *string
	Dxcc         *int32
	Country      *string
	State        *string
	County       *string
	CqZone       *int16
	ItuZone      *int16
	Continent    *string
	MyGridsquare *string
	MyCity       *string
	MyState      *string
	MyCountry    *string
	MyDxcc       *int32
	Sfi          *int16
	AIndex       *int16
	KIndex       *int16
	Operator     *string
	StationCall  *string
	ContestID    *string
	Srx          *string
	Stx          *string
	SrxString    *string
	StxString    *string
	SatName      *string
	SatMode      *string
	PropMode     *string
	SotaRef      *string
	MySotaRef    *string
	PotaRefs     []string
	MyPotaRefs   []string
	WwffRef      *string
	MyWwffRef    *string
	Iota         *string
	Sig          *string
	SigInfo      *string
	QslSent      *string
	QslSentDate  *time.Time
	QslRcvd      *string
	QslRcvdDate  *time.Time
	QslVia       *string
	LotwSent     *string
	LotwSentDate *time.Time
	LotwRcvd     *string
	LotwRcvdDate *time.Time
	EqslSent     *string
	EqslSentDate *time.Time
	EqslRcvd     *string
	EqslRcvdDate *time.Time
	Comment      *string
	Notes        *string
	Extra        map[string]string // Unknown ADIF fields → extra JSONB
	Source       string
	SourceID     *string // nil for ADIF imports (dedup logic handles it)
}

// mapError is a fatal field-level mapping error that causes a record to be skipped.
type mapError struct {
	Code    string
	Field   string
	Message string
}

func (e *mapError) Error() string { return e.Message }

// importWarning is a non-fatal issue recorded as a warning in import_job_errors.
type importWarning struct {
	Field   string
	Code    string
	Message string
}

// knownMappedFields lists all ADIF fields we explicitly map to typed columns.
// Anything not in this set that isn't APP_* goes into extra JSONB.
var knownMappedFields = map[string]bool{
	"CALL": true, "BAND": true, "MODE": true, "SUBMODE": true,
	"FREQ": true, "FREQ_RX": true, "BAND_RX": true,
	"QSO_DATE": true, "TIME_ON": true, "QSO_DATE_OFF": true, "TIME_OFF": true,
	"RST_SENT": true, "RST_RCVD": true,
	"NAME": true, "QTH": true,
	"TX_PWR": true, "RX_PWR": true,
	"MY_ANTENNA": true, "MY_RIG": true, "ANTENNA": true,
	"GRIDSQUARE": true, "DXCC": true, "COUNTRY": true, "STATE": true,
	"COUNTY": true, "CQZ": true, "ITUZ": true, "CONT": true,
	"MY_GRIDSQUARE": true, "MY_CITY": true, "MY_STATE": true,
	"MY_COUNTRY": true, "MY_DXCC": true,
	"SFI": true, "A_INDEX": true, "K_INDEX": true, "PROP_MODE": true,
	"OPERATOR": true, "STATION_CALLSIGN": true,
	"CONTEST_ID": true, "SRX": true, "STX": true,
	"SRX_STRING": true, "STX_STRING": true,
	"SAT_NAME": true, "SAT_MODE": true,
	"SOTA_REF": true, "MY_SOTA_REF": true,
	"POTA_REF": true, "MY_POTA_REF": true,
	"WWFF_REF": true, "MY_WWFF_REF": true,
	"IOTA": true, "SIG": true, "SIG_INFO": true,
	"QSL_SENT": true, "QSL_SENT_VIA": true, "QSL_RCVD": true, "QSL_RCVD_VIA": true,
	"QSL_VIA": true, "QSLSDATE": true, "QSLRDATE": true,
	"LOTW_QSL_SENT": true, "LOTW_QSLSDATE": true,
	"LOTW_QSL_RCVD": true, "LOTW_QSLRDATE": true,
	"EQSL_QSL_SENT": true, "EQSL_QSLSDATE": true,
	"EQSL_QSL_RCVD": true, "EQSL_QSLRDATE": true,
	"COMMENT": true, "NOTES": true,
}

// mapADIFRecord maps an ADIF record to a qsoRow.
// Returns a fatal mapError if the record cannot be inserted (missing required fields, invalid date).
// Returns importWarnings for non-fatal issues (frequency/band mismatch, unrecognized mode, etc.).
func mapADIFRecord(rec *adifpkg.Record, recordNum int, importJobID int64) (qsoRow, []importWarning, *mapError) {
	var warnings []importWarning

	// --- Required: CALL ---
	callsign := strings.ToUpper(strings.TrimSpace(rec.Get("CALL")))
	if callsign == "" {
		return qsoRow{}, nil, &mapError{
			Code:    "MISSING_CALLSIGN",
			Field:   "CALL",
			Message: fmt.Sprintf("record %d: missing required CALL field", recordNum),
		}
	}

	// --- Required: QSO_DATE + TIME_ON → datetime_on ---
	qsoDate := strings.TrimSpace(rec.Get("QSO_DATE"))
	timeOn := strings.TrimSpace(rec.Get("TIME_ON"))
	if qsoDate == "" {
		return qsoRow{}, nil, &mapError{
			Code:    "MISSING_DATE",
			Field:   "QSO_DATE",
			Message: fmt.Sprintf("record %d: missing required QSO_DATE field", recordNum),
		}
	}
	datetimeOn, err := parseADIFDateTime(qsoDate, timeOn)
	if err != nil {
		return qsoRow{}, nil, &mapError{
			Code:    "INVALID_DATE",
			Field:   "QSO_DATE",
			Message: fmt.Sprintf("record %d: invalid date/time: %v", recordNum, err),
		}
	}

	// --- Required: BAND ---
	band := strings.ToLower(strings.TrimSpace(rec.Get("BAND")))
	if band == "" {
		// Try to infer from FREQ if BAND is missing.
		if freqStr := strings.TrimSpace(rec.Get("FREQ")); freqStr != "" {
			if inferredBand := inferBandFromFreq(freqStr); inferredBand != "" {
				band = inferredBand
				warnings = append(warnings, importWarning{
					Field:   "BAND",
					Code:    "BAND_INFERRED",
					Message: fmt.Sprintf("record %d: BAND missing, inferred %q from FREQ %s", recordNum, band, freqStr),
				})
			}
		}
		if band == "" {
			return qsoRow{}, nil, &mapError{
				Code:    "MISSING_BAND",
				Field:   "BAND",
				Message: fmt.Sprintf("record %d: missing required BAND field and could not infer from FREQ", recordNum),
			}
		}
	}

	// --- Required: MODE ---
	rawMode := strings.ToUpper(strings.TrimSpace(rec.Get("MODE")))
	if rawMode == "" {
		return qsoRow{}, nil, &mapError{
			Code:    "MISSING_MODE",
			Field:   "MODE",
			Message: fmt.Sprintf("record %d: missing required MODE field", recordNum),
		}
	}
	rawSubmode := strings.ToUpper(strings.TrimSpace(rec.Get("SUBMODE")))

	normalizedMode, ok := adifpkg.NormalizeModePair(rawMode, rawSubmode)
	if !ok {
		if rawSubmode != "" {
			return qsoRow{}, nil, &mapError{
				Code:    "INVALID_MODE_SUBMODE",
				Field:   "MODE",
				Message: fmt.Sprintf("record %d: invalid MODE/SUBMODE combination %q/%q", recordNum, rawMode, rawSubmode),
			}
		}
		return qsoRow{}, nil, &mapError{
			Code:    "INVALID_MODE",
			Field:   "MODE",
			Message: fmt.Sprintf("record %d: invalid MODE %q", recordNum, rawMode),
		}
	}

	mode := normalizedMode.Mode
	var submode *string
	if normalizedMode.Submode != "" {
		submode = strPtr(normalizedMode.Submode)
	}

	row := qsoRow{
		Callsign:   callsign,
		Band:       band,
		Mode:       mode,
		Submode:    submode,
		DatetimeOn: datetimeOn,
		TimeSource: "assumed_utc",
		Extra:      make(map[string]string),
		Source:     "adif_import",
	}

	// --- Optional: FREQ → frequency_hz ---
	if freqStr := strings.TrimSpace(rec.Get("FREQ")); freqStr != "" {
		if hz, err := parseMHzToHz(freqStr); err == nil {
			row.FrequencyHz = &hz
		} else {
			warnings = append(warnings, importWarning{
				Field:   "FREQ",
				Code:    "INVALID_FREQ",
				Message: fmt.Sprintf("record %d: invalid FREQ %q: %v", recordNum, freqStr, err),
			})
		}
	}

	// --- Optional: FREQ_RX → freq_rx_hz ---
	if freqStr := strings.TrimSpace(rec.Get("FREQ_RX")); freqStr != "" {
		if hz, err := parseMHzToHz(freqStr); err == nil {
			row.FreqRxHz = &hz
		}
	}

	// --- Optional: QSO_DATE_OFF + TIME_OFF → datetime_off ---
	if dateOff := strings.TrimSpace(rec.Get("QSO_DATE_OFF")); dateOff != "" {
		timeOff := strings.TrimSpace(rec.Get("TIME_OFF"))
		if dt, err := parseADIFDateTime(dateOff, timeOff); err == nil {
			row.DatetimeOff = &dt
		}
	}

	// --- Simple string fields ---
	row.Name = optStr(rec.Get("NAME"))
	row.Qth = optStr(rec.Get("QTH"))
	row.RstSent = optStr(rec.Get("RST_SENT"))
	row.RstRcvd = optStr(rec.Get("RST_RCVD"))
	row.Gridsquare = optStr(strings.ToUpper(rec.Get("GRIDSQUARE")))
	row.Country = optStr(rec.Get("COUNTRY"))
	row.State = optStr(rec.Get("STATE"))
	row.County = optStr(rec.Get("COUNTY"))
	row.Continent = optStr(strings.ToUpper(rec.Get("CONT")))
	row.MyGridsquare = optStr(strings.ToUpper(rec.Get("MY_GRIDSQUARE")))
	row.MyCity = optStr(rec.Get("MY_CITY"))
	row.MyState = optStr(rec.Get("MY_STATE"))
	row.MyCountry = optStr(rec.Get("MY_COUNTRY"))
	row.MyAntenna = optStr(rec.Get("MY_ANTENNA"))
	row.MyRig = optStr(rec.Get("MY_RIG"))
	row.Operator = optStr(strings.ToUpper(rec.Get("OPERATOR")))
	row.StationCall = optStr(strings.ToUpper(rec.Get("STATION_CALLSIGN")))
	row.ContestID = optStr(rec.Get("CONTEST_ID"))
	row.Srx = optStr(rec.Get("SRX"))
	row.Stx = optStr(rec.Get("STX"))
	row.SrxString = optStr(rec.Get("SRX_STRING"))
	row.StxString = optStr(rec.Get("STX_STRING"))
	row.SatName = optStr(rec.Get("SAT_NAME"))
	row.SatMode = optStr(rec.Get("SAT_MODE"))
	row.PropMode = optStr(rec.Get("PROP_MODE"))
	row.SotaRef = optStr(rec.Get("SOTA_REF"))
	row.MySotaRef = optStr(rec.Get("MY_SOTA_REF"))
	row.WwffRef = optStr(rec.Get("WWFF_REF"))
	row.MyWwffRef = optStr(rec.Get("MY_WWFF_REF"))
	row.Iota = optStr(rec.Get("IOTA"))
	row.Sig = optStr(rec.Get("SIG"))
	row.SigInfo = optStr(rec.Get("SIG_INFO"))
	row.QslSent = optStr(strings.ToUpper(rec.Get("QSL_SENT")))
	row.QslRcvd = optStr(strings.ToUpper(rec.Get("QSL_RCVD")))
	row.QslVia = optStr(strings.ToUpper(rec.Get("QSL_VIA")))
	row.LotwSent = optStr(strings.ToUpper(rec.Get("LOTW_QSL_SENT")))
	row.LotwRcvd = optStr(strings.ToUpper(rec.Get("LOTW_QSL_RCVD")))
	row.EqslSent = optStr(strings.ToUpper(rec.Get("EQSL_QSL_SENT")))
	row.EqslRcvd = optStr(strings.ToUpper(rec.Get("EQSL_QSL_RCVD")))
	row.Comment = optStr(rec.Get("COMMENT"))
	row.Notes = optStr(rec.Get("NOTES"))

	// --- Integer fields ---
	if v := strings.TrimSpace(rec.Get("DXCC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			n32 := int32(n)
			row.Dxcc = &n32
		}
	}
	if v := strings.TrimSpace(rec.Get("MY_DXCC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			n32 := int32(n)
			row.MyDxcc = &n32
		}
	}
	if v := strings.TrimSpace(rec.Get("CQZ")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 16); err == nil {
			n16 := int16(n)
			row.CqZone = &n16
		}
	}
	if v := strings.TrimSpace(rec.Get("ITUZ")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 16); err == nil {
			n16 := int16(n)
			row.ItuZone = &n16
		}
	}
	if v := strings.TrimSpace(rec.Get("SFI")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 16); err == nil {
			n16 := int16(n)
			row.Sfi = &n16
		}
	}
	if v := strings.TrimSpace(rec.Get("A_INDEX")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 16); err == nil {
			n16 := int16(n)
			row.AIndex = &n16
		}
	}
	if v := strings.TrimSpace(rec.Get("K_INDEX")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 16); err == nil {
			n16 := int16(n)
			row.KIndex = &n16
		}
	}

	// --- Numeric fields ---
	if v := strings.TrimSpace(rec.Get("TX_PWR")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			row.TxPower = &f
		}
	}
	if v := strings.TrimSpace(rec.Get("RX_PWR")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			row.RxPwr = &f
		}
	}

	// --- Date fields ---
	if v := strings.TrimSpace(rec.Get("QSLSDATE")); v != "" {
		if d, err := parseADIFDate(v); err == nil {
			row.QslSentDate = &d
		}
	}
	if v := strings.TrimSpace(rec.Get("QSLRDATE")); v != "" {
		if d, err := parseADIFDate(v); err == nil {
			row.QslRcvdDate = &d
		}
	}
	if v := strings.TrimSpace(rec.Get("LOTW_QSLSDATE")); v != "" {
		if d, err := parseADIFDate(v); err == nil {
			row.LotwSentDate = &d
		}
	}
	if v := strings.TrimSpace(rec.Get("LOTW_QSLRDATE")); v != "" {
		if d, err := parseADIFDate(v); err == nil {
			row.LotwRcvdDate = &d
		}
	}
	if v := strings.TrimSpace(rec.Get("EQSL_QSLSDATE")); v != "" {
		if d, err := parseADIFDate(v); err == nil {
			row.EqslSentDate = &d
		}
	}
	if v := strings.TrimSpace(rec.Get("EQSL_QSLRDATE")); v != "" {
		if d, err := parseADIFDate(v); err == nil {
			row.EqslRcvdDate = &d
		}
	}

	// --- POTA arrays: comma-separated refs → TEXT[] ---
	row.PotaRefs = parseReferenceList(rec.Get("POTA_REF"))
	row.MyPotaRefs = parseReferenceList(rec.Get("MY_POTA_REF"))

	// --- Extra JSONB: collect all non-mapped, non-empty fields ---
	for _, field := range rec.Fields {
		name := field.Name
		val := strings.TrimSpace(field.Value)
		if val == "" {
			continue
		}
		// APP_* fields always go to extra
		if strings.HasPrefix(name, "APP_") {
			row.Extra[name] = val
			continue
		}
		// Unknown fields (not in our mapped set) go to extra
		if !knownMappedFields[name] {
			row.Extra[name] = val
		}
	}

	return row, warnings, nil
}

// copyColumns lists the qsos columns in the COPY statement order.
// This must match the order of values provided in the CopyFromRows implementation.
var copyColumns = []string{
	"logbook_id", "callsign", "name", "qth",
	"band", "mode", "submode",
	"frequency_hz", "freq_rx_hz",
	"datetime_on", "datetime_off", "time_source",
	"rst_sent", "rst_rcvd",
	"tx_power", "rx_pwr",
	"my_antenna", "my_rig",
	"gridsquare", "dxcc", "country", "state", "county",
	"cq_zone", "itu_zone", "continent",
	"my_gridsquare", "my_city", "my_state", "my_country", "my_dxcc",
	"sfi", "a_index", "k_index",
	"operator", "station_callsign",
	"contest_id", "srx", "stx", "srx_string", "stx_string",
	"sat_name", "sat_mode", "prop_mode",
	"sota_ref", "my_sota_ref",
	"pota_refs", "my_pota_refs",
	"wwff_ref", "my_wwff_ref",
	"iota", "sig", "sig_info",
	"qsl_sent", "qsl_sent_date", "qsl_rcvd", "qsl_rcvd_date", "qsl_via",
	"lotw_qsl_sent", "lotw_qsl_sent_date", "lotw_qsl_rcvd", "lotw_qsl_rcvd_date",
	"eqsl_qsl_sent", "eqsl_qsl_sent_date", "eqsl_qsl_rcvd", "eqsl_qsl_rcvd_date",
	"comment", "notes",
	"extra", "source", "source_id",
}

// insertQSOBatch deduplicates and inserts a batch of QSOs using pgx COPY.
// Deduplication is performed by checking existing QSOs with the same composite key.
// Returns: (inserted count, duplicate count, error count, error).
func insertQSOBatch(
	ctx context.Context,
	conn *pgxpool.Conn,
	logbookID int64,
	importJobID int64,
	batch []qsoRow,
	queries *db.Queries,
) (inserted, dupes, errCount int, err error) {
	if len(batch) == 0 {
		return 0, 0, 0, nil
	}

	// --- Deduplication check ---
	// Use a VALUES-based query to check all records in the batch at once.
	// This is much faster than N individual lookups.
	deduped, dupCount := deduplicateBatch(ctx, conn, logbookID, batch)
	dupes = dupCount

	if len(deduped) == 0 {
		return 0, dupes, 0, nil
	}

	// --- COPY insert ---
	// pgx CopyFrom is the fastest way to bulk-insert rows into PostgreSQL.
	// It uses the binary COPY protocol and streams rows without per-row round-trips.
	rows := make([][]interface{}, 0, len(deduped))
	for _, r := range deduped {
		extraJSON, _ := json.Marshal(r.Extra)

		var datetimeOff interface{} = nil
		if r.DatetimeOff != nil {
			datetimeOff = pgtype.Timestamptz{Time: r.DatetimeOff.UTC(), Valid: true}
		}
		var qslSentDate, qslRcvdDate interface{} = nil, nil
		var lotwSentDate, lotwRcvdDate interface{} = nil, nil
		var eqslSentDate, eqslRcvdDate interface{} = nil, nil
		if r.QslSentDate != nil {
			qslSentDate = pgtype.Date{Time: *r.QslSentDate, Valid: true}
		}
		if r.QslRcvdDate != nil {
			qslRcvdDate = pgtype.Date{Time: *r.QslRcvdDate, Valid: true}
		}
		if r.LotwSentDate != nil {
			lotwSentDate = pgtype.Date{Time: *r.LotwSentDate, Valid: true}
		}
		if r.LotwRcvdDate != nil {
			lotwRcvdDate = pgtype.Date{Time: *r.LotwRcvdDate, Valid: true}
		}
		if r.EqslSentDate != nil {
			eqslSentDate = pgtype.Date{Time: *r.EqslSentDate, Valid: true}
		}
		if r.EqslRcvdDate != nil {
			eqslRcvdDate = pgtype.Date{Time: *r.EqslRcvdDate, Valid: true}
		}

		// Encode POTA arrays as PostgreSQL text[] (pgx handles []string natively)
		var potaRefs, myPotaRefs interface{} = nil, nil
		if len(r.PotaRefs) > 0 {
			potaRefs = r.PotaRefs
		}
		if len(r.MyPotaRefs) > 0 {
			myPotaRefs = r.MyPotaRefs
		}

		rows = append(rows, []interface{}{
			logbookID, r.Callsign, r.Name, r.Qth,
			r.Band, r.Mode, r.Submode,
			r.FrequencyHz, r.FreqRxHz,
			pgtype.Timestamptz{Time: r.DatetimeOn.UTC(), Valid: true},
			datetimeOff, r.TimeSource,
			r.RstSent, r.RstRcvd,
			r.TxPower, r.RxPwr,
			r.MyAntenna, r.MyRig,
			r.Gridsquare, r.Dxcc, r.Country, r.State, r.County,
			r.CqZone, r.ItuZone, r.Continent,
			r.MyGridsquare, r.MyCity, r.MyState, r.MyCountry, r.MyDxcc,
			r.Sfi, r.AIndex, r.KIndex,
			r.Operator, r.StationCall,
			r.ContestID, r.Srx, r.Stx, r.SrxString, r.StxString,
			r.SatName, r.SatMode, r.PropMode,
			r.SotaRef, r.MySotaRef,
			potaRefs, myPotaRefs,
			r.WwffRef, r.MyWwffRef,
			r.Iota, r.Sig, r.SigInfo,
			r.QslSent, qslSentDate, r.QslRcvd, qslRcvdDate, r.QslVia,
			r.LotwSent, lotwSentDate, r.LotwRcvd, lotwRcvdDate,
			r.EqslSent, eqslSentDate, r.EqslRcvd, eqslRcvdDate,
			r.Comment, r.Notes,
			extraJSON, r.Source, r.SourceID,
		})
	}

	n, err := conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{"qsos"},
		copyColumns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		// If COPY fails due to FK constraint (bad band/mode), fall back to row-by-row
		// to identify which records are invalid and record them as errors.
		var fkErrors int
		for _, r := range deduped {
			if insertErr := insertSingleQSO(ctx, conn, logbookID, r); insertErr != nil {
				errCount++
				fkErrors++
				_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
					ImportJobID:  importJobID,
					Severity:     "error",
					ReasonCode:   strPtr("INSERT_FAILED"),
					ReasonDetail: insertErr.Error(),
					RawFragment:  strPtr(fmt.Sprintf("callsign=%s band=%s mode=%s", r.Callsign, r.Band, r.Mode)),
				})
			} else {
				inserted++
			}
		}
		return inserted, dupes, errCount, nil
	}

	return int(n), dupes, 0, nil
}

// insertSingleQSO is the fallback single-row insert used when COPY fails for a batch.
// It inserts one row at a time so we can capture per-record errors.
// Includes ALL columns matching the COPY path so no fields are dropped on fallback.
func insertSingleQSO(ctx context.Context, conn *pgxpool.Conn, logbookID int64, r qsoRow) error {
	extraJSON, _ := json.Marshal(r.Extra)

	var datetimeOff interface{} = nil
	if r.DatetimeOff != nil {
		datetimeOff = pgtype.Timestamptz{Time: r.DatetimeOff.UTC(), Valid: true}
	}
	var qslSentDate, qslRcvdDate interface{} = nil, nil
	var lotwSentDate, lotwRcvdDate interface{} = nil, nil
	var eqslSentDate, eqslRcvdDate interface{} = nil, nil
	if r.QslSentDate != nil {
		qslSentDate = pgtype.Date{Time: *r.QslSentDate, Valid: true}
	}
	if r.QslRcvdDate != nil {
		qslRcvdDate = pgtype.Date{Time: *r.QslRcvdDate, Valid: true}
	}
	if r.LotwSentDate != nil {
		lotwSentDate = pgtype.Date{Time: *r.LotwSentDate, Valid: true}
	}
	if r.LotwRcvdDate != nil {
		lotwRcvdDate = pgtype.Date{Time: *r.LotwRcvdDate, Valid: true}
	}
	if r.EqslSentDate != nil {
		eqslSentDate = pgtype.Date{Time: *r.EqslSentDate, Valid: true}
	}
	if r.EqslRcvdDate != nil {
		eqslRcvdDate = pgtype.Date{Time: *r.EqslRcvdDate, Valid: true}
	}

	var potaRefs, myPotaRefs interface{} = nil, nil
	if len(r.PotaRefs) > 0 {
		potaRefs = r.PotaRefs
	}
	if len(r.MyPotaRefs) > 0 {
		myPotaRefs = r.MyPotaRefs
	}

	_, err := conn.Exec(ctx, `
		INSERT INTO qsos (
			logbook_id, callsign, name, qth,
			band, mode, submode,
			frequency_hz, freq_rx_hz,
			datetime_on, datetime_off, time_source,
			rst_sent, rst_rcvd,
			tx_power, rx_pwr,
			my_antenna, my_rig,
			gridsquare, dxcc, country, state, county,
			cq_zone, itu_zone, continent,
			my_gridsquare, my_city, my_state, my_country, my_dxcc,
			sfi, a_index, k_index,
			operator, station_callsign,
			contest_id, srx, stx, srx_string, stx_string,
			sat_name, sat_mode, prop_mode,
			sota_ref, my_sota_ref,
			pota_refs, my_pota_refs,
			wwff_ref, my_wwff_ref,
			iota, sig, sig_info,
			qsl_sent, qsl_sent_date, qsl_rcvd, qsl_rcvd_date, qsl_via,
			lotw_qsl_sent, lotw_qsl_sent_date, lotw_qsl_rcvd, lotw_qsl_rcvd_date,
			eqsl_qsl_sent, eqsl_qsl_sent_date, eqsl_qsl_rcvd, eqsl_qsl_rcvd_date,
			comment, notes,
			extra, source, source_id
		) VALUES (
			$1,$2,$3,$4,
			$5,$6,$7,
			$8,$9,
			$10,$11,$12,
			$13,$14,
			$15,$16,
			$17,$18,
			$19,$20,$21,$22,$23,
			$24,$25,$26,
			$27,$28,$29,$30,$31,
			$32,$33,$34,
			$35,$36,
			$37,$38,$39,$40,$41,
			$42,$43,$44,
			$45,$46,
			$47,$48,
			$49,$50,
			$51,$52,$53,
			$54,$55,$56,$57,$58,
			$59,$60,$61,$62,
			$63,$64,$65,$66,
			$67,$68,
			$69,$70,$71
		)
	`,
		logbookID, r.Callsign, r.Name, r.Qth,
		r.Band, r.Mode, r.Submode,
		r.FrequencyHz, r.FreqRxHz,
		pgtype.Timestamptz{Time: r.DatetimeOn.UTC(), Valid: true},
		datetimeOff, r.TimeSource,
		r.RstSent, r.RstRcvd,
		r.TxPower, r.RxPwr,
		r.MyAntenna, r.MyRig,
		r.Gridsquare, r.Dxcc, r.Country, r.State, r.County,
		r.CqZone, r.ItuZone, r.Continent,
		r.MyGridsquare, r.MyCity, r.MyState, r.MyCountry, r.MyDxcc,
		r.Sfi, r.AIndex, r.KIndex,
		r.Operator, r.StationCall,
		r.ContestID, r.Srx, r.Stx, r.SrxString, r.StxString,
		r.SatName, r.SatMode, r.PropMode,
		r.SotaRef, r.MySotaRef,
		potaRefs, myPotaRefs,
		r.WwffRef, r.MyWwffRef,
		r.Iota, r.Sig, r.SigInfo,
		r.QslSent, qslSentDate, r.QslRcvd, qslRcvdDate, r.QslVia,
		r.LotwSent, lotwSentDate, r.LotwRcvd, lotwRcvdDate,
		r.EqslSent, eqslSentDate, r.EqslRcvd, eqslRcvdDate,
		r.Comment, r.Notes,
		extraJSON, r.Source, r.SourceID,
	)
	return err
}

// deduplicateBatch filters out records that already exist in the logbook.
// Uses a VALUES-based batch query to check all records at once rather than N lookups.
// Returns (non-duplicate rows, duplicate count).
func deduplicateBatch(ctx context.Context, conn *pgxpool.Conn, logbookID int64, batch []qsoRow) ([]qsoRow, int) {
	if len(batch) == 0 {
		return nil, 0
	}

	type dedupKey struct {
		callsign string
		band     string
		mode     string
		timeKey  int64 // exact second precision of datetime_on
	}

	minTime := batch[0].DatetimeOn.UTC()
	maxTime := minTime
	for _, r := range batch {
		dt := r.DatetimeOn.UTC()
		if dt.Before(minTime) {
			minTime = dt
		}
		if dt.After(maxTime) {
			maxTime = dt
		}
	}

	rows, err := conn.Query(ctx, `
		SELECT DISTINCT upper(callsign), band, mode,
		       extract(epoch FROM datetime_on)::bigint AS ts
		FROM qsos
		WHERE logbook_id = $1
		  AND deleted_at IS NULL
		  AND datetime_on >= $2
		  AND datetime_on <= $3
	`, logbookID, minTime, maxTime)
	if err != nil {
		slog.Warn("dedup query failed, inserting all records", slog.String("error", err.Error()))
		return batch, 0
	}
	defer rows.Close()

	existing := make(map[dedupKey]bool)
	for rows.Next() {
		var callsign, band, mode string
		var ts int64
		if err := rows.Scan(&callsign, &band, &mode, &ts); err != nil {
			continue
		}
		existing[dedupKey{callsign: callsign, band: band, mode: mode, timeKey: ts}] = true
	}

	var deduped []qsoRow
	dupeCount := 0
	for _, r := range batch {
		k := dedupKey{
			callsign: strings.ToUpper(r.Callsign),
			band:     r.Band,
			mode:     r.Mode,
			timeKey:  r.DatetimeOn.UTC().Unix(),
		}
		if existing[k] {
			dupeCount++
			continue
		}
		deduped = append(deduped, r)
		existing[k] = true
	}

	return deduped, dupeCount
}

// countADIFRecords does a fast first pass to count records and extract the ADIF version.
// This enables accurate progress reporting without holding all records in memory.
func countADIFRecords(ctx context.Context, filePath string) (int, string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, "", fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	p := adifpkg.NewParser(f)
	hdr, err := p.Header(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("parse header: %w", err)
	}

	adifVersion := ""
	if hdr != nil {
		adifVersion = hdr.ADIFVersion()
	}

	count := 0
	for {
		_, err := p.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip parse errors in counting pass
		}
		count++
	}

	return count, adifVersion, nil
}

// --- Utility functions ---

// parseADIFDateTime parses a QSO_DATE (YYYYMMDD) and TIME_ON (HHMM or HHMMSS)
// into a UTC timestamp. ADIF dates are defined as UTC in the spec.
func parseADIFDateTime(date, timeStr string) (time.Time, error) {
	date = strings.TrimSpace(date)
	timeStr = strings.TrimSpace(timeStr)

	if len(date) != 8 {
		return time.Time{}, fmt.Errorf("invalid QSO_DATE format %q (expected YYYYMMDD)", date)
	}

	year, err := strconv.Atoi(date[0:4])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid year in %q", date)
	}
	month, err := strconv.Atoi(date[4:6])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month in %q", date)
	}
	day, err := strconv.Atoi(date[6:8])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day in %q", date)
	}

	hour, min, sec := 0, 0, 0
	if timeStr != "" {
		switch len(timeStr) {
		case 4: // HHMM
			hour, _ = strconv.Atoi(timeStr[0:2])
			min, _ = strconv.Atoi(timeStr[2:4])
		case 6: // HHMMSS
			hour, _ = strconv.Atoi(timeStr[0:2])
			min, _ = strconv.Atoi(timeStr[2:4])
			sec, _ = strconv.Atoi(timeStr[4:6])
		default:
			// Non-standard time format: try to parse hour:min at minimum.
			if len(timeStr) >= 4 {
				hour, _ = strconv.Atoi(timeStr[0:2])
				min, _ = strconv.Atoi(timeStr[2:4])
			}
		}
	}

	t := time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC)
	if t.IsZero() || year < 1900 || year > 2100 {
		return time.Time{}, fmt.Errorf("implausible date: %q", date)
	}

	return t, nil
}

// parseADIFDate parses a QSL date field (YYYYMMDD) to a Go time.Time (midnight UTC).
func parseADIFDate(s string) (time.Time, error) {
	return parseADIFDateTime(s, "")
}

// parseMHzToHz converts a frequency string in MHz to an integer Hz value.
// Returns an error if the string is not a valid frequency.
func parseMHzToHz(s string) (int64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid frequency %q: %w", s, err)
	}
	if f <= 0 || f >= 300000 {
		return 0, fmt.Errorf("frequency %f MHz out of valid range", f)
	}
	return int64(math.Round(f * 1e6)), nil
}

// inferBandFromFreq attempts to determine the band from a frequency string in MHz.
// Returns the band name or empty string if no match found.
func inferBandFromFreq(freqStr string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(freqStr), 64)
	if err != nil {
		return ""
	}
	for name, bd := range adifpkg.KnownBands {
		if f >= bd.LowerMHz && f <= bd.UpperMHz {
			return name
		}
	}
	return ""
}

func parseReferenceList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, ",")
	refs := make([]string, 0, len(parts))
	for _, part := range parts {
		ref := strings.ToUpper(strings.TrimSpace(part))
		if ref == "" {
			continue
		}
		refs = append(refs, ref)
	}

	if len(refs) == 0 {
		return nil
	}
	return refs
}

// optStr returns a pointer to a trimmed non-empty string, or nil.
func optStr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// strPtr returns a pointer to the given string value.
func strPtr(s string) *string { return &s }

// int32Ptr returns a pointer to the given int32 value.
func int32Ptr(n int32) *int32 { return &n }

func createImportNotification(
	ctx context.Context,
	queries *db.Queries,
	importJob db.ImportJob,
	counters importCounters,
	finalStatus string,
	importErr error,
) error {
	notificationType := "import_complete"
	status := "completed"
	message := fmt.Sprintf(
		"Imported %s QSOs (%s duplicates)",
		formatCountWithCommas(counters.imported),
		formatCountWithCommas(counters.duplicate),
	)

	if finalStatus != "complete" {
		notificationType = "import_failed"
		status = "failed"
		errMsg := "unknown error"
		if importErr != nil {
			errMsg = strings.TrimSpace(importErr.Error())
		}
		message = fmt.Sprintf("Import failed: %s", errMsg)
	}

	title := "ADIF Import"
	if strings.EqualFold(importJob.Source, "qrz") {
		title = "QRZ Import"
	}

	payload, err := json.Marshal(map[string]any{
		"title":           title,
		"message":         message,
		"status":          status,
		"import_job_uuid": importJob.Uuid.String(),
		"imported":        counters.imported,
		"duplicates":      counters.duplicate,
		"errors":          counters.errors,
		"route":           fmt.Sprintf("/import?job=%s", importJob.Uuid.String()),
	})
	if err != nil {
		return fmt.Errorf("marshal notification payload: %w", err)
	}

	_, err = queries.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  importJob.UserID,
		Type:    notificationType,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}

	return nil
}

func formatCountWithCommas(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	s := strconv.Itoa(n)
	result := make([]byte, 0, len(s)+len(s)/3)
	prefix := len(s) % 3
	if prefix == 0 {
		prefix = 3
	}
	result = append(result, s[:prefix]...)
	for i := prefix; i < len(s); i += 3 {
		result = append(result, ',')
		result = append(result, s[i:i+3]...)
	}
	return string(result)
}

// warmCallsignCache queries unique callsigns from the logbook that are not yet
// cached and initiates background QRZ lookups. Runs in a goroutine; errors are
// logged but not propagated.
//
// Limits to 500 callsigns per import to bound the warm-up duration.
func (w *ADIFImportWorker) warmCallsignCache(logbookID, userID int64, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if w.Keyring == nil {
		return
	}

	// Query up to 500 callsigns from this logbook that aren't already cached.
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		log.Warn("callsign warm: acquire conn failed", slog.String("error", err.Error()))
		return
	}

	rows, err := conn.Query(ctx, `
		SELECT DISTINCT UPPER(q.callsign)
		FROM qsos q
		WHERE q.logbook_id = $1
		  AND NOT EXISTS (
		      SELECT 1 FROM callsign_cache cc
		      WHERE cc.callsign = UPPER(q.callsign)
		        AND cc.expires_at > NOW()
		  )
		ORDER BY 1
		LIMIT 500
	`, logbookID)
	conn.Release()
	if err != nil {
		log.Warn("callsign warm: query failed", slog.String("error", err.Error()))
		return
	}

	var callsigns []string
	for rows.Next() {
		var cs string
		if err := rows.Scan(&cs); err == nil {
			callsigns = append(callsigns, cs)
		}
	}
	rows.Close()

	if len(callsigns) == 0 {
		return
	}

	log.Info("callsign cache warm: queuing lookups", slog.Int("count", len(callsigns)))

	// Build QRZ client for the user.
	credConn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	if _, err := credConn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		credConn.Release()
		return
	}

	queries := db.New(credConn)
	cred, credErr := queries.GetCredential(ctx, db.GetCredentialParams{
		UserID:  userID,
		Service: "qrz",
	})
	credConn.Release()

	if credErr != nil {
		log.Debug("callsign warm: no QRZ credentials, skipping",
			slog.String("error", credErr.Error()))
		return
	}

	plaintext, decErr := w.Keyring.Decrypt(userID, cred.KeyVersion, cred.Credentials)
	if decErr != nil {
		log.Warn("callsign warm: decrypt failed", slog.String("error", decErr.Error()))
		return
	}

	parts := strings.SplitN(string(plaintext), ":", 2)
	if len(parts) != 2 {
		return
	}
	qrzClient := qrz.New(parts[0], parts[1])

	// Process callsigns sequentially (rate limiting enforced inside client).
	cached, looked, skipped := 0, 0, 0
	cacheConn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer cacheConn.Release()
	cacheQueries := db.New(cacheConn)

	for _, callsign := range callsigns {
		if ctx.Err() != nil {
			break
		}

		info, lookupErr := qrzClient.LookupCallsign(ctx, callsign)
		if errors.Is(lookupErr, pgx.ErrNoRows) || lookupErr != nil {
			skipped++
			continue
		}

		data, marshalErr := json.Marshal(info)
		if marshalErr != nil {
			skipped++
			continue
		}

		_, _ = cacheQueries.UpsertCallsignCache(ctx, db.UpsertCallsignCacheParams{
			Callsign: callsign,
			Data:     data,
			Source:   "qrz",
			ExpiresAt: pgtype.Timestamptz{
				Time:  time.Now().Add(qrz.CacheTTL),
				Valid: true,
			},
		})
		looked++
		_ = cached
	}

	log.Info("callsign cache warm: complete",
		slog.Int("looked_up", looked),
		slog.Int("skipped", skipped),
	)
}
