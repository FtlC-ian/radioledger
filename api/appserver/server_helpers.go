package appserver

// server_helpers.go — extracted helpers for server.go:
//   - registerWorkers      (#153)
//   - buildPeriodicJobs    (#153)
//   - scheduleBootstrapSyncs (#153, #154)
//   - randomizedCron / cronScheduleConfig (#155)
//   - scheduleInitialSyncForSource / sourceBootstrapConfig (#154)

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"

	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/jobs"
	callsignsvc "github.com/FtlC-ian/radioledger/api/internal/services/callsign"
	confirmsvc "github.com/FtlC-ian/radioledger/api/internal/services/confirmation"
	lotwsvc "github.com/FtlC-ian/radioledger/api/internal/services/lotw"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// ── #153: registerWorkers ──────────────────────────────────────────────────

// registerWorkers adds all static workers to the river.Workers registry.
// Workers that require RiverClient (cascadeWorker, autoSyncWorker) are NOT
// registered here — they are added in Run() after the client is created.
func registerWorkers(cfg *config.Config, pool *pgxpool.Pool, keyring *crypto.Keyring, vaultClient *lotwsvc.VaultClient) *river.Workers {
	workers := river.NewWorkers()

	river.AddWorker(workers, &jobs.ADIFImportWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &jobs.QRZImportWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &jobs.QSOEnrichmentBackfillWorker{Pool: pool})
	river.AddWorker(workers, &syncsvc.EQSLUploadWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.EQSLDownloadWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.EQSLConfirmationPullWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.ClubLogUploadWorker{Pool: pool, Keyring: keyring, ClubLogAPIKey: cfg.ClubLogAPIKey})
	river.AddWorker(workers, &syncsvc.ClubLogDeleteWorker{Pool: pool, Keyring: keyring, ClubLogAPIKey: cfg.ClubLogAPIKey})
	river.AddWorker(workers, &syncsvc.QRZUploadWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.QRZPollWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.SOTAUploadWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.POTAUploadWorker{Pool: pool, Keyring: keyring, APIBaseURL: cfg.POTAAPIBaseURL, AuthURL: cfg.POTAAuthURL})
	river.AddWorker(workers, &syncsvc.HamQTHUploadWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.HamQTHPollWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.LoTWSyncWorker{Pool: pool, VaultClient: vaultClient, Keyring: keyring})
	river.AddWorker(workers, &syncsvc.LoTWConfirmationPullWorker{Pool: pool, Keyring: keyring})
	river.AddWorker(workers, &jobs.CertExpiryCheckWorker{Pool: pool})
	river.AddWorker(workers, &jobs.AwardRefreshWorker{Pool: pool})
	river.AddWorker(workers, &jobs.AwardProgressRefreshWorker{Pool: pool})
	// autoSyncWorker registered in Run() after riverClient is created.
	river.AddWorker(workers, &jobs.SpotPollerWorker{Pool: pool})
	// PSK Reporter disabled for now — will revisit as desktop-driven or full mirror.
	// river.AddWorker(workers, &jobs.PSKReporterPollerWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.FCCWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.FCCDailySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.ISEDWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.ACMAWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.ANFRWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.IFTWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.RDIWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.OfcomWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.BNetzAWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.NbtcSyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.JJ1WTLMonthlySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.ANATELWeeklySyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.SdppiSyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.PotaSyncWorker{Pool: pool})
	river.AddWorker(workers, &callsignsvc.SotaSyncWorker{Pool: pool})
	river.AddWorker(workers, &confirmsvc.QSOMatchWorker{Pool: pool})
	// cascadeWorker registered in Run() after riverClient is created.
	river.AddWorker(workers, &jobs.GridBatchWorker{Pool: pool})
	river.AddWorker(workers, &jobs.ZCTARefreshWorker{Pool: pool})

	return workers
}

// ── #153: buildPeriodicJobs ────────────────────────────────────────────────

// buildPeriodicJobs constructs all periodic job entries. The mode parameter
// controls whether jobs are actually owned: only "worker" and "all" modes
// register the periodic schedule — the API-only server omits it to avoid
// duplicate firings across containers.
func buildPeriodicJobs(cfg *config.Config, mode string) ([]*river.PeriodicJob, error) {
	if mode == "server" {
		// API-only container: no periodic schedule ownership.
		return nil, nil
	}

	certExpiryPeriodic := river.NewPeriodicJob(
		river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return jobs.CertExpiryCheckArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	)

	// FCC has a unique two-schedule pattern (daily Mon-Sat + weekly Sunday).
	fccDailyCron, fccWeeklyCron := randomizedFCCSyncCrons(cfg)
	slog.Info("FCC daily sync scheduled", slog.String("cron", fccDailyCron))
	slog.Info("FCC weekly sync scheduled", slog.String("cron", fccWeeklyCron))

	fccDailySchedule, err := cron.ParseStandard(fccDailyCron)
	if err != nil {
		return nil, fmt.Errorf("parse FCC daily schedule: %w", err)
	}
	fccDailyPeriodic := river.NewPeriodicJob(
		fccDailySchedule,
		func() (river.JobArgs, *river.InsertOpts) {
			return callsignsvc.FCCDailySyncArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	)

	fccWeeklySchedule, err := cron.ParseStandard(fccWeeklyCron)
	if err != nil {
		return nil, fmt.Errorf("parse FCC weekly schedule: %w", err)
	}
	fccWeeklyPeriodic := river.NewPeriodicJob(
		fccWeeklySchedule,
		func() (river.JobArgs, *river.InsertOpts) {
			return callsignsvc.FCCWeeklySyncArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	)

	// All other sources use the parameterized randomizedCron helper.
	type sourceJobSpec struct {
		cronCfg  cronScheduleConfig
		logLabel string
		newArgs  func() (river.JobArgs, *river.InsertOpts)
	}

	sourceSpecs := []sourceJobSpec{
		{
			cronCfg:  cronScheduleConfig{seedKey: "ised", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "ISED weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.ISEDWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "acma", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "ACMA weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.ACMAWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "anfr", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "ANFR weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.ANFRWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "ift", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "IFT weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.IFTWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "rdi", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "RDI weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.RDIWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "ofcom", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "Ofcom weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.OfcomWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "bnetza", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "BNetzA weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.BNetzAWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "nbtc", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "NBTC weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.NbtcSyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "anatel", hourMin: 1, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "ANATEL weekly sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.ANATELWeeklySyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "pota_parks", hourMin: 3, hourMax: 5, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "POTA park sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.PotaSyncArgs{}, nil },
		},
		{
			cronCfg:  cronScheduleConfig{seedKey: "sota_summits", hourMin: 4, hourMax: 6, cronPattern: "CRON_TZ=UTC %d %d * * 0"},
			logLabel: "SOTA summit sync scheduled",
			newArgs:  func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.SotaSyncArgs{}, nil },
		},
	}

	periodicJobs := []*river.PeriodicJob{
		certExpiryPeriodic,
		fccDailyPeriodic,
		fccWeeklyPeriodic,
	}

	for _, spec := range sourceSpecs {
		cronStr := randomizedCron(cfg, spec.cronCfg)
		slog.Info(spec.logLabel, slog.String("cron", cronStr))
		schedule, err := cron.ParseStandard(cronStr)
		if err != nil {
			return nil, fmt.Errorf("parse %s schedule: %w", spec.cronCfg.seedKey, err)
		}
		fn := spec.newArgs // capture for closure
		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			schedule,
			fn,
			&river.PeriodicJobOpts{RunOnStart: false},
		))
	}

	// JJ1WTL monthly uses a different cron pattern (includes day-of-month).
	jj1wtlCron := randomizedJJ1WTLSyncCron(cfg)
	slog.Info("JJ1WTL monthly sync scheduled", slog.String("cron", jj1wtlCron))
	jj1wtlSchedule, err := cron.ParseStandard(jj1wtlCron)
	if err != nil {
		return nil, fmt.Errorf("parse JJ1WTL monthly schedule: %w", err)
	}
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		jj1wtlSchedule,
		func() (river.JobArgs, *river.InsertOpts) { return callsignsvc.JJ1WTLMonthlySyncArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: false},
	))

	// ZCTA monthly uses a different cron pattern (includes day-of-month).
	zctaCron := randomizedZCTARefreshCron(cfg)
	slog.Info("ZCTA refresh scheduled", slog.String("cron", zctaCron))
	zctaSchedule, err := cron.ParseStandard(zctaCron)
	if err != nil {
		return nil, fmt.Errorf("parse ZCTA refresh schedule: %w", err)
	}
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		zctaSchedule,
		func() (river.JobArgs, *river.InsertOpts) { return jobs.ZCTARefreshArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: false},
	))

	// Fixed-interval jobs.
	periodicJobs = append(periodicJobs,
		river.NewPeriodicJob(
			river.PeriodicInterval(60*time.Second),
			func() (river.JobArgs, *river.InsertOpts) { return jobs.SpotPollerArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		// PSK Reporter disabled — will revisit as desktop-driven or full mirror.
		// river.NewPeriodicJob(
		// 	river.PeriodicInterval(15*time.Minute),
		// 	func() (river.JobArgs, *river.InsertOpts) { return jobs.PSKReporterPollerArgs{}, nil },
		// 	&river.PeriodicJobOpts{RunOnStart: false},
		// ),
		river.NewPeriodicJob(
			river.PeriodicInterval(2*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) { return jobs.AwardProgressRefreshArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(15*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) { return jobs.AutoSyncSchedulerArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: false},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return jobs.GridBatchArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: false},
		),
	)

	return periodicJobs, nil
}

// ── #153: scheduleBootstrapSyncs ──────────────────────────────────────────

// scheduleBootstrapSyncs enqueues first-boot sync jobs for all callsign/park/summit
// sources that have no data yet. Safe to call from a goroutine.
func scheduleBootstrapSyncs(ctx context.Context, pool *pgxpool.Pool, rc *river.Client[pgx.Tx]) {
	sources := bootstrapSourceConfigs()
	for _, src := range sources {
		if err := scheduleInitialSyncForSource(ctx, pool, rc, src); err != nil {
			slog.Error("failed to schedule initial sync",
				slog.String("source", src.source),
				slog.String("error", err.Error()),
			)
		}
	}
}

// ── #153: startHTTPServer ─────────────────────────────────────────────────

// startHTTPServer creates, starts, and returns an *http.Server. The returned
// channel receives any non-ErrServerClosed error from ListenAndServe.
func startHTTPServer(cfg *config.Config, h http.Handler) (*http.Server, chan error) {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      h,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- fmt.Errorf("http server: %w", err)
		}
	}()
	return srv, serverErr
}

// ── #155: randomizedCron ──────────────────────────────────────────────────

// cronScheduleConfig parameterizes a single-cron schedule builder.
type cronScheduleConfig struct {
	// seedKey is appended to the deterministic hash so each source gets a different offset.
	seedKey string
	// hourMin and hourMax are inclusive bounds for the random hour selection.
	hourMin int
	hourMax int
	// cronPattern is a fmt.Sprintf pattern with two %d verbs: minute, hour.
	// A trailing day-of-week / day-of-month field must be included in the pattern.
	// Example: "CRON_TZ=UTC %d %d * * 0"  (Sunday only)
	cronPattern string
}

// randomizedCron generates a stable cron expression for a given source.
// The schedule is deterministic per deployment (seeded by hostname + DB URL + env)
// so it remains consistent across restarts while spreading load across instances.
func randomizedCron(cfg *config.Config, sc cronScheduleConfig) string {
	rng := rand.New(rand.NewSource(scheduleSeed(cfg, sc.seedKey)))
	minute := rng.Intn(60)
	hour := sc.hourMin + rng.Intn(sc.hourMax-sc.hourMin+1)
	return fmt.Sprintf(sc.cronPattern, minute, hour)
}

// randomizedFCCSyncCrons returns (dailyCron, weeklyCron) for FCC.
// FCC is the one outlier: it needs two crons with different day-of-week patterns
// and slightly different hour ranges, so it keeps its own generator.
func randomizedFCCSyncCrons(cfg *config.Config) (dailyCron string, weeklyCron string) {
	rng := rand.New(rand.NewSource(scheduleSeed(cfg, "fcc")))

	dailyMinute := rng.Intn(60)   // 0-59
	dailyHour := 2 + rng.Intn(4)  // 2-5 UTC, Mon-Sat
	weeklyMinute := rng.Intn(60)  // 0-59
	weeklyHour := 1 + rng.Intn(5) // 1-5 UTC, Sunday

	dailyCron = fmt.Sprintf("CRON_TZ=UTC %d %d * * 1-6", dailyMinute, dailyHour)
	weeklyCron = fmt.Sprintf("CRON_TZ=UTC %d %d * * 0", weeklyMinute, weeklyHour)
	return dailyCron, weeklyCron
}

// randomizedJJ1WTLSyncCron generates a monthly cron for the JJ1WTL Japan database.
// Monthly syncs include a randomized day-of-month (1-28) to spread load.
func randomizedJJ1WTLSyncCron(cfg *config.Config) string {
	rng := rand.New(rand.NewSource(scheduleSeed(cfg, "jj1wtl")))

	monthlyMinute := rng.Intn(60)  // 0-59
	monthlyHour := 1 + rng.Intn(5) // 1-5 UTC
	monthlyDay := 1 + rng.Intn(28) // 1-28

	return fmt.Sprintf("CRON_TZ=UTC %d %d %d * *", monthlyMinute, monthlyHour, monthlyDay)
}

// randomizedZCTARefreshCron generates a monthly cron for the Census ZCTA5 refresh.
func randomizedZCTARefreshCron(cfg *config.Config) string {
	rng := rand.New(rand.NewSource(scheduleSeed(cfg, "zcta_refresh")))

	monthlyMinute := rng.Intn(60)  // 0-59
	monthlyHour := 2 + rng.Intn(4) // 2-5 UTC
	monthlyDay := 5 + rng.Intn(10) // 5-14 (early month, after Census updates)

	return fmt.Sprintf("CRON_TZ=UTC %d %d %d * *", monthlyMinute, monthlyHour, monthlyDay)
}

// scheduleSeed returns a stable int64 seed for a given source key.
// The seed is deterministic per deployment so schedules remain consistent across
// restarts while spreading load across different instances.
func scheduleSeed(cfg *config.Config, source string) int64 {
	hasher := fnv.New64a()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}

	_, _ = hasher.Write([]byte(hostname))
	_, _ = hasher.Write([]byte("|"))
	_, _ = hasher.Write([]byte(cfg.DatabaseURL))
	_, _ = hasher.Write([]byte("|"))
	_, _ = hasher.Write([]byte(cfg.Env))
	_, _ = hasher.Write([]byte("|"))
	_, _ = fmt.Fprintf(hasher, "%d", cfg.Port)
	_, _ = hasher.Write([]byte("|"))
	_, _ = hasher.Write([]byte(source))

	return int64(hasher.Sum64())
}

// ── #154: sourceBootstrapConfig / scheduleInitialSyncForSource ────────────

// sourceBootstrapConfig describes one callsign-source initial-sync check.
type sourceBootstrapConfig struct {
	// source is the human-readable name used in log messages (e.g. "FCC", "ISED").
	source string
	// countQuery is the SQL used to detect whether data already exists.
	// It must return a single int64 row.
	countQuery string
	// newJobArgs constructs the River job args to enqueue on first boot.
	newJobArgs func() river.JobArgs
}

// bootstrapSourceConfigs returns the ordered list of source configs for
// first-boot sync scheduling.
func bootstrapSourceConfigs() []sourceBootstrapConfig {
	return []sourceBootstrapConfig{
		{
			source:     "FCC",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'fcc'",
			newJobArgs: func() river.JobArgs { return callsignsvc.FCCWeeklySyncArgs{} },
		},
		{
			source:     "ISED",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'ised'",
			newJobArgs: func() river.JobArgs { return callsignsvc.ISEDWeeklySyncArgs{} },
		},
		{
			source:     "ACMA",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'acma'",
			newJobArgs: func() river.JobArgs { return callsignsvc.ACMAWeeklySyncArgs{} },
		},
		{
			source:     "ANFR",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'anfr'",
			newJobArgs: func() river.JobArgs { return callsignsvc.ANFRWeeklySyncArgs{} },
		},
		{
			source:     "IFT",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'ift'",
			newJobArgs: func() river.JobArgs { return callsignsvc.IFTWeeklySyncArgs{} },
		},
		{
			source:     "RDI",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'rdi'",
			newJobArgs: func() river.JobArgs { return callsignsvc.RDIWeeklySyncArgs{} },
		},
		{
			source:     "Ofcom",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'ofcom'",
			newJobArgs: func() river.JobArgs { return callsignsvc.OfcomWeeklySyncArgs{} },
		},
		{
			source:     "BNetzA",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'bnetza'",
			newJobArgs: func() river.JobArgs { return callsignsvc.BNetzAWeeklySyncArgs{} },
		},
		{
			source:     "NBTC",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'nbtc'",
			newJobArgs: func() river.JobArgs { return callsignsvc.NbtcSyncArgs{} },
		},
		{
			source:     "JJ1WTL",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'jj1wtl'",
			newJobArgs: func() river.JobArgs { return callsignsvc.JJ1WTLMonthlySyncArgs{} },
		},
		{
			source:     "ANATEL",
			countQuery: "SELECT COUNT(*) FROM callsign_records WHERE source = 'anatel'",
			newJobArgs: func() river.JobArgs { return callsignsvc.ANATELWeeklySyncArgs{} },
		},
		{
			source:     "POTA parks",
			countQuery: "SELECT COUNT(*) FROM pota_parks",
			newJobArgs: func() river.JobArgs { return callsignsvc.PotaSyncArgs{} },
		},
		{
			source:     "SOTA summits",
			countQuery: "SELECT COUNT(*) FROM sota_summits",
			newJobArgs: func() river.JobArgs { return callsignsvc.SotaSyncArgs{} },
		},
	}
}

// scheduleInitialSyncForSource checks whether the table for a callsign source is
// empty and, if so, enqueues one bootstrap sync job. It is idempotent — it skips
// enqueueing when a matching job is already pending/running.
func scheduleInitialSyncForSource(ctx context.Context, pool *pgxpool.Pool, rc *river.Client[pgx.Tx], cfg sourceBootstrapConfig) error {
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var count int64
	if err := pool.QueryRow(checkCtx, cfg.countQuery).Scan(&count); err != nil {
		slog.Warn("failed to inspect records; skipping initial bootstrap",
			slog.String("source", cfg.source),
			slog.String("error", err.Error()),
		)
		return nil
	}
	if count > 0 {
		slog.Debug("records already populated; initial bootstrap not needed",
			slog.String("source", cfg.source),
			slog.Int64("record_count", count),
		)
		return nil
	}

	jobArgs := cfg.newJobArgs()
	alreadyQueued, err := hasPendingJob(checkCtx, pool, jobArgs.Kind())
	if err != nil {
		return fmt.Errorf("check pending %s sync jobs: %w", cfg.source, err)
	}
	if alreadyQueued {
		slog.Info("records are empty but sync job is already queued",
			slog.String("source", cfg.source),
		)
		return nil
	}

	slog.Info("first boot detected; scheduling initial sync", slog.String("source", cfg.source))
	if _, err := rc.Insert(checkCtx, jobArgs, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	}); err != nil {
		return fmt.Errorf("enqueue initial %s sync: %w", cfg.source, err)
	}
	slog.Info("scheduled full download", slog.String("source", cfg.source))
	return nil
}

// hasPendingJob returns true if there is already a pending/running River job of the given kind.
func hasPendingJob(ctx context.Context, pool *pgxpool.Pool, kind string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM river_job
			WHERE kind = $1
			  AND state IN ('available', 'scheduled', 'running', 'retryable')
		)
	`, kind).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
