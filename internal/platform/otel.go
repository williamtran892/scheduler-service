package platform

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SetupOTel installs the global tracer and meter providers for the configured exporter
// and returns a function that flushes and stops them. With ExporterNone it installs
// nothing, so otel.Tracer and otel.Meter stay no-ops. The OTLP exporters read their
// endpoint and headers from the standard OTEL_EXPORTER_OTLP_* variables.
func SetupOTel(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }
	if cfg.OTelExporter == ExporterNone {
		return noop, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attribute.String("service.name", cfg.ServiceName)),
	)
	if err != nil {
		return noop, fmt.Errorf("otel resource: %w", err)
	}

	var (
		spanExporter   sdktrace.SpanExporter
		metricExporter sdkmetric.Exporter
	)
	switch cfg.OTelExporter {
	case ExporterStdout:
		if spanExporter, err = stdouttrace.New(stdouttrace.WithWriter(os.Stdout)); err != nil {
			return noop, fmt.Errorf("stdout trace exporter: %w", err)
		}
		if metricExporter, err = stdoutmetric.New(stdoutmetric.WithWriter(os.Stdout)); err != nil {
			return noop, fmt.Errorf("stdout metric exporter: %w", err)
		}
	case ExporterOTLP:
		if spanExporter, err = otlptracehttp.New(ctx); err != nil {
			return noop, fmt.Errorf("otlp trace exporter: %w", err)
		}
		if metricExporter, err = otlpmetrichttp.New(ctx); err != nil {
			return noop, fmt.Errorf("otlp metric exporter: %w", err)
		}
	default:
		return noop, fmt.Errorf("unknown OTel exporter %q", cfg.OTelExporter)
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spanExporter), sdktrace.WithResource(res))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)), sdkmetric.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
