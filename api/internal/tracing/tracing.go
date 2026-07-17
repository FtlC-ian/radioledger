package tracing

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	stdouttrace "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config controls OpenTelemetry initialization.
type Config struct {
	ServiceName  string
	Environment  string
	Exporter     string
	OTLPEndpoint string
}

// Init configures and installs a global tracer provider.
//
// Exporter selection:
//   - "otlp": sends traces to OTEL_EXPORTER_OTLP_ENDPOINT
//   - "stdout": writes spans to stdout (dev-friendly)
//   - "noop": tracing disabled
//
// If Exporter is empty, selection defaults to:
//   - otlp when OTLPEndpoint is set
//   - stdout in development
//   - noop otherwise
func Init(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	exporterKind := resolveExporter(cfg)

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "radioledger-api"
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.DeploymentEnvironment(strings.TrimSpace(cfg.Environment)),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	var tp *sdktrace.TracerProvider
	switch exporterKind {
	case "otlp":
		exporter, err := newOTLPExporter(ctx, cfg.OTLPEndpoint)
		if err != nil {
			return nil, err
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
		)
	case "stdout":
		exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("create stdout trace exporter: %w", err)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
		)
	default: // noop
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.NeverSample()),
			sdktrace.WithResource(res),
		)
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

func resolveExporter(cfg Config) string {
	exporter := strings.ToLower(strings.TrimSpace(cfg.Exporter))
	switch exporter {
	case "otlp", "stdout", "noop":
		return exporter
	}
	if strings.TrimSpace(cfg.OTLPEndpoint) != "" {
		return "otlp"
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Environment), "development") {
		return "stdout"
	}
	return "noop"
}

func newOTLPExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		// Default Jaeger/Tempo OTLP HTTP endpoint.
		endpoint = "http://localhost:4318"
	}

	opts := []otlptracehttp.Option{}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
		if strings.HasPrefix(endpoint, "http://") {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
	} else {
		opts = append(opts, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}
	return exporter, nil
}
