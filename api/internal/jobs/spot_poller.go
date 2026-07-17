package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

const (
	potaSpotsURL = "https://api.pota.app/spot/"
	sotaSpotsURL = "https://api-db.sota.org.uk/spots/all"
)

type SpotPollerArgs struct{}

func (SpotPollerArgs) Kind() string { return "spot_poller" }

type SpotPollerWorker struct {
	river.WorkerDefaults[SpotPollerArgs]
	Pool       *pgxpool.Pool
	HTTPClient *http.Client
}

type externalSpot struct {
	Source       string
	Callsign     string
	Reference    string
	FrequencyKHz *float64
	Band         *string
	Mode         *string
	SpottedAt    time.Time
	RawPayload   []byte
}

type potaSpot struct {
	SpotID    int64  `json:"spotId"`
	SpotTime  string `json:"spotTime"`
	Activator string `json:"activator"`
	Frequency string `json:"frequency"`
	Mode      string `json:"mode"`
	Reference string `json:"reference"`
}

func (w *SpotPollerWorker) Work(ctx context.Context, job *river.Job[SpotPollerArgs]) error {
	log := slog.With(slog.String("job_kind", job.Args.Kind()), slog.Int64("job_id", job.ID))

	hc := w.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}

	potaSpots, err := fetchPOTASpots(ctx, hc)
	if err != nil {
		log.Warn("spot_poller: failed to fetch pota spots", slog.String("error", err.Error()))
	}
	sotaSpots, err := fetchSOTASpots(ctx, hc)
	if err != nil {
		log.Warn("spot_poller: failed to fetch sota spots", slog.String("error", err.Error()))
	}

	allSpots := append(potaSpots, sotaSpots...)
	if len(allSpots) == 0 {
		return nil
	}

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("spot_poller: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("spot_poller: set worker role: %w", err)
	}

	queries := db.New(tx)
	now := time.Now().UTC()

	var upserted, notifications int
	for i, spot := range allSpots {
		savepointName := fmt.Sprintf("spot_upsert_%d", i+1)
		if err := createSavepoint(ctx, tx, savepointName); err != nil {
			return fmt.Errorf("spot_poller: create savepoint %s: %w", savepointName, err)
		}

		if err := queries.UpsertSpot(ctx, db.UpsertSpotParams{
			Source:       spot.Source,
			Callsign:     strings.ToUpper(strings.TrimSpace(spot.Callsign)),
			Reference:    strings.ToUpper(strings.TrimSpace(spot.Reference)),
			FrequencyKhz: float64ToNumeric(spot.FrequencyKHz),
			Band:         normalizeWorkerOptional(spot.Band),
			Mode:         normalizeWorkerOptional(spot.Mode),
			SpottedAt:    pgtype.Timestamptz{Time: spot.SpottedAt.UTC(), Valid: true},
			RawPayload:   spot.RawPayload,
		}); err != nil {
			if rollbackErr := rollbackToSavepoint(ctx, tx, savepointName); rollbackErr != nil {
				return fmt.Errorf("spot_poller: rollback savepoint %s after upsert failure: %w", savepointName, rollbackErr)
			}
			if releaseErr := releaseSavepoint(ctx, tx, savepointName); releaseErr != nil {
				return fmt.Errorf("spot_poller: release savepoint %s after rollback: %w", savepointName, releaseErr)
			}
			log.Warn("spot_poller: upsert spot failed",
				slog.String("source", spot.Source),
				slog.String("reference", spot.Reference),
				slog.String("callsign", spot.Callsign),
				slog.String("savepoint", savepointName),
				slog.String("error", err.Error()),
			)
			continue
		}
		if err := releaseSavepoint(ctx, tx, savepointName); err != nil {
			return fmt.Errorf("spot_poller: release savepoint %s: %w", savepointName, err)
		}
		upserted++

		matches, err := queries.ListMatchingSpotWatchRules(ctx, db.ListMatchingSpotWatchRulesParams{
			Source:    spot.Source,
			Reference: strings.ToUpper(strings.TrimSpace(spot.Reference)),
			Mode:      normalizeWorkerOptional(spot.Mode),
			Band:      normalizeWorkerOptional(spot.Band),
		})
		if err != nil {
			log.Warn("spot_poller: list matching watch rules failed", slog.String("error", err.Error()))
			continue
		}

		for _, match := range matches {
			if !match.NotificationsEnabled {
				continue
			}
			cooldown := time.Duration(match.CooldownMinutes) * time.Minute
			if match.LastNotifiedAt.Valid && cooldown > 0 {
				if now.Sub(match.LastNotifiedAt.Time.UTC()) < cooldown {
					continue
				}
			}

			payload, err := json.Marshal(map[string]any{
				"source":          spot.Source,
				"reference":       strings.ToUpper(strings.TrimSpace(spot.Reference)),
				"callsign":        strings.ToUpper(strings.TrimSpace(spot.Callsign)),
				"mode":            strings.ToUpper(strings.TrimSpace(valueOrEmpty(spot.Mode))),
				"band":            strings.ToUpper(strings.TrimSpace(valueOrEmpty(spot.Band))),
				"spotted_at":      spot.SpottedAt.UTC().Format(time.RFC3339),
				"frequency_khz":   spot.FrequencyKHz,
				"watch_rule_uuid": match.Uuid.String(),
			})
			if err != nil {
				continue
			}

			if _, err := queries.CreateWorkerNotification(ctx, db.CreateWorkerNotificationParams{
				UserID:  match.UserID,
				Type:    "spot_alert",
				Payload: payload,
			}); err != nil {
				log.Warn("spot_poller: create notification failed",
					slog.Int64("user_id", match.UserID),
					slog.String("reference", spot.Reference),
					slog.String("error", err.Error()),
				)
				continue
			}

			if err := queries.UpdateSpotWatchRuleLastNotified(ctx, db.UpdateSpotWatchRuleLastNotifiedParams{
				RuleID:         match.ID,
				LastNotifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
			}); err != nil {
				log.Warn("spot_poller: update last_notified_at failed", slog.String("error", err.Error()))
			}
			notifications++
		}
	}

	if _, err := queries.DeleteSpotsOlderThan(ctx, pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true}); err != nil {
		return fmt.Errorf("spot_poller: cleanup old spots: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("spot_poller: commit tx: %w", err)
	}

	log.Info("spot_poller complete", slog.Int("spots_upserted", upserted), slog.Int("notifications_created", notifications))
	return nil
}

func fetchPOTASpots(ctx context.Context, client *http.Client) ([]externalSpot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, potaSpotsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var rows []potaSpot
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}

	spots := make([]externalSpot, 0, len(rows))
	for _, row := range rows {
		ref := strings.ToUpper(strings.TrimSpace(row.Reference))
		call := strings.ToUpper(strings.TrimSpace(row.Activator))
		if ref == "" || call == "" {
			continue
		}
		spotTime, err := parseSpotTime(row.SpotTime)
		if err != nil {
			continue
		}
		freq := parseFrequencyToKHz(row.Frequency)
		mode := normalizeWorkerOptional(ptrString(strings.TrimSpace(row.Mode)))
		band := deriveBandFromKHz(freq)

		rawPayload, _ := json.Marshal(row)
		spots = append(spots, externalSpot{
			Source:       "pota",
			Callsign:     call,
			Reference:    ref,
			FrequencyKHz: freq,
			Band:         band,
			Mode:         mode,
			SpottedAt:    spotTime,
			RawPayload:   rawPayload,
		})
	}
	return spots, nil
}

func fetchSOTASpots(ctx context.Context, client *http.Client) ([]externalSpot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sotaSpotsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}

	spots := make([]externalSpot, 0, len(rows))
	for _, row := range rows {
		ref := firstString(row, "summitCode", "summit", "SummitCode", "Summit", "summit_ref")
		call := firstString(row, "activatorCallsign", "callsign", "Callsign", "activator", "ActivatorCallsign")
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(call) == "" {
			continue
		}
		spotTime, err := parseSpotTime(firstString(row, "timeStamp", "spotTime", "time", "TimeStamp", "SpotTime"))
		if err != nil {
			continue
		}
		freq := parseFrequencyToKHz(firstString(row, "frequency", "Frequency", "freq"))
		mode := normalizeWorkerOptional(ptrString(firstString(row, "mode", "Mode")))
		band := deriveBandFromKHz(freq)

		rawPayload, _ := json.Marshal(row)
		spots = append(spots, externalSpot{
			Source:       "sota",
			Callsign:     strings.ToUpper(strings.TrimSpace(call)),
			Reference:    strings.ToUpper(strings.TrimSpace(ref)),
			FrequencyKHz: freq,
			Band:         band,
			Mode:         mode,
			SpottedAt:    spotTime,
			RawPayload:   rawPayload,
		})
	}
	return spots, nil
}

func parseSpotTime(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized time format")
}

func parseFrequencyToKHz(raw string) *float64 {
	value := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(raw), "khz"))
	if value == "" {
		return nil
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f <= 0 {
		return nil
	}

	// POTA values are typically already in kHz (e.g., 7064.0).
	// Some APIs provide MHz values (e.g., 14.074), convert those.
	if f < 100 {
		f = f * 1000
	}
	return &f
}

func deriveBandFromKHz(freq *float64) *string {
	if freq == nil {
		return nil
	}
	f := *freq
	switch {
	case f >= 1800 && f <= 2000:
		return ptrString("160M")
	case f >= 3500 && f <= 4000:
		return ptrString("80M")
	case f >= 5330 && f <= 5407:
		return ptrString("60M")
	case f >= 7000 && f <= 7300:
		return ptrString("40M")
	case f >= 10100 && f <= 10150:
		return ptrString("30M")
	case f >= 14000 && f <= 14350:
		return ptrString("20M")
	case f >= 18068 && f <= 18168:
		return ptrString("17M")
	case f >= 21000 && f <= 21450:
		return ptrString("15M")
	case f >= 24890 && f <= 24990:
		return ptrString("12M")
	case f >= 28000 && f <= 29700:
		return ptrString("10M")
	case f >= 50000 && f <= 54000:
		return ptrString("6M")
	case f >= 144000 && f <= 148000:
		return ptrString("2M")
	case f >= 222000 && f <= 225000:
		return ptrString("1.25M")
	case f >= 420000 && f <= 450000:
		return ptrString("70CM")
	default:
		return nil
	}
}

func float64ToNumeric(value *float64) pgtype.Numeric {
	if value == nil {
		return pgtype.Numeric{}
	}
	var n pgtype.Numeric
	if err := n.Scan(*value); err != nil {
		return pgtype.Numeric{}
	}
	return n
}

func normalizeWorkerOptional(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.ToUpper(strings.TrimSpace(*v))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch v := value.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return v
				}
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64)
			case json.Number:
				return v.String()
			}
		}
	}
	return ""
}

func ptrString(v string) *string {
	value := strings.TrimSpace(v)
	if value == "" {
		return nil
	}
	return &value
}

func createSavepoint(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, "SAVEPOINT "+name)
	return err
}

func rollbackToSavepoint(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name)
	return err
}

func releaseSavepoint(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+name)
	return err
}
