package jobs

// CertExpiryCheckWorker is a River background job that scans all station
// locations for approaching LoTW certificate expiry dates and creates
// in-app notifications at 60, 30, and 7 days before expiry.
//
// Architecture:
//   - The desktop client POSTs only the expiry DATE to the server (private
//     keys never leave the operator's machine).
//   - This job runs daily; it queries station_locations.lotw_cert_expiry
//     across all users and fires notifications at each threshold once.
//   - Duplicate suppression: before creating a notification, the job checks
//     whether one with the same (user, callsign, expires_at, threshold_days)
//     was already sent within the past 25 days.
//
// Scheduling: enqueued as a periodic River job in cmd/server/main.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// certExpiryThresholds is the ordered list of warning thresholds (days before expiry).
// Notifications are created once per threshold per cert.
var certExpiryThresholds = []int{60, 30, 7}

// lotwRenewalGuideURL is included in notification payloads so the user can act immediately.
const lotwRenewalGuideURL = "https://lotw.arrl.org/lotw-help/creating-a-certificate/"

// CertExpiryCheckArgs holds the (empty) arguments for the daily cert expiry check job.
// The job operates across all users, so no per-user arguments are needed.
type CertExpiryCheckArgs struct{}

// Kind returns the unique River job kind identifier for cert expiry checks.
func (CertExpiryCheckArgs) Kind() string { return "cert_expiry_check" }

// CertExpiryCheckWorker is the River worker that performs daily LoTW cert expiry scans.
type CertExpiryCheckWorker struct {
	river.WorkerDefaults[CertExpiryCheckArgs]
	Pool *pgxpool.Pool
}

// Work executes the cert expiry check.
// For each station location with a non-null lotw_cert_expiry:
//  1. Compute days until expiry.
//  2. For each threshold in [60, 30, 7], create a notification if the cert
//     expires within that many days and no duplicate notification exists.
func (w *CertExpiryCheckWorker) Work(ctx context.Context, job *river.Job[CertExpiryCheckArgs]) error {
	log := slog.With(slog.String("job_kind", job.Args.Kind()), slog.Int64("job_id", job.ID))
	log.Info("cert expiry check started")

	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("cert_expiry_check: acquire conn: %w", err)
	}
	defer conn.Release()

	// Run as the worker role to bypass RLS on station_locations and notifications.
	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("cert_expiry_check: set worker role: %w", err)
	}

	queries := db.New(conn)

	certs, err := queries.ListAllExpiringCerts(ctx)
	if err != nil {
		return fmt.Errorf("cert_expiry_check: list expiring certs: %w", err)
	}

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)

	var notified, skipped int

	for _, cert := range certs {
		if !cert.LotwCertExpiry.Valid {
			continue
		}
		expiryDate := cert.LotwCertExpiry.Time.UTC()
		daysUntilExpiry := int(expiryDate.Sub(today).Hours() / 24)

		locationName := ""
		if cert.LotwLocationName != nil {
			locationName = *cert.LotwLocationName
		}

		for _, threshold := range certExpiryThresholds {
			// Only fire when we're within the threshold window.
			if daysUntilExpiry > threshold {
				continue
			}

			thresholdStr := fmt.Sprintf("%d", threshold)
			expiryStr := expiryDate.Format("2006-01-02")

			// Deduplicate: skip if we already sent this threshold alert recently.
			var expiryPgDate pgtype.Date
			if scanErr := expiryPgDate.Scan(expiryStr); scanErr != nil {
				log.Warn("cert_expiry_check: failed to parse expiry date for dedup",
					slog.String("expiry", expiryStr),
					slog.String("error", scanErr.Error()),
				)
				continue
			}
			count, err := queries.HasRecentCertExpiryNotification(ctx, db.HasRecentCertExpiryNotificationParams{
				UserID:        cert.UserID,
				Callsign:      cert.Callsign,
				ExpiresAt:     expiryPgDate,
				ThresholdDays: thresholdStr,
			})
			if err != nil {
				log.Warn("cert_expiry_check: duplicate check failed",
					slog.Int64("user_id", cert.UserID),
					slog.String("callsign", cert.Callsign),
					slog.String("error", err.Error()),
				)
				continue
			}
			if count > 0 {
				skipped++
				continue
			}

			payload, err := buildCertExpiryPayload(cert.Callsign, locationName, expiryStr, daysUntilExpiry, threshold)
			if err != nil {
				log.Error("cert_expiry_check: build payload failed",
					slog.String("callsign", cert.Callsign),
					slog.String("error", err.Error()),
				)
				continue
			}

			if _, err := queries.CreateWorkerNotification(ctx, db.CreateWorkerNotificationParams{
				UserID:  cert.UserID,
				Type:    "cert_expiry",
				Payload: payload,
			}); err != nil {
				log.Error("cert_expiry_check: create notification failed",
					slog.Int64("user_id", cert.UserID),
					slog.String("callsign", cert.Callsign),
					slog.Int("threshold", threshold),
					slog.String("error", err.Error()),
				)
				continue
			}

			notified++
			log.Info("cert_expiry_check: notification created",
				slog.Int64("user_id", cert.UserID),
				slog.String("callsign", cert.Callsign),
				slog.Int("days_until_expiry", daysUntilExpiry),
				slog.Int("threshold", threshold),
			)
		}
	}

	log.Info("cert expiry check complete",
		slog.Int("certs_checked", len(certs)),
		slog.Int("notifications_created", notified),
		slog.Int("duplicates_skipped", skipped),
	)
	return nil
}

// certExpiryPayload is the JSONB structure stored in notifications.payload for
// cert_expiry type notifications. All fields are present for UI rendering.
type certExpiryPayload struct {
	Callsign      string `json:"callsign"`
	LocationName  string `json:"location_name"`
	ExpiresAt     string `json:"expires_at"` // YYYY-MM-DD
	DaysRemaining int    `json:"days_remaining"`
	ThresholdDays string `json:"threshold_days"` // "60", "30", or "7"
	RenewalURL    string `json:"renewal_url"`
}

func buildCertExpiryPayload(callsign, locationName, expiresAt string, daysRemaining, threshold int) ([]byte, error) {
	p := certExpiryPayload{
		Callsign:      callsign,
		LocationName:  locationName,
		ExpiresAt:     expiresAt,
		DaysRemaining: daysRemaining,
		ThresholdDays: fmt.Sprintf("%d", threshold),
		RenewalURL:    lotwRenewalGuideURL,
	}
	return json.Marshal(p)
}

// DaysUntilExpiry computes the integer number of days between today and
// the given expiry date (rounded toward zero). Exported for unit tests.
func DaysUntilExpiry(today, expiry time.Time) int {
	todayUTC := today.UTC().Truncate(24 * time.Hour)
	expiryUTC := expiry.UTC().Truncate(24 * time.Hour)
	diff := expiryUTC.Sub(todayUTC)
	return int(diff.Hours() / 24)
}

// ShouldNotify returns true when the given daysRemaining value falls within
// the alert window for a threshold. Exported for unit tests.
//
// The rule is: notify when daysRemaining <= threshold.
// This matches how the worker loop fires — it only creates one notification
// per threshold (the duplicate check prevents re-alerting the next day).
func ShouldNotify(daysRemaining, threshold int) bool {
	return daysRemaining <= threshold
}
