package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Logger logs request/response metadata with structured fields.
//
// Logged fields:
//   - request_id
//   - user_id (when authenticated)
//   - method, path, status
//   - duration_ms
//   - request_size, response_size
//   - remote_ip
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseLogger{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		durationMS := time.Since(start).Milliseconds()
		SetDurationMS(r.Context(), durationMS)

		attrs := []slog.Attr{
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", sanitizedPathForLogs(r)),
			slog.Int("status", rw.status),
			slog.Int64("duration_ms", durationMS),
			slog.Int64("request_size", requestSize(r)),
			slog.Int64("response_size", rw.bytesWritten),
			slog.String("remote_ip", ClientIP(r)),
		}

		if userID, ok := UserIDFromContext(r.Context()); ok {
			attrs = append(attrs, slog.Int64("user_id", userID))
		}

		level := slog.LevelInfo
		switch {
		case rw.status >= 500:
			level = slog.LevelError
		case rw.status >= 400:
			level = slog.LevelWarn
		}

		slog.LogAttrs(r.Context(), level, "http_request", attrs...)
	})
}

type responseLogger struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
}

func (rw *responseLogger) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseLogger) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

func requestSize(r *http.Request) int64 {
	if r.ContentLength >= 0 {
		return r.ContentLength
	}
	if v := r.Header.Get("Content-Length"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func sanitizedPathForLogs(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}

	q := r.URL.Query()
	if len(q) == 0 {
		return r.URL.Path
	}

	if q.Has("access_token") {
		q.Set("access_token", "[REDACTED]")
	}
	if q.Has("stream_token") {
		q.Set("stream_token", "[REDACTED]")
	}
	if q.Has("stream_ticket") {
		q.Set("stream_ticket", "[REDACTED]")
	}
	if q.Has("token") {
		q.Set("token", "[REDACTED]")
	}

	return r.URL.Path + "?" + q.Encode()
}
