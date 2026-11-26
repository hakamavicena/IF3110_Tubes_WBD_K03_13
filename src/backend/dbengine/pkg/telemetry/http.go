package telemetry

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func Middleware(m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			cid := r.Header.Get("X-Correlation-ID")
			if !ValidCorrelationID(cid) {
				cid = NewCorrelationID()
			}
			w.Header().Set("X-Correlation-ID", cid)

			ctx := WithCorrelationID(r.Context(), cid)
			ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))
			ctx, span := StartSpan(ctx, r.Method+" "+r.URL.Path,
				attribute.String("http.request.method", r.Method),
				attribute.String("url.path", r.URL.Path),
				attribute.String("correlation_id", cid),
			)
			defer span.End()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			dur := time.Since(start)
			span.SetAttributes(attribute.Int("http.response.status_code", rec.status))
			if rec.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rec.status))
			}
			if m != nil {
				m.ObserveHTTP(r.Method, rec.status, dur)
			}
			lvl := slog.LevelInfo
			if rec.status >= 500 {
				lvl = slog.LevelError
			}
			slog.Log(ctx, lvl, "http request", "event", "http.request", "method", r.Method,
				"path", r.URL.Path, "status", rec.status, "duration_ms", dur.Milliseconds())
		})
	}
}
