package service

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"scheduler/internal/platform"
)

// Middleware decorates a Service.
type Middleware func(Service) Service

// LoggingMiddleware logs every call with its method, key ids, duration and error.
func LoggingMiddleware(logger log.Logger) Middleware {
	return func(next Service) Service { return loggingService{logger: logger, next: next} }
}

type loggingService struct {
	logger log.Logger
	next   Service
}

func (s loggingService) Book(ctx context.Context, cmd BookCommand) (rec Record, err error) {
	defer func(begin time.Time) {
		_ = s.logger.Log("method", "Book", "dealership", cmd.DealershipID, "service_type", cmd.ServiceTypeID,
			"channel", cmd.Channel, "appointment", rec.Appointment.ID, "took", time.Since(begin), "err", err)
	}(time.Now())
	return s.next.Book(ctx, cmd)
}

func (s loggingService) Get(ctx context.Context, q GetQuery) (rec Record, err error) {
	defer func(begin time.Time) {
		_ = s.logger.Log("method", "Get", "appointment", q.AppointmentID, "events", q.IncludeEvents,
			"took", time.Since(begin), "err", err)
	}(time.Now())
	return s.next.Get(ctx, q)
}

func (s loggingService) Reschedule(ctx context.Context, cmd RescheduleCommand) (Record, error) {
	panic("not implemented")
}

func (s loggingService) Cancel(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

func (s loggingService) CheckIn(ctx context.Context, cmd CheckInCommand) (Record, error) {
	panic("not implemented")
}

func (s loggingService) Start(ctx context.Context, cmd StartCommand) (Record, error) {
	panic("not implemented")
}

func (s loggingService) Complete(ctx context.Context, cmd CompleteCommand) (Record, error) {
	panic("not implemented")
}

func (s loggingService) MarkNoShow(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

// InstrumentingMiddleware counts calls (scheduling.requests) and records their duration
// (scheduling.request.duration) by method and problem code, which gives design 7's SLOT_TAKEN
// rate at confirm. An instrument that cannot be created is a no-op, so metrics never stop a
// booking.
func InstrumentingMiddleware(meter metric.Meter) Middleware {
	requests, _ := meter.Int64Counter("scheduling.requests",
		metric.WithDescription("Scheduling service calls by method and problem code"))
	duration, _ := meter.Float64Histogram("scheduling.request.duration", metric.WithUnit("s"),
		metric.WithDescription("Scheduling service call duration"))
	return func(next Service) Service {
		return instrumentingService{requests: requests, duration: duration, next: next}
	}
}

type instrumentingService struct {
	requests metric.Int64Counter
	duration metric.Float64Histogram
	next     Service
}

func (s instrumentingService) Book(ctx context.Context, cmd BookCommand) (rec Record, err error) {
	defer func(begin time.Time) { s.observe(ctx, "Book", begin, err) }(time.Now())
	return s.next.Book(ctx, cmd)
}

func (s instrumentingService) Get(ctx context.Context, q GetQuery) (rec Record, err error) {
	defer func(begin time.Time) { s.observe(ctx, "Get", begin, err) }(time.Now())
	return s.next.Get(ctx, q)
}

func (s instrumentingService) Reschedule(ctx context.Context, cmd RescheduleCommand) (Record, error) {
	panic("not implemented")
}

func (s instrumentingService) Cancel(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

func (s instrumentingService) CheckIn(ctx context.Context, cmd CheckInCommand) (Record, error) {
	panic("not implemented")
}

func (s instrumentingService) Start(ctx context.Context, cmd StartCommand) (Record, error) {
	panic("not implemented")
}

func (s instrumentingService) Complete(ctx context.Context, cmd CompleteCommand) (Record, error) {
	panic("not implemented")
}

func (s instrumentingService) MarkNoShow(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

func (s instrumentingService) observe(ctx context.Context, method string, begin time.Time, err error) {
	attrs := metric.WithAttributes(attribute.String("method", method), attribute.String("code", outcomeCode(err)))
	s.requests.Add(ctx, 1, attrs)
	s.duration.Record(ctx, time.Since(begin).Seconds(), attrs)
}

// outcomeCode is the problem code of err, OK for success and INTERNAL for an error without one.
func outcomeCode(err error) string {
	if err == nil {
		return "OK"
	}
	var coded platform.Coded
	if errors.As(err, &coded) {
		return coded.ProblemCode()
	}
	return "INTERNAL"
}

// TracingMiddleware opens a span per call, named scheduling.<Method>, and records the error.
func TracingMiddleware(tracer trace.Tracer) Middleware {
	return func(next Service) Service { return tracingService{tracer: tracer, next: next} }
}

type tracingService struct {
	tracer trace.Tracer
	next   Service
}

func (s tracingService) Book(ctx context.Context, cmd BookCommand) (rec Record, err error) {
	ctx, span := s.tracer.Start(ctx, "scheduling.Book")
	defer func() { endSpan(span, err) }()
	return s.next.Book(ctx, cmd)
}

func (s tracingService) Get(ctx context.Context, q GetQuery) (rec Record, err error) {
	ctx, span := s.tracer.Start(ctx, "scheduling.Get")
	defer func() { endSpan(span, err) }()
	return s.next.Get(ctx, q)
}

func (s tracingService) Reschedule(ctx context.Context, cmd RescheduleCommand) (Record, error) {
	panic("not implemented")
}

func (s tracingService) Cancel(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

func (s tracingService) CheckIn(ctx context.Context, cmd CheckInCommand) (Record, error) {
	panic("not implemented")
}

func (s tracingService) Start(ctx context.Context, cmd StartCommand) (Record, error) {
	panic("not implemented")
}

func (s tracingService) Complete(ctx context.Context, cmd CompleteCommand) (Record, error) {
	panic("not implemented")
}

func (s tracingService) MarkNoShow(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
