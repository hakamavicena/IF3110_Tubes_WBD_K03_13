package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "backend/dbengine"

var otlpExporterFactory func(ctx context.Context) (sdktrace.SpanExporter, error)

var propagator = propagation.TraceContext{}

func InitTracing(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagator)

	kind := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")))
	if kind == "" {
		kind = "console"
	}
	var exp sdktrace.SpanExporter
	var err error
	switch kind {
	case "none":
		return func(context.Context) error { return nil }, nil
	case "console":
		exp, err = stdouttrace.New(stdouttrace.WithWriter(os.Stderr), stdouttrace.WithPrettyPrint())
	case "otlp":
		if otlpExporterFactory == nil {
			return nil, fmt.Errorf("OTEL_TRACES_EXPORTER=otlp membutuhkan build dengan -tags otlp (lihat docs/OBSERVABILITY.md)")
		}
		exp, err = otlpExporterFactory(ctx)
	default:
		return nil, fmt.Errorf("OTEL_TRACES_EXPORTER %q tidak dikenal", kind)
	}
	if err != nil {
		return nil, err
	}

	res := resource.NewSchemaless(attribute.String("service.name", serviceName))
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if kind == "console" {
		opts = append(opts, sdktrace.WithSyncer(exp)) // langsung tampil, cocok untuk demo/log
	} else {
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, span
}

func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, RedactString(err.Error()))
	}
	span.End()
}

func InjectTraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func ExtractTraceParent(ctx context.Context, traceParent string) context.Context {
	if traceParent == "" {
		return ctx
	}
	return propagator.Extract(ctx, propagation.MapCarrier{"traceparent": traceParent})
}

func TraceIDs(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}
