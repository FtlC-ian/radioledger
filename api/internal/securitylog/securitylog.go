package securitylog

import (
	"context"
	"log/slog"
)

// Event logs a tagged security event.
func Event(ctx context.Context, event string, attrs ...slog.Attr) {
	all := make([]slog.Attr, 0, len(attrs)+2)
	all = append(all,
		slog.String("log_type", "security"),
		slog.String("event", event),
	)
	all = append(all, attrs...)
	slog.LogAttrs(ctx, slog.LevelWarn, "security_event", all...)
}
