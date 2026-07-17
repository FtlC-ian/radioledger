package jobs

// AutoSyncSchedulerWorker is a River periodic job that scans all active users
// and enqueues sync jobs (eQSL, ClubLog, QRZ, etc.) based on their subscription tier.
//
// # Architecture
//
// The scheduler runs every 15 minutes and performs a lightweight sweep:
//  1. Query all non-deleted users who have at least one active sync credential.
//  2. For each user, compare time.Now() against last_auto_sync_at + tier_interval.
//  3. If enough time has elapsed (or this is the user's first auto-sync), enqueue
//     upload and poll jobs for each service the user has credentials configured for.
//  4. Update last_auto_sync_at = NOW() for every user that was enqueued.
//
// # Tier Intervals
//
//   - free            → weekly  (7 × 24 hours)
//   - standard        → daily   (24 hours)
//   - premium / club  → hourly  (1 hour)
//
// # Services
//
// Only services with implemented upload/poll workers are enqueued:
//   - eqsl:    EQSLUploadWorker + EQSLConfirmationPullWorker
//   - clublog: ClubLogUploadWorker + ClubLogPollWorker
//   - qrz:     QRZUploadWorker + QRZPollWorker
//   - lotw:    LoTWSyncWorker + LoTWConfirmationPullWorker
//
// HamQTH is listed as a known service but still skipped here.
//
// # Scheduling
//
// Registered as a periodic River job in cmd/server/main.go (every 15 minutes).
// The worker itself is registered alongside other workers and receives its
// RiverClient field after the client is created (late-binding, same pattern as
// CascadeConfirmationWorker).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// syncTierInterval returns the auto-sync cadence for a given subscription tier.
//
//   - free              → weekly  (7 days)
//   - standard          → daily   (24 hours)
//   - premium / club    → hourly  (1 hour)
//
// Any unrecognised tier falls back to weekly (safest/least aggressive default).
func syncTierInterval(tier string) time.Duration {
	switch tier {
	case "standard":
		return 24 * time.Hour
	case "premium", "club":
		return 1 * time.Hour
	default: // "free" and any unrecognised value
		return 7 * 24 * time.Hour
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RiverJobInserter — thin interface so AutoSyncSchedulerWorker can be tested
// without depending on the concrete river.Client type.
// ──────────────────────────────────────────────────────────────────────────────

// RiverJobInserter is satisfied by *river.Client[pgx.Tx].
// Defined locally to avoid importing the pgx transaction type in this package.
type RiverJobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// ──────────────────────────────────────────────────────────────────────────────
// AutoSyncSchedulerArgs
// ──────────────────────────────────────────────────────────────────────────────

// AutoSyncSchedulerArgs holds the (empty) arguments for the periodic scheduler.
// The job operates across all users, so no per-user arguments are needed here.
type AutoSyncSchedulerArgs struct{}

// Kind returns the unique River job kind identifier for the auto-sync scheduler.
func (AutoSyncSchedulerArgs) Kind() string { return "auto_sync_scheduler" }

// ──────────────────────────────────────────────────────────────────────────────
// AutoSyncSchedulerWorker
// ──────────────────────────────────────────────────────────────────────────────

// AutoSyncSchedulerWorker is the River worker that runs the periodic auto-sync sweep.
//
// RiverClient is late-bound after the river.Client is created in main.go (same
// pattern as CascadeConfirmationWorker).
type AutoSyncSchedulerWorker struct {
	river.WorkerDefaults[AutoSyncSchedulerArgs]
	Pool        *pgxpool.Pool
	RiverClient RiverJobInserter
}

// userSyncCandidate holds per-user data fetched by the scheduler sweep query.
type userSyncCandidate struct {
	UserID           int64
	Callsign         string
	ZitadelID        string
	SubscriptionTier string
	LastAutoSyncAt   *time.Time
	LastLoTWPullAt   *time.Time
	LastEQSLPullAt   *time.Time
	Services         []string // active sync services with credentials configured
}

// Work executes the auto-sync scheduler sweep.
//
// For each eligible user the scheduler:
//  1. Computes the required sync interval from the user's subscription tier.
//  2. Skips the user if their last_auto_sync_at is too recent.
//  3. Enqueues upload+poll jobs for each configured service.
//  4. Updates last_auto_sync_at = NOW() so the next sweep skips them.
func (w *AutoSyncSchedulerWorker) Work(ctx context.Context, job *river.Job[AutoSyncSchedulerArgs]) error {
	log := slog.With(slog.String("job_kind", job.Args.Kind()), slog.Int64("job_id", job.ID))
	log.Info("auto-sync scheduler sweep started")

	if w.RiverClient == nil {
		return fmt.Errorf("auto_sync_scheduler: RiverClient not set")
	}

	candidates, err := w.fetchCandidates(ctx)
	if err != nil {
		return fmt.Errorf("auto_sync_scheduler: fetch candidates: %w", err)
	}

	now := time.Now().UTC()
	var enqueued, skipped int

	for _, u := range candidates {
		interval := syncTierInterval(u.SubscriptionTier)

		// Skip if last auto-sync was within the tier's required interval.
		if u.LastAutoSyncAt != nil && now.Sub(*u.LastAutoSyncAt) < interval {
			skipped++
			continue
		}

		if err := w.enqueueSyncJobs(ctx, log, u); err != nil {
			log.ErrorContext(ctx, "auto_sync_scheduler: failed to enqueue sync jobs",
				slog.Int64("user_id", u.UserID),
				slog.String("error", err.Error()),
			)
			continue
		}

		if err := w.markAutoSynced(ctx, u.UserID, now); err != nil {
			log.WarnContext(ctx, "auto_sync_scheduler: failed to update last_auto_sync_at",
				slog.Int64("user_id", u.UserID),
				slog.String("error", err.Error()),
			)
			// Non-fatal: the sync jobs were already enqueued. The next sweep will
			// re-check last_auto_sync_at; if the update fails persistently the user
			// gets a slightly more frequent sync — acceptable.
		}

		log.InfoContext(ctx, "auto_sync_scheduler: enqueued sync jobs",
			slog.Int64("user_id", u.UserID),
			slog.String("tier", u.SubscriptionTier),
			slog.Duration("interval", interval),
			slog.Any("services", u.Services),
		)
		enqueued++
	}

	log.Info("auto-sync scheduler sweep complete",
		slog.Int("candidates", len(candidates)),
		slog.Int("enqueued", enqueued),
		slog.Int("skipped_not_due", skipped),
	)
	return nil
}

// fetchCandidates queries all non-deleted users who have at least one active sync
// credential. It runs as the radioledger_worker role to bypass RLS.
//
// The query aggregates active services per user in a single round-trip.
func (w *AutoSyncSchedulerWorker) fetchCandidates(ctx context.Context) ([]userSyncCandidate, error) {
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return nil, fmt.Errorf("set worker role: %w", err)
	}

	rows, err := conn.Query(ctx, `
		SELECT
			u.id,
			COALESCE(u.callsign, ''),
			COALESCE(u.zitadel_id, ''),
			u.subscription_tier,
			u.last_auto_sync_at,
			lss.last_pull_at,
			ess.last_pull_at,
			array_agg(usc.service ORDER BY usc.service) AS services
		FROM users u
		JOIN user_service_credentials usc
			ON usc.user_id = u.id
			AND usc.is_active = TRUE
			AND usc.service IN ('eqsl', 'clublog', 'qrz', 'lotw')
		LEFT JOIN lotw_sync_status lss ON lss.user_id = u.id
		LEFT JOIN eqsl_sync_status ess ON ess.user_id = u.id
		WHERE u.deleted_at IS NULL
		GROUP BY u.id, u.callsign, u.zitadel_id, u.subscription_tier, u.last_auto_sync_at, lss.last_pull_at, ess.last_pull_at
	`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var candidates []userSyncCandidate
	for rows.Next() {
		var c userSyncCandidate
		var lastSync, lastLoTWPull, lastEQSLPull pgtype.Timestamptz
		var services []string

		if err := rows.Scan(&c.UserID, &c.Callsign, &c.ZitadelID, &c.SubscriptionTier, &lastSync, &lastLoTWPull, &lastEQSLPull, &services); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		if lastSync.Valid {
			t := lastSync.Time
			c.LastAutoSyncAt = &t
		}
		if lastLoTWPull.Valid {
			t := lastLoTWPull.Time
			c.LastLoTWPullAt = &t
		}
		if lastEQSLPull.Valid {
			t := lastEQSLPull.Time
			c.LastEQSLPullAt = &t
		}
		c.Services = services
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// enqueueSyncJobs enqueues upload and poll River jobs for each of the user's
// configured services. Jobs are enqueued with default insert options (unique by
// kind+args via River's built-in dedup when available).
func (w *AutoSyncSchedulerWorker) enqueueSyncJobs(ctx context.Context, log *slog.Logger, u userSyncCandidate) error {
	for _, svc := range u.Services {
		switch svc {
		case "eqsl":
			// Count pending eQSL QSOs to decide whether to delay the pull.
			eqslPendingCount, eqslPendingErr := syncsvc.CountPendingEQSLQSOs(ctx, w.Pool, u.UserID)
			if eqslPendingErr != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: count pending eqsl qsos failed",
					slog.Int64("user_id", u.UserID), slog.String("error", eqslPendingErr.Error()))
			}
			if eqslPendingCount > 0 {
				if err := syncsvc.EnqueueEQSLUpload(ctx, w.RiverClient, u.UserID); err != nil {
					log.WarnContext(ctx, "auto_sync_scheduler: enqueue eqsl upload failed",
						slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
				}
			}
			// Delay the pull if upload ran so eQSL has time to process the new cards.
			pullDelay := time.Duration(0)
			if eqslPendingCount > 0 {
				pullDelay = 30 * time.Second
			}
			if _, err := syncsvc.EnqueueEQSLConfirmationPull(ctx, w.RiverClient, u.UserID, u.LastEQSLPullAt, pullDelay); err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue eqsl confirmation pull failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}

		case "clublog":
			if err := syncsvc.EnqueueClubLogUpload(ctx, w.RiverClient, u.UserID); err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue clublog upload failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}
			if err := syncsvc.EnqueueClubLogPoll(ctx, w.RiverClient, u.UserID); err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue clublog poll failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}

		case "qrz":
			if err := syncsvc.EnqueueQRZUpload(ctx, w.RiverClient, u.UserID); err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue qrz upload failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}
			if err := syncsvc.EnqueueQRZPoll(ctx, w.RiverClient, u.UserID); err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue qrz poll failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}

		case "lotw":
			vaultUserID := lotwVaultUserID(u)
			_, qsoCount, err := syncsvc.EnqueueLoTWSyncForPendingQSOs(ctx, w.Pool, w.RiverClient, u.UserID, vaultUserID)
			if err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue lotw upload failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}
			if strings.TrimSpace(u.Callsign) == "" {
				log.WarnContext(ctx, "auto_sync_scheduler: skipping lotw confirmation pull because user has no callsign",
					slog.Int64("user_id", u.UserID))
				continue
			}
			delay := time.Duration(0)
			if qsoCount > 0 {
				delay = 30 * time.Second
			}
			if _, err := syncsvc.EnqueueLoTWConfirmationPull(ctx, w.RiverClient, u.UserID, u.Callsign, u.LastLoTWPullAt, delay); err != nil {
				log.WarnContext(ctx, "auto_sync_scheduler: enqueue lotw confirmation pull failed",
					slog.Int64("user_id", u.UserID), slog.String("error", err.Error()))
			}

		default:
			// HamQTH and other future services: skip silently until
			// scheduler wiring is implemented.
			log.DebugContext(ctx, "auto_sync_scheduler: no worker for service, skipping",
				slog.String("service", svc),
				slog.Int64("user_id", u.UserID),
			)
		}
	}
	return nil
}

func lotwVaultUserID(u userSyncCandidate) string {
	if strings.TrimSpace(u.ZitadelID) != "" {
		return "zitadel:" + strings.TrimSpace(u.ZitadelID)
	}
	return fmt.Sprintf("local:%d", u.UserID)
}

// markAutoSynced updates last_auto_sync_at for the given user.
// Runs directly against the pool (uses SET LOCAL ROLE for worker bypass).
func (w *AutoSyncSchedulerWorker) markAutoSynced(ctx context.Context, userID int64, syncedAt time.Time) error {
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("set worker role: %w", err)
	}

	_, err = conn.Exec(ctx, `
		UPDATE users SET last_auto_sync_at = $2, updated_at = NOW()
		WHERE id = $1
	`, userID, syncedAt)
	return err
}
