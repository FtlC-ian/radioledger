package metrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registerOnce sync.Once

	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "radioledger_http_requests_total",
		Help: "Total HTTP requests handled by the API, partitioned by method/path/status.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "radioledger_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, partitioned by method and path.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	qsosLoggedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "radioledger_qsos_logged_total",
		Help: "Total QSOs logged by the platform.",
	})

	adifImportDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "radioledger_adif_import_duration_seconds",
		Help:    "ADIF import job duration in seconds.",
		Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	})

	adifImportRecords = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "radioledger_adif_import_records_total",
		Help: "ADIF import record outcomes by status (imported|duplicate|error).",
	}, []string{"status"})

	dbQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "radioledger_db_query_duration_seconds",
		Help:    "Database query latency in seconds, partitioned by normalized query name.",
		Buckets: prometheus.DefBuckets,
	}, []string{"query"})

	riverQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_river_queue_depth",
		Help: "Current depth of the River job queue.",
	})

	riverQueueDepthByService = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "river_queue_depth",
		Help: "Current depth of the River queue by service.",
	}, []string{"service"})

	riverJobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "river_job_duration_seconds",
		Help:    "Duration of sync worker jobs by service.",
		Buckets: prometheus.DefBuckets,
	}, []string{"service"})

	riverJobFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "river_job_failures_total",
		Help: "Total sync worker failures by service.",
	}, []string{"service"})
	activeUsers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_active_users",
		Help: "Approximate active authenticated users observed in the last 5 minutes.",
	})

	activeUsersMu   sync.Mutex
	activeUsersSeen = map[int64]time.Time{}

	// DB connection pool gauges (pgxpool).
	dbPoolOpenConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_db_pool_open_connections",
		Help: "Total open connections in the pgxpool (acquired + idle + constructing).",
	})

	dbPoolInUseConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_db_pool_in_use_connections",
		Help: "Connections currently acquired (in use) from the pgxpool.",
	})

	dbPoolIdleConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_db_pool_idle_connections",
		Help: "Idle connections currently waiting in the pgxpool.",
	})

	dbPoolMaxConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_db_pool_max_connections",
		Help: "Maximum number of connections allowed in the pgxpool.",
	})

	dbPoolWaitCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_db_pool_wait_count_total",
		Help: "Cumulative number of times a connection acquire had to wait (pool exhausted).",
	})

	dbPoolWaitDuration = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "radioledger_db_pool_wait_duration_seconds_total",
		Help: "Cumulative duration (seconds) spent waiting for a connection from the pool.",
	})
)

// Init registers all RadioLedger Prometheus metrics exactly once.
func Init() {
	registerOnce.Do(func() {
		prometheus.MustRegister(
			httpRequestsTotal,
			httpRequestDuration,
			qsosLoggedTotal,
			adifImportDuration,
			adifImportRecords,
			dbQueryDuration,
			riverQueueDepth,
			riverQueueDepthByService,
			riverJobDuration,
			riverJobFailures,
			activeUsers,
			dbPoolOpenConns,
			dbPoolInUseConns,
			dbPoolIdleConns,
			dbPoolMaxConns,
			dbPoolWaitCount,
			dbPoolWaitDuration,
		)
	})
}

// Handler returns the Prometheus scrape handler for GET /metrics.
func Handler() http.Handler {
	Init()
	return promhttp.Handler()
}

// Middleware records HTTP request counters and durations for chi handlers.
func Middleware(next http.Handler) http.Handler {
	Init()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusCapturingWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		path := routePattern(r)
		status := strconv.Itoa(rw.status)
		httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
	})
}

// IncQSOsLogged increments the total QSO counter.
func IncQSOsLogged(count int) {
	Init()
	if count > 0 {
		qsosLoggedTotal.Add(float64(count))
	}
}

// ObserveADIFImportDuration records ADIF import duration.
func ObserveADIFImportDuration(d time.Duration) {
	Init()
	if d > 0 {
		adifImportDuration.Observe(d.Seconds())
	}
}

// AddADIFImportRecords increments ADIF record outcome counters.
func AddADIFImportRecords(status string, count int) {
	Init()
	if count <= 0 {
		return
	}
	switch status {
	case "imported", "duplicate", "error":
		adifImportRecords.WithLabelValues(status).Add(float64(count))
	}
}

// ObserveDBQueryDuration records normalized DB query latency.
func ObserveDBQueryDuration(query string, d time.Duration) {
	Init()
	if query == "" {
		query = "unknown"
	}
	dbQueryDuration.WithLabelValues(query).Observe(d.Seconds())
}

// SetRiverQueueDepth sets the River queue depth gauge.
func SetRiverQueueDepth(depth int64) {
	Init()
	riverQueueDepth.Set(float64(depth))
}

func SetRiverQueueDepthForService(service string, depth int64) {
	Init()
	riverQueueDepthByService.WithLabelValues(service).Set(float64(depth))
}

func ObserveRiverJobDuration(service string, d time.Duration) {
	Init()
	if d > 0 {
		riverJobDuration.WithLabelValues(service).Observe(d.Seconds())
	}
}

func IncRiverJobFailure(service string) {
	Init()
	riverJobFailures.WithLabelValues(service).Inc()
}

// MarkUserActive updates the active users gauge for the moving 5-minute window.
func MarkUserActive(userID int64) {
	Init()
	if userID <= 0 {
		return
	}

	now := time.Now()
	cutoff := now.Add(-5 * time.Minute)

	activeUsersMu.Lock()
	activeUsersSeen[userID] = now
	for id, seen := range activeUsersSeen {
		if seen.Before(cutoff) {
			delete(activeUsersSeen, id)
		}
	}
	activeUsers.Set(float64(len(activeUsersSeen)))
	activeUsersMu.Unlock()
}

// StartRiverQueueDepthCollector periodically refreshes the River queue depth gauge.
func StartRiverQueueDepthCollector(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	Init()
	if pool == nil {
		return
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}

	update := func() {
		qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		var depth int64
		err := pool.QueryRow(qctx, `
			SELECT COUNT(*)
			FROM river_job
			WHERE state IN ('available', 'scheduled', 'retryable', 'running')
		`).Scan(&depth)
		if err == nil {
			SetRiverQueueDepth(depth)
		}

		rows, err := pool.Query(qctx, `
			SELECT
				CASE
					WHEN kind LIKE 'eqsl_%' THEN 'eqsl'
					WHEN kind LIKE 'clublog_%' THEN 'clublog'
					WHEN kind LIKE 'lotw_%' THEN 'lotw'
					WHEN kind LIKE 'qrz_%' THEN 'qrz'
					ELSE 'other'
				END AS service,
				COUNT(*)
			FROM river_job
			WHERE state IN ('available', 'scheduled', 'retryable', 'running')
			GROUP BY 1
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var service string
				var cnt int64
				if rows.Scan(&service, &cnt) == nil {
					SetRiverQueueDepthForService(service, cnt)
				}
			}
		}
	}

	update()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				update()
			}
		}
	}()
}

// StartDBPoolMetricsCollector periodically records pgxpool connection pool stats as Prometheus gauges.
// It samples OpenConnections, InUse, Idle, MaxConns, WaitCount, and cumulative WaitDuration.
func StartDBPoolMetricsCollector(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	Init()
	if pool == nil {
		return
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}

	update := func() {
		stat := pool.Stat()
		dbPoolOpenConns.Set(float64(stat.TotalConns()))
		dbPoolInUseConns.Set(float64(stat.AcquiredConns()))
		dbPoolIdleConns.Set(float64(stat.IdleConns()))
		dbPoolMaxConns.Set(float64(stat.MaxConns()))
		dbPoolWaitCount.Set(float64(stat.EmptyAcquireCount()))
		dbPoolWaitDuration.Set(stat.AcquireDuration().Seconds())
	}

	update()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				update()
			}
		}
	}()
}

// NormalizeQueryLabel converts raw SQL into a low-cardinality query label.
func NormalizeQueryLabel(sql string) string {
	s := strings.ToLower(strings.TrimSpace(sql))
	if s == "" {
		return "unknown"
	}

	parts := strings.Fields(s)
	if len(parts) == 0 {
		return "unknown"
	}

	op := parts[0]
	switch op {
	case "select":
		if table := tokenAfter(parts, "from"); table != "" {
			return "select_" + sanitizeToken(table)
		}
		return "select"
	case "insert":
		if table := tokenAfter(parts, "into"); table != "" {
			return "insert_" + sanitizeToken(table)
		}
		return "insert"
	case "update":
		if len(parts) > 1 {
			return "update_" + sanitizeToken(parts[1])
		}
		return "update"
	case "delete":
		if table := tokenAfter(parts, "from"); table != "" {
			return "delete_" + sanitizeToken(table)
		}
		return "delete"
	default:
		return sanitizeToken(op)
	}
}

func tokenAfter(parts []string, needle string) string {
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == needle {
			return parts[i+1]
		}
	}
	return ""
}

func sanitizeToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, `"'`)
	token = strings.TrimSuffix(token, ",")
	token = strings.TrimPrefix(token, "public.")
	token = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, token)
	if token == "" {
		return "unknown"
	}
	return token
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := strings.TrimSpace(rc.RoutePattern()); p != "" {
			return p
		}
	}
	if p := strings.TrimSpace(r.URL.Path); p != "" {
		return p
	}
	return "unknown"
}

type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
