package jobs

// CallsignCacheWarmWorker is a River background job that pre-warms the callsign_cache
// by looking up unique callsigns from a recently-imported ADIF file.
//
// Triggered automatically at the end of an ADIF import. Skips callsigns already
// in the cache. For each cache miss, it queries QRZ (if credentials are configured
// for the user) and stores the result.
//
// Rate limiting: the QRZ client inside qrzClientForUserID enforces 1 req/sec.
// The worker processes callsigns sequentially (not concurrently) to respect this.
//
// If the user has no QRZ credentials stored, the job logs a debug message and
// exits cleanly without error. This is expected behaviour for users who have not
// set up QRZ integration.
//
// The job is idempotent: re-running it for the same import will simply skip any
// callsigns that were cached on the first run.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/qrz"
)

// CallsignCacheWarmArgs holds the arguments for a callsign cache warm job.
type CallsignCacheWarmArgs struct {
	// UserID is the user whose QRZ credentials should be used for lookups.
	UserID int64 `json:"user_id"`
	// Callsigns is the de-duplicated list of callsigns to warm.
	// These come from the just-completed ADIF import.
	Callsigns []string `json:"callsigns"`
	// ImportJobID is used for logging context only.
	ImportJobID int64 `json:"import_job_id"`
}

// Kind returns the unique River job kind identifier for cache warm jobs.
func (CallsignCacheWarmArgs) Kind() string { return "callsign_cache_warm" }

// CallsignCacheWarmWorker warms the callsign cache for callsigns from an ADIF import.
type CallsignCacheWarmWorker struct {
	river.WorkerDefaults[CallsignCacheWarmArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work processes the cache warm job.
func (w *CallsignCacheWarmWorker) Work(ctx context.Context, job *river.Job[CallsignCacheWarmArgs]) error {
	args := job.Args
	log := slog.With(
		slog.Int64("user_id", args.UserID),
		slog.Int64("import_job_id", args.ImportJobID),
		slog.Int("callsign_count", len(args.Callsigns)),
	)
	log.Info("callsign cache warm job started")

	if len(args.Callsigns) == 0 {
		return nil
	}

	if w.Keyring == nil {
		log.Debug("callsign cache warm: keyring not configured, skipping QRZ lookups")
		return nil
	}

	// Fetch the user's QRZ credentials (requires tenant context for RLS).
	qrzClient, err := w.buildQRZClient(ctx, args.UserID)
	if errors.Is(err, pgx.ErrNoRows) || isNoCredentialError(err) {
		log.Debug("callsign cache warm: no QRZ credentials for user, skipping")
		return nil
	}
	if err != nil {
		log.Warn("callsign cache warm: could not build QRZ client", slog.String("error", err.Error()))
		return nil // Non-fatal: cache warm is best-effort
	}

	cached := 0
	looked := 0
	skipped := 0

	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return nil // Non-fatal
	}
	defer conn.Release()

	queries := db.New(conn)

	for _, callsign := range args.Callsigns {
		callsign = strings.ToUpper(strings.TrimSpace(callsign))
		if callsign == "" {
			continue
		}

		// Check cache first.
		if _, cacheErr := queries.GetCallsignCache(ctx, callsign); cacheErr == nil {
			cached++
			continue
		}

		// Cache miss — look up via QRZ.
		info, lookupErr := qrzClient.LookupCallsign(ctx, callsign)
		if errors.Is(lookupErr, qrz.ErrNotFound) {
			skipped++
			continue
		}
		if errors.Is(lookupErr, qrz.ErrNotSubscribed) {
			log.Debug("callsign cache warm: QRZ subscription required, stopping")
			break
		}
		if lookupErr != nil {
			log.Warn("callsign cache warm: QRZ lookup failed",
				slog.String("callsign", callsign), slog.String("error", lookupErr.Error()))
			skipped++
			continue
		}

		data, marshalErr := json.Marshal(info)
		if marshalErr != nil {
			skipped++
			continue
		}

		_, _ = queries.UpsertCallsignCache(ctx, db.UpsertCallsignCacheParams{
			Callsign: callsign,
			Data:     data,
			Source:   "qrz",
			ExpiresAt: pgtype.Timestamptz{
				Time:  time.Now().Add(qrz.CacheTTL),
				Valid: true,
			},
		})
		looked++
	}

	log.Info("callsign cache warm job complete",
		slog.Int("already_cached", cached),
		slog.Int("looked_up", looked),
		slog.Int("skipped", skipped),
	)
	return nil
}

// buildQRZClient fetches and decrypts the user's QRZ credentials and returns a client.
func (w *CallsignCacheWarmWorker) buildQRZClient(ctx context.Context, userID int64) (*qrz.Client, error) {
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	// Use worker role (no per-user RLS, but worker role can read credentials for the given user).
	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return nil, err
	}

	queries := db.New(conn)
	cred, err := queries.GetCredential(ctx, db.GetCredentialParams{
		UserID:  userID,
		Service: "qrz",
	})
	if err != nil {
		return nil, err
	}

	plaintext, err := w.Keyring.Decrypt(userID, cred.KeyVersion, cred.Credentials)
	if err != nil {
		return nil, err
	}

	// The "qrz" service credential may be either:
	//   - Legacy XML API: "username:password" (plain text, colon-separated)
	//   - Logbook API key: JSON {"api_key": "..."}
	// The XML API is needed for callsign lookups (qrz.Client). If only a logbook
	// key is stored, we cannot do callsign lookups — skip gracefully.
	if qrz.IsLogbookCredential(plaintext) {
		return nil, errors.New("qrz: only logbook API key stored; callsign lookups require username:password")
	}

	parts := strings.SplitN(string(plaintext), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return nil, errors.New("qrz credential format invalid: expected 'username:password'")
	}

	return qrz.New(parts[0], parts[1]), nil
}

// isNoCredentialError returns true if the error indicates no stored credentials.
func isNoCredentialError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no QRZ credentials") ||
		errors.Is(err, pgx.ErrNoRows)
}
