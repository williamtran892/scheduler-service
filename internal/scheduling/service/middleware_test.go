package service

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"

	"scheduler/internal/scheduling"
)

func TestLoggingMiddleware_LogsMethodIdsDurationAndError(t *testing.T) {
	next := NewMockService(gomock.NewController(t))
	next.EXPECT().Book(gomock.Any(), gomock.Any()).Return(Record{}, scheduling.ErrSlotTaken)
	var buf bytes.Buffer

	_, _ = LoggingMiddleware(log.NewLogfmtLogger(&buf))(next).Book(context.Background(), booking())

	line := buf.String()
	for _, want := range []string{"method=Book", "dealership=man01", "service_type=interim", "took=", "err="} {
		if !strings.Contains(line, want) {
			t.Errorf("log %q has no %q", line, want)
		}
	}
}

func TestInstrumentingMiddleware_CountsCallsByMethodAndCode(t *testing.T) {
	next := NewMockService(gomock.NewController(t))
	next.EXPECT().Book(gomock.Any(), gomock.Any()).Return(Record{}, &scheduling.SlotTakenError{Location: london})
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	_, _ = InstrumentingMiddleware(meter)(next).Book(context.Background(), booking())

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var counted, timed bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "scheduling.requests":
				sum, _ := m.Data.(metricdata.Sum[int64])
				for _, dp := range sum.DataPoints {
					method, _ := dp.Attributes.Value(attribute.Key("method"))
					code, _ := dp.Attributes.Value(attribute.Key("code"))
					if method.AsString() == "Book" && code.AsString() == "SLOT_TAKEN" && dp.Value == 1 {
						counted = true
					}
				}
			case "scheduling.request.duration":
				timed = true
			}
		}
	}
	if !counted || !timed {
		t.Errorf("counted Book/SLOT_TAKEN = %v, duration recorded = %v; metrics %+v", counted, timed, rm.ScopeMetrics)
	}
}

func TestTracingMiddleware_OpensASpanPerCallAndRecordsTheError(t *testing.T) {
	next := NewMockService(gomock.NewController(t))
	next.EXPECT().Book(gomock.Any(), gomock.Any()).Return(Record{}, scheduling.ErrSlotTaken)
	next.EXPECT().Get(gomock.Any(), gomock.Any()).Return(Record{}, nil)
	rec := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("test")
	svc := TracingMiddleware(tracer)(next)

	_, _ = svc.Book(context.Background(), booking())
	_, _ = svc.Get(context.Background(), GetQuery{AppointmentID: "x"})

	spans := rec.Ended()
	if len(spans) != 2 {
		t.Fatalf("%d spans, want 2", len(spans))
	}
	if spans[0].Name() != "scheduling.Book" || spans[0].Status().Code != codes.Error {
		t.Errorf("first span %s status %v, want scheduling.Book with an error", spans[0].Name(), spans[0].Status())
	}
	if spans[1].Name() != "scheduling.Get" || spans[1].Status().Code == codes.Error {
		t.Errorf("second span %s status %v, want scheduling.Get without an error", spans[1].Name(), spans[1].Status())
	}
}

// lifecycleCalls calls each lifecycle method once on svc, so a middleware can be checked for all
// six. The commands name appointment "appt-9".
func lifecycleCalls(svc Service) map[string]func() {
	tc := TransitionCommand{AppointmentID: "appt-9", ExpectedVersion: 2, Channel: scheduling.ChannelAdvisorConsole}
	ctx := context.Background()
	return map[string]func(){
		"Reschedule": func() { _, _ = svc.Reschedule(ctx, RescheduleCommand{TransitionCommand: tc}) },
		"Cancel":     func() { _, _ = svc.Cancel(ctx, tc) },
		"CheckIn":    func() { _, _ = svc.CheckIn(ctx, CheckInCommand{TransitionCommand: tc}) },
		"Start":      func() { _, _ = svc.Start(ctx, StartCommand{TransitionCommand: tc}) },
		"Complete":   func() { _, _ = svc.Complete(ctx, CompleteCommand{TransitionCommand: tc}) },
		"MarkNoShow": func() { _, _ = svc.MarkNoShow(ctx, tc) },
	}
}

// nextReturning is a mock Service whose lifecycle methods all fail with err.
func nextReturning(t *testing.T, err error) *MockService {
	next := NewMockService(gomock.NewController(t))
	next.EXPECT().Reschedule(gomock.Any(), gomock.Any()).AnyTimes().Return(Record{}, err)
	next.EXPECT().Cancel(gomock.Any(), gomock.Any()).AnyTimes().Return(Record{}, err)
	next.EXPECT().CheckIn(gomock.Any(), gomock.Any()).AnyTimes().Return(Record{}, err)
	next.EXPECT().Start(gomock.Any(), gomock.Any()).AnyTimes().Return(Record{}, err)
	next.EXPECT().Complete(gomock.Any(), gomock.Any()).AnyTimes().Return(Record{}, err)
	next.EXPECT().MarkNoShow(gomock.Any(), gomock.Any()).AnyTimes().Return(Record{}, err)
	return next
}

func TestLoggingMiddleware_LogsEveryLifecycleMethodWithItsAppointment(t *testing.T) {
	for method := range lifecycleCalls(nil) {
		t.Run(method, func(t *testing.T) {
			var buf bytes.Buffer
			svc := LoggingMiddleware(log.NewLogfmtLogger(&buf))(nextReturning(t, scheduling.ErrInvalidTransition))
			lifecycleCalls(svc)[method]()
			line := buf.String()
			for _, want := range []string{"method=" + method, "appointment=appt-9", "took=", "err="} {
				if !strings.Contains(line, want) {
					t.Errorf("log %q has no %q", line, want)
				}
			}
		})
	}
}

func TestInstrumentingMiddleware_CountsEveryLifecycleMethodWithItsCode(t *testing.T) {
	for method := range lifecycleCalls(nil) {
		t.Run(method, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
			svc := InstrumentingMiddleware(meter)(nextReturning(t, &scheduling.VersionConflictError{Current: 3, Sent: 2}))
			lifecycleCalls(svc)[method]()

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("collect: %v", err)
			}
			counted := false
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					sum, ok := m.Data.(metricdata.Sum[int64])
					if m.Name != "scheduling.requests" || !ok {
						continue
					}
					for _, dp := range sum.DataPoints {
						got, _ := dp.Attributes.Value(attribute.Key("method"))
						code, _ := dp.Attributes.Value(attribute.Key("code"))
						if got.AsString() == method && code.AsString() == "VERSION_CONFLICT" && dp.Value == 1 {
							counted = true
						}
					}
				}
			}
			if !counted {
				t.Errorf("no scheduling.requests point for %s/VERSION_CONFLICT; metrics %+v", method, rm.ScopeMetrics)
			}
		})
	}
}

func TestTracingMiddleware_OpensASpanPerLifecycleMethod(t *testing.T) {
	for method := range lifecycleCalls(nil) {
		t.Run(method, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("test")
			svc := TracingMiddleware(tracer)(nextReturning(t, scheduling.ErrForbidden))
			lifecycleCalls(svc)[method]()

			spans := rec.Ended()
			if len(spans) != 1 || spans[0].Name() != "scheduling."+method || spans[0].Status().Code != codes.Error {
				t.Fatalf("spans = %v, want one scheduling.%s with an error", spans, method)
			}
		})
	}
}
