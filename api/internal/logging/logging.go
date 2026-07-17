package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/FtlC-ian/radioledger/api/internal/middleware"
)

// Config controls slog setup.
type Config struct {
	Env    string
	Level  string
	Format string
	Out    io.Writer
}

// Setup builds and installs the default slog logger.
func Setup(cfg Config) {
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}

	handlerOpts := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}
	format := resolveFormat(cfg.Env, cfg.Format)

	var base slog.Handler
	if format == "text" {
		base = slog.NewTextHandler(cfg.Out, handlerOpts)
	} else {
		base = slog.NewJSONHandler(cfg.Out, handlerOpts)
	}

	h := &contextFieldsHandler{next: base}
	slog.SetDefault(slog.New(h))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func resolveFormat(env, format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "json" || format == "text" {
		return format
	}
	if strings.EqualFold(strings.TrimSpace(env), "development") {
		return "text"
	}
	return "json"
}

type contextFieldsHandler struct {
	next slog.Handler
}

func (h *contextFieldsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextFieldsHandler) Handle(ctx context.Context, record slog.Record) error {
	record.AddAttrs(slog.String("request_id", middleware.RequestIDFromContext(ctx)))
	if userID, ok := middleware.UserIDFromContext(ctx); ok {
		record.AddAttrs(slog.Int64("user_id", userID))
	}
	if duration, ok := middleware.DurationMSFromContext(ctx); ok {
		record.AddAttrs(slog.Int64("duration_ms", duration))
	} else {
		record.AddAttrs(slog.Int64("duration_ms", 0))
	}
	return h.next.Handle(ctx, record)
}

func (h *contextFieldsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextFieldsHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextFieldsHandler) WithGroup(name string) slog.Handler {
	return &contextFieldsHandler{next: h.next.WithGroup(name)}
}

// Security returns a logger tagged for security events.
func Security() *slog.Logger {
	return slog.Default().With("log_type", "security")
}
