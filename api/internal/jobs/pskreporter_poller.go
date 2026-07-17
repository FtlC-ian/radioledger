package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/pskreporter"
)

// PSKReporterPollerArgs defines the River job payload for the PSK Reporter poller.
// No arguments are needed — the worker fetches all users with callsigns on every
// invocation.
type PSKReporterPollerArgs struct{}

func (PSKReporterPollerArgs) Kind() string { return "pskreporter_poller" }

// PSKReporterPollerWorker polls pskreporter.info for reception reports for every
// user who has a callsign configured. It runs every 15 minutes via a River
// periodic job and stores results in the psk_reception_reports table.
type PSKReporterPollerWorker struct {
	river.WorkerDefaults[PSKReporterPollerArgs]
	Pool            *pgxpool.Pool
	HTTPClient      *http.Client
	PSKClient       *pskreporter.Client
}

func (w *PSKReporterPollerWorker) Work(ctx context.Context, job *river.Job[PSKReporterPollerArgs]) error {
	log := slog.With(slog.String("job_kind", job.Args.Kind()), slog.Int64("job_id", job.ID))

	// Lazy-init the PSK Reporter client.
	pskClient := w.PSKClient
	if pskClient == nil {
		if w.HTTPClient != nil {
			pskClient = pskreporter.NewWithHTTPClient(w.HTTPClient)
		} else {
			pskClient = pskreporter.New()
		}
	}

	// Fetch all users who have a callsign configured.
	// We use a direct pool connection (bypassing RLS) via the worker role.
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("pskreporter_poller: acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("pskreporter_poller: set worker role: %w", err)
	}

	queries := db.New(conn)
	users, err := queries.ListUsersWithCallsign(ctx)
	if err != nil {
		return fmt.Errorf("pskreporter_poller: list users: %w", err)
	}

	if len(users) == 0 {
		log.Debug("pskreporter_poller: no users with callsigns configured")
		return nil
	}

	log.Info("pskreporter_poller: polling for users",
		slog.Int("user_count", len(users)),
	)

	var totalInserted int
	now := time.Now().UTC()

	for _, user := range users {
		if user.Callsign == nil || *user.Callsign == "" {
			continue
		}
		callsign := *user.Callsign

		// FetchReports respects the 5-minute per-callsign rate limit.
		reports, err := pskClient.FetchReports(ctx, callsign, "FT8")
		if err != nil {
			log.Warn("pskreporter_poller: fetch reports failed",
				slog.String("callsign", callsign),
				slog.Int64("user_id", user.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		if reports == nil {
			// Rate limit not yet expired — skip silently.
			continue
		}

		for _, report := range reports {
			var freqNumeric pgtype.Numeric
			if fkHz := report.FrequencyKHz(); fkHz != nil {
				if err := freqNumeric.Scan(*fkHz); err != nil {
					freqNumeric = pgtype.Numeric{} // invalid → NULL
				}
			}

			var snrVal *int16
			if report.SNR != nil {
				v := *report.SNR
				snrVal = &v
			}

			var gridVal *string
			if report.Grid != "" {
				g := report.Grid
				gridVal = &g
			}

			mode := report.Mode
			var modeVal *string
			if mode != "" {
				modeVal = &mode
			}

			if err := queries.UpsertPSKReceptionReport(ctx, db.UpsertPSKReceptionReportParams{
				UserID:           user.ID,
				SenderCallsign:   report.SenderCallsign,
				ReceiverCallsign: report.ReceiverCallsign,
				FrequencyKhz:     freqNumeric,
				Mode:             modeVal,
				Snr:              snrVal,
				Grid:             gridVal,
				SpottedAt:        pgtype.Timestamptz{Time: report.SpottedAt, Valid: true},
			}); err != nil {
				log.Warn("pskreporter_poller: upsert report failed",
					slog.Int64("user_id", user.ID),
					slog.String("sender", report.SenderCallsign),
					slog.String("receiver", report.ReceiverCallsign),
					slog.String("error", err.Error()),
				)
				continue
			}
			totalInserted++
		}
	}

	// Prune reports older than 30 days.
	cutoff := pgtype.Timestamptz{Time: now.Add(-30 * 24 * time.Hour), Valid: true}
	deleted, err := queries.DeletePSKReportsOlderThan(ctx, cutoff)
	if err != nil {
		log.Warn("pskreporter_poller: prune old reports failed", slog.String("error", err.Error()))
	}

	log.Info("pskreporter_poller complete",
		slog.Int("reports_inserted", totalInserted),
		slog.Int64("reports_pruned", deleted),
		slog.Int("users_polled", len(users)),
	)
	return nil
}
