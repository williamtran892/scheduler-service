package service

import (
	"github.com/go-kit/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Middleware decorates a Service.
type Middleware func(Service) Service

// LoggingMiddleware logs every call with its method, key ids, duration and error.
func LoggingMiddleware(logger log.Logger) Middleware { panic("not implemented") }

// InstrumentingMiddleware counts calls (scheduling.requests) and records their duration
// (scheduling.request.duration) by method and problem code.
func InstrumentingMiddleware(meter metric.Meter) Middleware { panic("not implemented") }

// TracingMiddleware opens a span per call, named scheduling.<Method>, and records the error.
func TracingMiddleware(tracer trace.Tracer) Middleware { panic("not implemented") }
