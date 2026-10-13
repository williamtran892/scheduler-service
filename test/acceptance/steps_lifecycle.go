package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/cucumber/godog"
)

// Lifecycle steps: the six transitions after booking (api/openapi.yaml rescheduleAppointment,
// cancelAppointment, checkInAppointment, startAppointment, completeAppointment, markNoShow) and
// the record assertions they need. Every transition sends the version of the last record the
// suite saw for that appointment, unless the step names one.

// verbPattern is the step-text form of a transition without a body.
const verbPattern = `(checked in|started|completed|cancelled|marked a no-show)`

// operations maps a step's verb to the route segment of its operation.
var operations = map[string]string{
	"checked in":       "check-in",
	"started":          "start",
	"completed":        "complete",
	"cancelled":        "cancel",
	"marked a no-show": "no-show",
}

// reachPath is the legal path from CONFIRMED to each status, as route segments.
var reachPath = map[string][]string{
	"CONFIRMED":   nil,
	"CHECKED_IN":  {"check-in"},
	"IN_PROGRESS": {"check-in", "start"},
	"COMPLETED":   {"check-in", "start", "complete"},
	"CANCELLED":   {"cancel"},
	"NO_SHOW":     {"no-show"},
}

func registerLifecycleSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the appointment(?: of "([^"]*)")? is `+verbPattern+`(?: with version (\d+))?(?: through "([^"]*)")?$`,
		func(ctx context.Context, who, verb, version, channel string) error {
			return w.transition(ctx, who, operations[verb], nil, version, channel)
		})
	sc.Step(`^the appointment has been `+verbPattern+`$`, func(ctx context.Context, verb string) error {
		return w.transitionOK(ctx, operations[verb], nil)
	})
	sc.Step(`^the appointment is started with a start time of `+timePattern+` `+dayPattern+`$`,
		func(ctx context.Context, hhmm, d string) error {
			t, err := instant(d, hhmm)
			if err != nil {
				return err
			}
			return w.transition(ctx, "", "start", map[string]any{"startedAt": t.Format(time.RFC3339)}, "", "")
		})
	sc.Step(`^the appointment has reached "([^"]*)"$`, w.reach)

	sc.Step(`^the appointment is rescheduled to `+timePattern+` `+dayPattern+`(?: with version (\d+))?(?: through "([^"]*)")?$`,
		func(ctx context.Context, hhmm, d, version, channel string) error {
			body, err := startAtBody(hhmm, d)
			if err != nil {
				return err
			}
			return w.transition(ctx, "", "reschedule", body, version, channel)
		})
	sc.Step(`^the appointment has been rescheduled to `+timePattern+` `+dayPattern+`$`,
		func(ctx context.Context, hhmm, d string) error {
			body, err := startAtBody(hhmm, d)
			if err != nil {
				return err
			}
			return w.transitionOK(ctx, "reschedule", body)
		})
	sc.Step(`^the appointment is reassigned to technician "([^"]*)"(?: through "([^"]*)")?$`,
		func(ctx context.Context, tech, channel string) error {
			id, err := lookup("technician", technicians, tech)
			if err != nil {
				return err
			}
			return w.transition(ctx, "", "reschedule", map[string]any{"technicianId": id}, "", channel)
		})
	sc.Step(`^the appointment is reassigned to bay "([^"]*)"$`, func(ctx context.Context, bay string) error {
		id, err := lookup("bay", bays, bay)
		if err != nil {
			return err
		}
		return w.transition(ctx, "", "reschedule", map[string]any{"serviceBayId": id}, "", "")
	})
	sc.Step(`^the appointment has been reassigned to bay "([^"]*)" with technician "([^"]*)"$`,
		func(ctx context.Context, bay, tech string) error {
			bayID, err := lookup("bay", bays, bay)
			if err != nil {
				return err
			}
			techID, err := lookup("technician", technicians, tech)
			if err != nil {
				return err
			}
			return w.transitionOK(ctx, "reschedule", map[string]any{"serviceBayId": bayID, "technicianId": techID})
		})
	sc.Step(`^appointment "([^"]*)" is `+verbPattern+`$`, func(ctx context.Context, id, verb string) error {
		return w.postTransition(ctx, id, operations[verb], map[string]any{"expectedVersion": 1}, defaultChannel)
	})

	sc.Step(`^a "(reschedule|cancel|check-in|start|complete|no-show)" request is prepared for the appointment$`, w.prepareTransition)
	sc.Step(`^the transition request is sent$`, func(ctx context.Context) error {
		if w.prepared == nil || w.prepared.path == "" {
			return fmt.Errorf("no transition request was prepared")
		}
		resp, err := w.post(ctx, w.prepared)
		if err != nil {
			return err
		}
		w.record(resp)
		return nil
	})

	sc.Step(`^the appointment owns no ledger cells$`, w.ownsNoCells)
	sc.Step(`^the appointment owns (\d+) bay cells and (\d+) technician cells$`, w.ownsCellCounts)
	sc.Step(`^the appointment has an actual start time and no actual end time$`, func() error { return w.actualTimes(true, false) })
	sc.Step(`^the appointment has an actual start time and an actual end time$`, func() error { return w.actualTimes(true, true) })
	sc.Step(`^the appointment's actual start is `+timePattern+` `+dayPattern+`$`, w.actualStartIs)
	sc.Step(`^the audit trail has exactly one "([^"]*)" event by "([^"]*)"$`, w.auditTrailHasOneBy)
}

func startAtBody(hhmm, d string) (map[string]any, error) {
	t, err := instant(d, hhmm)
	if err != nil {
		return nil, err
	}
	return map[string]any{"startAt": t.Format(time.RFC3339)}, nil
}

// targetOf returns the id of the appointment a step names: the named customer's, or "the
// appointment".
func (w *world) targetOf(who string) (string, error) {
	if who != "" {
		return w.appointmentOf(who)
	}
	return w.currentID()
}

// versionOf is the version of the last record the suite saw for id.
func (w *world) versionOf(id string) (int, error) {
	rec, ok := w.seen[id]
	if !ok {
		return 0, fmt.Errorf("no record of appointment %s has been seen in this scenario", id)
	}
	v, ok := rec["version"].(float64)
	if !ok {
		return 0, fmt.Errorf("record of appointment %s has no numeric version: %v", id, rec["version"])
	}
	return int(v), nil
}

// transition sends one transition to the appointment who names. body holds the operation's own
// fields; expectedVersion is the last seen version unless version is given; the channel is
// ADVISOR_CONSOLE unless given.
func (w *world) transition(ctx context.Context, who, operation string, body map[string]any, version, channel string) error {
	id, err := w.targetOf(who)
	if err != nil {
		return err
	}
	if body == nil {
		body = map[string]any{}
	}
	if version != "" {
		v, err := strconv.Atoi(version)
		if err != nil {
			return fmt.Errorf("version %q: %w", version, err)
		}
		body["expectedVersion"] = v
	} else {
		v, err := w.versionOf(id)
		if err != nil {
			return err
		}
		body["expectedVersion"] = v
	}
	if channel == "" {
		channel = defaultChannel
	}
	return w.postTransition(ctx, id, operation, body, channel)
}

// postTransition sends POST /appointments/{id}/{operation}, records the response, and remembers when it
// was sent and answered.
func (w *world) postTransition(ctx context.Context, id, operation string, body map[string]any, channel string) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.sentAt = time.Now()
	resp, err := w.do(ctx, http.MethodPost, "/appointments/"+id+"/"+operation, raw, map[string]string{"X-Channel": channel})
	w.answeredAt = time.Now()
	if err != nil {
		return err
	}
	w.record(resp)
	return nil
}

// transitionOK sends a transition to "the appointment" and requires 200.
func (w *world) transitionOK(ctx context.Context, operation string, body map[string]any) error {
	if err := w.transition(ctx, "", operation, body, "", ""); err != nil {
		return err
	}
	if w.status != http.StatusOK {
		return fmt.Errorf("%s: status %d, want 200; body: %s", operation, w.status, w.body)
	}
	return nil
}

// reach drives a confirmed appointment along the legal path to status.
func (w *world) reach(ctx context.Context, status string) error {
	path, ok := reachPath[status]
	if !ok {
		return fmt.Errorf("unknown status %q", status)
	}
	for _, operation := range path {
		if err := w.transitionOK(ctx, operation, nil); err != nil {
			return err
		}
	}
	return nil
}

// prepareTransition builds, without sending, the request for operation on "the appointment":
// the current expectedVersion, the default channel and, for a reschedule, the current startAt.
func (w *world) prepareTransition(operation string) error {
	id, err := w.currentID()
	if err != nil {
		return err
	}
	v, err := w.versionOf(id)
	if err != nil {
		return err
	}
	body := map[string]any{"expectedVersion": v}
	if operation == "reschedule" {
		body["startAt"] = w.seen[id]["startAt"]
	}
	w.prepared = &bookingRequest{body: body, channel: defaultChannel, path: "/appointments/" + id + "/" + operation}
	return nil
}

// ---- record assertions ---------------------------------------------------------------------

// staffCells returns the record's cells object, which only a staff view carries.
func (w *world) staffCells() (map[string]any, error) {
	a, err := w.appointment()
	if err != nil {
		return nil, err
	}
	cells, ok := a["cells"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("record has no cells object (not a staff view?); record: %s", mustJSON(a))
	}
	return cells, nil
}

func (w *world) ownsNoCells() error {
	cells, err := w.staffCells()
	if err != nil {
		return err
	}
	for _, resource := range []string{"bay", "technician"} {
		if r, ok := cells[resource]; ok && r != nil {
			return fmt.Errorf("record still owns cells.%s = %s", resource, mustJSON(r))
		}
	}
	return nil
}

func (w *world) ownsCellCounts(bayCells, techCells int) error {
	cells, err := w.staffCells()
	if err != nil {
		return err
	}
	for resource, want := range map[string]int{"bay": bayCells, "technician": techCells} {
		if got := field(cells, resource, "cells"); fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("cells.%s.cells is %v, want %d; cells: %s", resource, got, want, mustJSON(cells))
		}
	}
	return nil
}

func (w *world) actualTimes(wantStart, wantEnd bool) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	for key, want := range map[string]bool{"actualStartAt": wantStart, "actualEndAt": wantEnd} {
		v := a[key]
		switch {
		case want:
			if _, err := parseInstant(v, key); err != nil {
				return err
			}
		case v != nil:
			return fmt.Errorf("%s is %v, want null", key, v)
		}
	}
	return nil
}

func (w *world) actualStartIs(hhmm, d string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	want, err := instant(d, hhmm)
	if err != nil {
		return err
	}
	got, err := parseInstant(a["actualStartAt"], "actualStartAt")
	if err != nil {
		return err
	}
	if !got.Equal(want) {
		return fmt.Errorf("actualStartAt is %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	return nil
}

// auditTrailHasOneBy checks one event of eventType, recorded by actor with no channel (a
// background job acts through no channel).
func (w *world) auditTrailHasOneBy(eventType, actor string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	events, ok := a["events"].([]any)
	if !ok {
		return fmt.Errorf("record has no events[]; record: %s", mustJSON(a))
	}
	var found []map[string]any
	for _, e := range events {
		if m, ok := e.(map[string]any); ok && m["type"] == eventType {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		return fmt.Errorf("%d %s events, want exactly 1; events: %s", len(found), eventType, mustJSON(events))
	}
	if found[0]["actor"] != actor {
		return fmt.Errorf("%s event actor is %v, want %s", eventType, found[0]["actor"], actor)
	}
	if ch, ok := found[0]["channel"]; ok && ch != nil && ch != "" {
		return fmt.Errorf("%s event channel is %v, want none", eventType, ch)
	}
	return nil
}
