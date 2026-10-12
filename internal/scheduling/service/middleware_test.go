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
