package tracing

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type queryTraceKey struct{}

type queryTraceState struct {
	startedAt time.Time
	label     string
	span      trace.Span
}

// PGXTracer instruments pgx query execution for OTEL traces and Prometheus metrics.
type PGXTracer struct{}

// NewPGXTracer returns a pgx tracer implementation for DB observability.
func NewPGXTracer() *PGXTracer { return &PGXTracer{} }

// TraceQueryStart starts a DB client span and stores timing state in context.
func (t *PGXTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	label := metrics.NormalizeQueryLabel(data.SQL)
	tracer := otel.Tracer("radioledger.db")
	ctx, span := tracer.Start(ctx, "db.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.query", label),
			attribute.String("db.operation", operationFromSQL(data.SQL)),
			attribute.String("db.statement", truncateSQL(data.SQL, 240)),
		),
	)

	return context.WithValue(ctx, queryTraceKey{}, queryTraceState{
		startedAt: time.Now(),
		label:     label,
		span:      span,
	})
}

// TraceQueryEnd records query duration metrics and completes the span.
func (t *PGXTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(queryTraceKey{}).(queryTraceState)
	if !ok {
		return
	}

	duration := time.Since(state.startedAt)
	metrics.ObserveDBQueryDuration(state.label, duration)

	if data.Err != nil {
		state.span.RecordError(data.Err)
		state.span.SetStatus(codes.Error, data.Err.Error())
	} else {
		state.span.SetStatus(codes.Ok, "")
	}
	state.span.End()
}

func operationFromSQL(sql string) string {
	sql = strings.TrimSpace(strings.ToUpper(sql))
	if sql == "" {
		return "UNKNOWN"
	}
	parts := strings.Fields(sql)
	if len(parts) == 0 {
		return "UNKNOWN"
	}
	return parts[0]
}

func truncateSQL(sql string, max int) string {
	sql = strings.TrimSpace(sql)
	if max <= 0 || len(sql) <= max {
		return sql
	}
	return sql[:max] + "…"
}
