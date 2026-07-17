package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/qrz"
	"github.com/FtlC-ian/radioledger/api/internal/services/qsoenrich"
)

// QRZImportArgs holds the River payload for a one-time full QRZ logbook import.
type QRZImportArgs struct {
	ImportJobID int64 `json:"import_job_id"`
	LogbookID   int64 `json:"logbook_id"`
	UserID      int64 `json:"user_id"`
}

func (QRZImportArgs) Kind() string { return "qrz_import" }

// QRZImportWorker fetches a user's entire QRZ logbook as ADIF, then reuses
// the existing ADIF import pipeline to parse/map/dedup/insert records.
type QRZImportWorker struct {
	river.WorkerDefaults[QRZImportArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

func (w *QRZImportWorker) Work(ctx context.Context, job *river.Job[QRZImportArgs]) error {
	args := job.Args
	log := slog.With(
		slog.Int64("import_job_id", args.ImportJobID),
		slog.Int64("logbook_id", args.LogbookID),
		slog.Int64("user_id", args.UserID),
	)
	log.Info("qrz import worker started")

	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("set worker role: %w", err)
	}
	queries := db.New(conn)

	importJob, err := queries.GetImportJobByID(ctx, args.ImportJobID)
	if err != nil {
		return fmt.Errorf("load import job: %w", err)
	}
	if importJob.Status != "pending" {
		log.Warn("qrz import job not pending, skipping", slog.String("status", importJob.Status))
		return nil
	}

	apiKey, err := w.resolveAPIKey(ctx, queries, args.UserID)
	if err != nil {
		_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
			ImportJobID:  args.ImportJobID,
			Severity:     "error",
			ReasonCode:   strPtr("QRZ_CREDENTIALS_ERROR"),
			ReasonDetail: err.Error(),
		})
		_ = queries.CompleteImportJob(ctx, db.CompleteImportJobParams{
			ID:        args.ImportJobID,
			Status:    "error",
			Imported:  0,
			Skipped:   0,
			Duplicate: 0,
			Errors:    1,
			Warnings:  0,
		})
		return err
	}

	client := qrz.NewLogbookClient(apiKey)
	fetchResult, err := fetchQRZWithRetry(ctx, client)
	if err != nil {
		_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
			ImportJobID:  args.ImportJobID,
			Severity:     "error",
			ReasonCode:   strPtr("QRZ_FETCH_FAILED"),
			ReasonDetail: err.Error(),
		})
		_ = queries.CompleteImportJob(ctx, db.CompleteImportJobParams{
			ID:        args.ImportJobID,
			Status:    "error",
			Imported:  0,
			Skipped:   0,
			Duplicate: 0,
			Errors:    1,
			Warnings:  0,
		})
		return err
	}

	tmpFile, err := os.CreateTemp("", "radioledger-qrz-import-*.adi")
	if err != nil {
		return fmt.Errorf("create qrz temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := io.WriteString(tmpFile, fetchResult.ADIF); err != nil {
		return fmt.Errorf("write qrz adif temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync qrz adif temp file: %w", err)
	}

	totalRecords, adifVersion, err := countADIFRecords(ctx, tmpPath)
	if err != nil {
		log.Warn("qrz import: failed counting records", slog.String("error", err.Error()))
	}

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
		return fmt.Errorf("start qrz import job: %w", err)
	}

	helper := &ADIFImportWorker{Pool: w.Pool, Keyring: w.Keyring}
	counters, importErr := helper.importRecords(ctx, conn, queries, ADIFImportArgs{
		ImportJobID: args.ImportJobID,
		FilePath:    tmpPath,
		LogbookID:   args.LogbookID,
		UserID:      args.UserID,
	}, totalRecords, log)

	if importErr == nil {
		enriched, enrichErr := qsoenrich.EnrichLogbook(ctx, conn, args.LogbookID)
		if enrichErr != nil {
			log.Warn("qrz enrichment post-import failed", slog.String("error", enrichErr.Error()))
		} else if enriched > 0 {
			log.Info("qrz enrichment post-import complete", slog.Int64("rows_enriched", enriched))
		}
	}

	finalStatus := "complete"
	if importErr != nil {
		finalStatus = "error"
		_ = queries.CreateImportJobError(ctx, db.CreateImportJobErrorParams{
			ImportJobID:  args.ImportJobID,
			Severity:     "error",
			ReasonCode:   strPtr("IMPORT_FAILED"),
			ReasonDetail: importErr.Error(),
		})
	}

	total32Final := int32(counters.total)
	if err := queries.CompleteImportJob(ctx, db.CompleteImportJobParams{
		ID:           args.ImportJobID,
		Status:       finalStatus,
		TotalRecords: &total32Final,
		Imported:     int32(counters.imported),
		Skipped:      int32(counters.skipped),
		Duplicate:    int32(counters.duplicate),
		Errors:       int32(counters.errors),
		Warnings:     int32(counters.warnings),
	}); err != nil {
		return fmt.Errorf("complete qrz import job: %w", err)
	}

	if notifyErr := createImportNotification(ctx, queries, importJob, counters, finalStatus, importErr); notifyErr != nil {
		log.Warn("failed to create qrz import notification", slog.String("error", notifyErr.Error()))
	}

	if importErr != nil {
		return importErr
	}
	return nil
}

func (w *QRZImportWorker) resolveAPIKey(ctx context.Context, queries *db.Queries, userID int64) (string, error) {
	if w.Keyring == nil {
		return "", fmt.Errorf("keyring not configured")
	}

	cred, err := queries.GetCredential(ctx, db.GetCredentialParams{UserID: userID, Service: "qrz"})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no saved QRZ credentials configured")
	}
	if err != nil {
		return "", fmt.Errorf("load qrz credentials: %w", err)
	}

	plaintext, err := w.Keyring.Decrypt(userID, cred.KeyVersion, cred.Credentials)
	if err != nil {
		return "", fmt.Errorf("decrypt qrz credentials: %w", err)
	}

	creds, err := qrz.DecodeLogbookCredentials(plaintext)
	if err != nil {
		return "", fmt.Errorf("decode qrz credentials: %w", err)
	}
	return strings.TrimSpace(creds.APIKey), nil
}

func fetchQRZWithRetry(ctx context.Context, client *qrz.LogbookClient) (*qrz.FetchResult, error) {
	backoffs := []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second}
	var lastErr error

	for i, wait := range backoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		res, err := client.FetchAllQSOs(ctx)
		if err == nil {
			return res, nil
		}
		lastErr = err

		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "api key is invalid") || strings.Contains(msg, "auth") {
			return nil, err
		}

		if i == len(backoffs)-1 {
			break
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("qrz fetch failed")
	}
	return nil, lastErr
}
