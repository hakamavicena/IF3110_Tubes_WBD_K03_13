package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

type ctxHandler struct{ slog.Handler }

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	if a := Actor(ctx); a != "" {
		r.AddAttrs(slog.String("actor", a))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h ctxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return ctxHandler{h.Handler.WithAttrs(attrs)}
}

func (h ctxHandler) WithGroup(name string) slog.Handler {
	return ctxHandler{h.Handler.WithGroup(name)}
}

func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
		return a
	}
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, RedactString(a.Value.String()))
	case slog.KindAny:
		switch v := a.Value.Any().(type) {
		case error:
			return slog.String(a.Key, RedactString(v.Error()))
		case []string:
			out := make([]string, len(v))
			for i, s := range v {
				out[i] = RedactString(s)
			}
			return slog.Any(a.Key, out)
		}
	}
	return a
}

func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceAttr})
	return slog.New(ctxHandler{h})
}

func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

func SetupLogging() *slog.Logger {
	l := NewLogger(os.Stdout, ParseLevel(os.Getenv("LOG_LEVEL")))
	slog.SetDefault(l)
	return l
}
