package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func init() {
	otlpExporterFactory = func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return otlptracehttp.New(ctx)
	}
}
