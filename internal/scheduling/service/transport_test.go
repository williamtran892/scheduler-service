package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kithttp "github.com/go-kit/kit/transport/http"
	"go.uber.org/mock/gomock"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

const (
	dealershipUUID  = "d0000000-0000-4000-8000-000000000001"
	serviceUUID     = "5e000000-0000-4000-8000-000000000001"
	customerUUID    = "c0000000-0000-4000-8000-000000000001"
	vehicleUUID     = "f0000000-0000-4000-8000-000000000001"
	appointmentUUID = "8f1c3b7a-5d2e-4c1f-9a6b-2e7d1c4f8a90"
)

// server wires the routes exactly as main does, around a mock Service.
func server(t *testing.T) (*MockService, *httptest.Server) {
	t.Helper()
	svc := NewMockService(gomock.NewController(t))
	mux := http.NewServeMux()
	RegisterRoutes(mux, MakeServerEndpoints(svc), kithttp.ServerErrorEncoder(platform.EncodeError))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return svc, srv
}

func validBody() map[string]any {
	return map[string]any{
		"dealershipId": dealershipUUID, "serviceTypeId": serviceUUID, "customerId": customerUUID, "vehicleId": vehicleUUID,
		"startAt": "2026-10-20T09:00:00+01:00", "kind": "DROP_OFF",
		"customerRequests": map[string]any{"courtesyCar": true, "notes": "Charging port cover loose"},
		"notes":            "Customer prefers a morning call when ready.",
	}
}

func send(t *testing.T, srv *httptest.Server, method, path string, body any, channel string) (*http.Response, map[string]any) {
	t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		r = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if channel != "" {
		req.Header.Set("X-Channel", channel)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var m map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func recordFor(id string) Record {
	r := evRecord()
	r.Appointment.ID = id
	return r
}

func TestTransport_BookIs201WithLocationAndTheRecord(t *testing.T) {
	svc, srv := server(t)
	var got BookCommand
	svc.EXPECT().Book(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, cmd BookCommand) (Record, error) {
		got = cmd
		return recordFor(appointmentUUID), nil
	})

	resp, body := send(t, srv, http.MethodPost, "/appointments", validBody(), "ADVISOR_CONSOLE")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %v", resp.StatusCode, body)
	}
	if loc := resp.Header.Get("Location"); loc != "/appointments/"+appointmentUUID {
		t.Errorf("Location = %q", loc)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if body["id"] != appointmentUUID || get(body, "cells", "bay", "cells") != float64(10) {
		t.Errorf("body = %v, want the staff view of the record", body)
	}
	yes := true
	want := BookCommand{DealershipID: dealershipUUID, ServiceTypeID: serviceUUID, CustomerID: customerUUID, VehicleID: vehicleUUID,
		Kind: scheduling.KindDropOff, Channel: scheduling.ChannelAdvisorConsole, Notes: "Customer prefers a morning call when ready.",
		CustomerRequests: scheduling.CustomerRequests{CourtesyCar: &yes, Notes: "Charging port cover loose"}}
	if !got.StartAt.Equal(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("command start = %s, want 2026-10-20T08:00Z", got.StartAt)
	}
	if got.DealershipID != want.DealershipID || got.ServiceTypeID != want.ServiceTypeID || got.CustomerID != want.CustomerID ||
		got.VehicleID != want.VehicleID || got.Kind != want.Kind || got.Channel != want.Channel || got.Notes != want.Notes ||
		got.CustomerRequests.Notes != want.CustomerRequests.Notes || got.CustomerRequests.CourtesyCar == nil || !*got.CustomerRequests.CourtesyCar {
		t.Errorf("command = %+v\nwant %+v", got, want)
	}
}

func TestTransport_BookDecoderRejections(t *testing.T) {
	without := func(field string) map[string]any { b := validBody(); delete(b, field); return b }
	with := func(field string, v any) map[string]any { b := validBody(); b[field] = v; return b }
	tests := []struct {
		name    string
		body    any
		channel string
		status  int
		code    string
		field   string
	}{
		{"malformed JSON", `{"dealershipId":`, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", ""},
		{"missing dealershipId", without("dealershipId"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "dealershipId"},
		{"missing serviceTypeId", without("serviceTypeId"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "serviceTypeId"},
		{"missing customerId", without("customerId"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "customerId"},
		{"missing vehicleId", without("vehicleId"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "vehicleId"},
		{"missing startAt", without("startAt"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startAt"},
		{"missing kind", without("kind"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "kind"},
		{"startAt without offset", with("startAt", "2026-10-20T09:00:00"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startAt"},
		{"startAt not a date", with("startAt", "tomorrow"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startAt"},
		{"dealershipId not a UUID", with("dealershipId", "MAN01"), "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "dealershipId"},
		{"no channel", validBody(), "", 400, "VALIDATION_ERROR", "X-Channel"},
		{"unknown channel", validBody(), "KIOSK", 422, "INVALID_VALUE", "X-Channel"},
		{"unknown kind", with("kind", "PICKUP"), "ADVISOR_CONSOLE", 422, "INVALID_VALUE", "kind"},
		{"notes over 4000 characters", with("notes", strings.Repeat("n", 4001)), "ADVISOR_CONSOLE", 422, "INVALID_VALUE", "notes"},
		{"customer request notes over 2000 characters", with("customerRequests", map[string]any{"notes": strings.Repeat("n", 2001)}),
			"ADVISOR_CONSOLE", 422, "INVALID_VALUE", "customerRequests.notes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, srv := server(t) // no Book expectation: the service must not be called

			resp, body := send(t, srv, http.MethodPost, "/appointments", tt.body, tt.channel)
			defer resp.Body.Close()

			if resp.StatusCode != tt.status || body["code"] != tt.code {
				t.Fatalf("got %d %v, want %d %s; body %v", resp.StatusCode, body["code"], tt.status, tt.code, body)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q", ct)
			}
			if tt.field != "" && !namesField(body, tt.field) {
				t.Errorf("errors = %v, want one naming %s", body["errors"], tt.field)
			}
		})
	}
}

func TestTransport_TextAtTheLimitsIsAccepted(t *testing.T) {
	svc, srv := server(t)
	svc.EXPECT().Book(gomock.Any(), gomock.Any()).Return(recordFor(appointmentUUID), nil)
	b := validBody()
	b["notes"] = strings.Repeat("n", 4000)
	b["customerRequests"] = map[string]any{"notes": strings.Repeat("n", 2000)}

	resp, body := send(t, srv, http.MethodPost, "/appointments", b, "CALL_CENTRE")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201; body %v", resp.StatusCode, body)
	}
}

func namesField(body map[string]any, field string) bool {
	errs, _ := body["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok && m["field"] == field {
			return true
		}
	}
	return false
}

func TestTransport_BookServiceErrorsBecomeProblems(t *testing.T) {
	alternatives := []scheduling.Slot{{StartAt: on("08:15"), EndAt: on("09:00")}}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		check  func(body map[string]any) bool
	}{
		{"slot taken carries alternatives", &scheduling.SlotTakenError{Alternatives: alternatives, Location: london}, 409, "SLOT_TAKEN",
			func(b map[string]any) bool { a, _ := b["alternatives"].([]any); return len(a) == 1 }},
		{"availability changed carries alternatives", &scheduling.AvailabilityChangedError{Alternatives: alternatives, Location: london}, 409, "AVAILABILITY_CHANGED",
			func(b map[string]any) bool { a, _ := b["alternatives"].([]any); return len(a) == 1 }},
		{"duplicate booking carries the existing appointment",
			&scheduling.DuplicateBookingError{Existing: storedAppointment("interim", scheduling.StatusConfirmed), ServiceTypeCode: "INTERIM", Location: london},
			409, "DUPLICATE_BOOKING", func(b map[string]any) bool { return get(b, "existing", "id") == "existing-1" }},
		{"off grid", scheduling.ErrOffGrid, 422, "OFF_GRID_START", nil},
		{"outside opening hours", scheduling.ErrOutsideOpeningHours, 422, "OUTSIDE_OPENING_HOURS", nil},
		{"no qualified technician", scheduling.ErrNoQualifiedTechnician, 422, "NO_QUALIFIED_TECHNICIAN", nil},
		{"staff-only service", scheduling.ErrNotBookableOnline, 422, "SERVICE_NOT_BOOKABLE_ONLINE", nil},
		{"invalid reference", &platform.FieldError{Code: "INVALID_VALUE", Field: "vehicleId", Message: "belongs to another customer"}, 422, "INVALID_VALUE",
			func(b map[string]any) bool { return namesField(b, "vehicleId") }},
		{"anything else", errors.New("connection reset"), 500, "INTERNAL", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, srv := server(t)
			svc.EXPECT().Book(gomock.Any(), gomock.Any()).Return(Record{}, tt.err)

			resp, body := send(t, srv, http.MethodPost, "/appointments", validBody(), "ADVISOR_CONSOLE")
			defer resp.Body.Close()

			if resp.StatusCode != tt.status || body["code"] != tt.code {
				t.Fatalf("got %d %v, want %d %s; body %v", resp.StatusCode, body["code"], tt.status, tt.code, body)
			}
			if tt.check != nil && !tt.check(body) {
				t.Errorf("body = %v", body)
			}
		})
	}
}

func TestTransport_GetReturnsTheRecordForTheChannel(t *testing.T) {
	svc, srv := server(t)
	gomock.InOrder(
		svc.EXPECT().Get(gomock.Any(), GetQuery{AppointmentID: appointmentUUID}).Return(recordFor(appointmentUUID), nil),
		svc.EXPECT().Get(gomock.Any(), GetQuery{AppointmentID: appointmentUUID, IncludeEvents: true}).Return(recordFor(appointmentUUID), nil),
		svc.EXPECT().Get(gomock.Any(), GetQuery{AppointmentID: appointmentUUID}).Return(recordFor(appointmentUUID), nil),
	)

	resp, body := send(t, srv, http.MethodGet, "/appointments/"+appointmentUUID, nil, "ADVISOR_CONSOLE")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || body["id"] != appointmentUUID || body["cells"] == nil {
		t.Errorf("staff GET = %d %v, want 200 with cells", resp.StatusCode, body)
	}
	resp, _ = send(t, srv, http.MethodGet, "/appointments/"+appointmentUUID+"?include=events", nil, "ADVISOR_CONSOLE")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET with events = %d, want 200", resp.StatusCode)
	}
	resp, body = send(t, srv, http.MethodGet, "/appointments/"+appointmentUUID, nil, "CUSTOMER_APP")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || body["cells"] != nil || body["notes"] != nil {
		t.Errorf("customer GET = %d %v, want 200 without cells or notes", resp.StatusCode, body)
	}
}

func TestTransport_GetFailures(t *testing.T) {
	t.Run("unknown appointment is 404", func(t *testing.T) {
		svc, srv := server(t)
		svc.EXPECT().Get(gomock.Any(), gomock.Any()).Return(Record{}, scheduling.ErrNotFound)
		resp, body := send(t, srv, http.MethodGet, "/appointments/"+appointmentUUID, nil, "ADVISOR_CONSOLE")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || body["code"] != "NOT_FOUND" {
			t.Errorf("got %d %v, want 404 NOT_FOUND", resp.StatusCode, body)
		}
	})
	t.Run("an id that is not a UUID is 404 without asking the service", func(t *testing.T) {
		_, srv := server(t)
		resp, body := send(t, srv, http.MethodGet, "/appointments/not-a-uuid", nil, "ADVISOR_CONSOLE")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || body["code"] != "NOT_FOUND" {
			t.Errorf("got %d %v, want 404 NOT_FOUND", resp.StatusCode, body)
		}
	})
	t.Run("no channel is 400", func(t *testing.T) {
		_, srv := server(t)
		resp, body := send(t, srv, http.MethodGet, "/appointments/"+appointmentUUID, nil, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !namesField(body, "X-Channel") {
			t.Errorf("got %d %v, want 400 naming X-Channel", resp.StatusCode, body)
		}
	})
	t.Run("listing appointments is not served", func(t *testing.T) {
		_, srv := server(t)
		resp, _ := send(t, srv, http.MethodGet, "/appointments", nil, "ADVISOR_CONSOLE")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("GET /appointments = %d, want 405 (listAppointments is not in this feature)", resp.StatusCode)
		}
	})
}

// ---- lifecycle routes (feature 002) --------------------------------------------------------

const (
	technicianUUID = "e0000000-0000-4000-8000-000000000003"
	bayUUID        = "b0000000-0000-4000-8000-000000000001"
)

func lifecyclePath(operation string) string {
	return "/appointments/" + appointmentUUID + "/" + operation
}

func TestTransport_LifecycleRoutesDecodeTheirCommand(t *testing.T) {
	base := TransitionCommand{AppointmentID: appointmentUUID, ExpectedVersion: 2, Reason: "Customer asked",
		Channel: scheduling.ChannelAdvisorConsole}
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	tests := []struct {
		operation string
		body      map[string]any
		expect    func(svc *MockService, got *any)
		want      any
	}{
		{"cancel", map[string]any{"expectedVersion": 2, "reason": "Customer asked"},
			func(s *MockService, got *any) {
				s.EXPECT().Cancel(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, c TransitionCommand) (Record, error) {
					*got = c
					return recordFor(appointmentUUID), nil
				})
			}, base},
		{"no-show", map[string]any{"expectedVersion": 2, "reason": "Customer asked"},
			func(s *MockService, got *any) {
				s.EXPECT().MarkNoShow(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, c TransitionCommand) (Record, error) {
					*got = c
					return recordFor(appointmentUUID), nil
				})
			}, base},
		{"check-in", map[string]any{"expectedVersion": 2, "reason": "Customer asked", "mileage": 41230,
			"additionalConcerns": "Intermittent warning light on cold start"},
			func(s *MockService, got *any) {
				s.EXPECT().CheckIn(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, c CheckInCommand) (Record, error) {
					*got = c
					return recordFor(appointmentUUID), nil
				})
			}, CheckInCommand{TransitionCommand: base, Mileage: func() *int { m := 41230; return &m }(),
				AdditionalConcerns: "Intermittent warning light on cold start"}},
		{"start", map[string]any{"expectedVersion": 2, "reason": "Customer asked", "startedAt": "2026-10-20T09:05:00+01:00"},
			func(s *MockService, got *any) {
				s.EXPECT().Start(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, c StartCommand) (Record, error) {
					*got = c
					return recordFor(appointmentUUID), nil
				})
			}, StartCommand{TransitionCommand: base, StartedAt: func() *time.Time { v := at("2026-10-20T08:05:00Z"); return &v }()}},
		{"complete", map[string]any{"expectedVersion": 2, "reason": "Customer asked", "completedAt": "2026-10-20T10:50:00+01:00"},
			func(s *MockService, got *any) {
				s.EXPECT().Complete(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, c CompleteCommand) (Record, error) {
					*got = c
					return recordFor(appointmentUUID), nil
				})
			}, CompleteCommand{TransitionCommand: base, CompletedAt: func() *time.Time { v := at("2026-10-20T09:50:00Z"); return &v }()}},
		{"reschedule", map[string]any{"expectedVersion": 2, "reason": "Customer asked", "startAt": "2026-10-20T09:30:00+01:00",
			"technicianId": technicianUUID, "serviceBayId": bayUUID},
			func(s *MockService, got *any) {
				s.EXPECT().Reschedule(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, c RescheduleCommand) (Record, error) {
					*got = c
					return recordFor(appointmentUUID), nil
				})
			}, RescheduleCommand{TransitionCommand: base, StartAt: func() *time.Time { v := at("2026-10-20T08:30:00Z"); return &v }(),
				TechnicianID: technicianUUID, ServiceBayID: bayUUID}},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			svc, srv := server(t)
			var got any
			tt.expect(svc, &got)

			resp, body := send(t, srv, http.MethodPost, lifecyclePath(tt.operation), tt.body, "ADVISOR_CONSOLE")
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK || body["id"] != appointmentUUID || get(body, "cells", "bay", "cells") != float64(10) {
				t.Fatalf("got %d %v, want 200 with the staff view of the record", resp.StatusCode, body)
			}
			if !sameCommand(got, tt.want) {
				t.Errorf("command = %+v\nwant      %+v", got, tt.want)
			}
		})
	}
}

// sameCommand compares two lifecycle commands, instants by Equal and pointers by what they hold.
func sameCommand(got, want any) bool {
	sameTime := func(a, b *time.Time) bool { return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b)) }
	sameInt := func(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	switch w := want.(type) {
	case TransitionCommand:
		g, ok := got.(TransitionCommand)
		return ok && g == w
	case CheckInCommand:
		g, ok := got.(CheckInCommand)
		return ok && g.TransitionCommand == w.TransitionCommand && sameInt(g.Mileage, w.Mileage) && g.AdditionalConcerns == w.AdditionalConcerns
	case StartCommand:
		g, ok := got.(StartCommand)
		return ok && g.TransitionCommand == w.TransitionCommand && sameTime(g.StartedAt, w.StartedAt)
	case CompleteCommand:
		g, ok := got.(CompleteCommand)
		return ok && g.TransitionCommand == w.TransitionCommand && sameTime(g.CompletedAt, w.CompletedAt)
	case RescheduleCommand:
		g, ok := got.(RescheduleCommand)
		return ok && g.TransitionCommand == w.TransitionCommand && sameTime(g.StartAt, w.StartAt) &&
			g.TechnicianID == w.TechnicianID && g.ServiceBayID == w.ServiceBayID
	}
	return false
}

func TestTransport_ACustomerChannelGetsTheCustomerViewOfATransition(t *testing.T) {
	svc, srv := server(t)
	svc.EXPECT().Cancel(gomock.Any(), gomock.Any()).Return(recordFor(appointmentUUID), nil)

	resp, body := send(t, srv, http.MethodPost, lifecyclePath("cancel"), map[string]any{"expectedVersion": 1}, "CUSTOMER_WEB")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %v", resp.StatusCode, body)
	}
	if _, ok := body["cells"]; ok {
		t.Errorf("customer view has cells: %v", body)
	}
}

func TestTransport_LifecycleDecoderRejections(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		body      any
		channel   string
		status    int
		code      string
		field     string
	}{
		{"malformed JSON", "cancel", `{"expectedVersion":`, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", ""},
		{"missing expectedVersion", "cancel", map[string]any{"reason": "x"}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "expectedVersion"},
		{"expectedVersion not an integer", "no-show", map[string]any{"expectedVersion": "one"}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "expectedVersion"},
		{"expectedVersion below 1", "check-in", map[string]any{"expectedVersion": 0}, "ADVISOR_CONSOLE", 422, "INVALID_VALUE", "expectedVersion"},
		{"reason over 500 characters", "cancel", map[string]any{"expectedVersion": 1, "reason": strings.Repeat("r", 501)},
			"ADVISOR_CONSOLE", 422, "INVALID_VALUE", "reason"},
		{"negative mileage", "check-in", map[string]any{"expectedVersion": 1, "mileage": -1}, "ADVISOR_CONSOLE", 422, "INVALID_VALUE", "mileage"},
		{"mileage not an integer", "check-in", map[string]any{"expectedVersion": 1, "mileage": "far"}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "mileage"},
		{"additional concerns over 4000 characters", "check-in", map[string]any{"expectedVersion": 1, "additionalConcerns": strings.Repeat("c", 4001)},
			"ADVISOR_CONSOLE", 422, "INVALID_VALUE", "additionalConcerns"},
		{"startedAt without offset", "start", map[string]any{"expectedVersion": 1, "startedAt": "2026-11-02T12:00:00"},
			"ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startedAt"},
		{"completedAt not a date", "complete", map[string]any{"expectedVersion": 1, "completedAt": "yesterday"},
			"ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "completedAt"},
		{"reschedule naming nothing to change", "reschedule", map[string]any{"expectedVersion": 1}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startAt"},
		{"reschedule with only a reason", "reschedule", map[string]any{"expectedVersion": 1, "reason": "x"}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startAt"},
		{"reschedule startAt without offset", "reschedule", map[string]any{"expectedVersion": 1, "startAt": "2026-10-20T09:30:00"},
			"ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "startAt"},
		{"technicianId not a UUID", "reschedule", map[string]any{"expectedVersion": 1, "technicianId": "Lee"}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "technicianId"},
		{"serviceBayId not a UUID", "reschedule", map[string]any{"expectedVersion": 1, "serviceBayId": "Bay 1"}, "ADVISOR_CONSOLE", 400, "VALIDATION_ERROR", "serviceBayId"},
		{"no channel", "complete", map[string]any{"expectedVersion": 1}, "", 400, "VALIDATION_ERROR", "X-Channel"},
		{"unknown channel", "start", map[string]any{"expectedVersion": 1}, "KIOSK", 422, "INVALID_VALUE", "X-Channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, srv := server(t) // no expectation: the service must not be called

			resp, body := send(t, srv, http.MethodPost, lifecyclePath(tt.operation), tt.body, tt.channel)
			defer resp.Body.Close()

			if resp.StatusCode != tt.status || body["code"] != tt.code {
				t.Fatalf("got %d %v, want %d %s; body %v", resp.StatusCode, body["code"], tt.status, tt.code, body)
			}
			if tt.field != "" && !namesField(body, tt.field) {
				t.Errorf("errors = %v, want one naming %s", body["errors"], tt.field)
			}
		})
	}
}

func TestTransport_ALifecycleRouteWithAnIdThatIsNotAUUIDIsNotFound(t *testing.T) {
	for _, operation := range []string{"reschedule", "cancel", "check-in", "start", "complete", "no-show"} {
		t.Run(operation, func(t *testing.T) {
			_, srv := server(t)

			resp, body := send(t, srv, http.MethodPost, "/appointments/not-a-uuid/"+operation, map[string]any{"expectedVersion": 1}, "ADVISOR_CONSOLE")
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound || body["code"] != "NOT_FOUND" {
				t.Errorf("got %d %v, want 404 NOT_FOUND", resp.StatusCode, body)
			}
		})
	}
}

func TestTransport_LifecycleServiceErrorsBecomeProblems(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"a staff action through a customer channel", scheduling.ErrForbidden, 403, "FORBIDDEN"},
		{"a stale version", &scheduling.VersionConflictError{Current: 3, Sent: 2}, 409, "VERSION_CONFLICT"},
		{"a transition the state machine refuses", scheduling.ErrInvalidTransition, 409, "INVALID_TRANSITION"},
		{"a late arrival with nothing free", &scheduling.SlotTakenError{Location: london}, 409, "SLOT_TAKEN"},
		{"an unknown appointment", scheduling.ErrNotFound, 404, "NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, srv := server(t)
			svc.EXPECT().CheckIn(gomock.Any(), gomock.Any()).Return(Record{}, tt.err)

			resp, body := send(t, srv, http.MethodPost, lifecyclePath("check-in"), map[string]any{"expectedVersion": 1}, "ADVISOR_CONSOLE")
			defer resp.Body.Close()

			if resp.StatusCode != tt.status || body["code"] != tt.code {
				t.Errorf("got %d %v, want %d %s", resp.StatusCode, body, tt.status, tt.code)
			}
		})
	}
	t.Run("the version conflict states both versions", func(t *testing.T) {
		svc, srv := server(t)
		svc.EXPECT().Start(gomock.Any(), gomock.Any()).Return(Record{}, &scheduling.VersionConflictError{Current: 3, Sent: 2})

		resp, body := send(t, srv, http.MethodPost, lifecyclePath("start"), map[string]any{"expectedVersion": 2}, "ADVISOR_CONSOLE")
		defer resp.Body.Close()

		if body["detail"] != "Appointment is at version 3; you sent 2." {
			t.Errorf("detail = %v, want the contract's version-conflict detail", body["detail"])
		}
	})
}
