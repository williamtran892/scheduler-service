package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"
)

// Assertions on the Appointment record and the Problem body, both as api/openapi.yaml defines
// them. The ledger is seen only through the record's cell ranges (cells.bay, cells.technician).

func registerRecordSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the response is (\d+)$`, w.responseStatusIs)
	sc.Step(`^the response is (\d+) "([^"]*)"$`, w.responseIsProblem)
	sc.Step(`^the response is (\d+) "([^"]*)" naming the field "([^"]*)"$`, w.responseNamesField)
	sc.Step(`^the response is 201 with an id that is a UUID$`, w.createdWithUUID)
	sc.Step(`^the response is 201 with the same appointment as the first booking$`, w.createdSameAsFirst)
	sc.Step(`^the Location header points at that appointment$`, w.locationPointsAtAppointment)

	sc.Step(`^the appointment status is "([^"]*)" at version (\d+)$`, w.appointmentStatusAndVersion)
	sc.Step(`^the appointment runs from `+timePattern+` to `+timePattern+` `+dayPattern+`$`, w.appointmentRuns)
	sc.Step(`^the appointment is for "([^"]*)" with vehicle "([^"]*)", service type "([^"]*)" at dealership "([^"]*)"$`, w.appointmentIsFor)
	sc.Step(`^the appointment is on bay "([^"]*)" with technician "([^"]*)"$`, w.appointmentOnPair)
	sc.Step(`^the appointment is on bay "([^"]*)"$`, func(bay string) error { return w.appointmentOnPair(bay, "") })
	sc.Step(`^the appointment's bay is of type "([^"]*)" or "([^"]*)"$`, w.appointmentBayTypeIn)
	sc.Step(`^the appointment's technician is one of "([^"]*)", "([^"]*)" or "([^"]*)"$`, w.appointmentTechnicianIn)
	sc.Step(`^the appointment owns (\d+) (bay|technician) cells from `+timePattern+` to `+timePattern+`$`, w.appointmentOwnsCells)
	sc.Step(`^the appointment records the kind "([^"]*)" through "([^"]*)"$`, w.appointmentKindAndChannel)
	sc.Step(`^the appointment has customer requests "([^"]*)"( with a courtesy car)?$`, w.appointmentCustomerRequests)
	sc.Step(`^the appointment has the notes "([^"]*)"$`, w.appointmentNotes)
	sc.Step(`^the record shows no ledger cells and no notes$`, func() error {
		if err := w.recordLacks("cells"); err != nil {
			return err
		}
		return w.recordLacks("notes")
	})
	sc.Step(`^the record shows no notes$`, func() error { return w.recordLacks("notes") })
	sc.Step(`^the record shows the technician by id only$`, w.technicianByIDOnly)
	sc.Step(`^the fetched record equals the booking response$`, w.fetchedEqualsBooked)
	sc.Step(`^the audit trail has exactly one "([^"]*)" event through "([^"]*)"$`, w.auditTrailHasOne)

	sc.Step(`^the response is (\d+) "([^"]*)" with between (\d+) and (\d+) alternatives$`, w.problemWithAlternatives)
	sc.Step(`^the alternatives include `+timePattern+` and `+timePattern+` `+dayPattern+`$`, w.alternativesInclude)
	sc.Step(`^no alternative starts at `+timePattern+` `+dayPattern+`$`, w.noAlternativeAt)
	sc.Step(`^every alternative ends (\d+) minutes after it starts$`, w.everyAlternativeLasts)
	sc.Step(`^the problem carries the first booking as the existing appointment with service type "([^"]*)"$`, w.problemCarriesFirstBooking)
	sc.Step(`^the two appointments hold different bays and different technicians$`, w.lastTwoDisjoint)
}

// ---- status and problem -------------------------------------------------------------------

func (w *world) responseIsProblem(status int, code string) error {
	if err := w.responseStatusIs(status); err != nil {
		return err
	}
	p, err := w.last().json()
	if err != nil {
		return err
	}
	if ct := w.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		return fmt.Errorf("Content-Type is %q, want application/problem+json; body: %s", ct, w.body)
	}
	if p["code"] != code {
		return fmt.Errorf("problem code is %v, want %s; body: %s", p["code"], code, w.body)
	}
	if fmt.Sprint(p["status"]) != fmt.Sprint(status) {
		return fmt.Errorf("problem status is %v, want %d; body: %s", p["status"], status, w.body)
	}
	return nil
}

func (w *world) responseNamesField(status int, code, field string) error {
	if err := w.responseIsProblem(status, code); err != nil {
		return err
	}
	p, _ := w.last().json()
	errs, _ := p["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok && m["field"] == field {
			return nil
		}
	}
	return fmt.Errorf("problem errors[] does not name the field %q; body: %s", field, w.body)
}

func (w *world) createdWithUUID() error {
	if err := w.responseStatusIs(http.StatusCreated); err != nil {
		return err
	}
	id, err := w.currentID()
	if err != nil {
		return err
	}
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("id %q is not a UUID: %w", id, err)
	}
	return nil
}

func (w *world) createdSameAsFirst() error {
	if err := w.createdWithUUID(); err != nil {
		return err
	}
	first, err := w.firstBooking()
	if err != nil {
		return err
	}
	if w.current["id"] != first["id"] {
		return fmt.Errorf("id is %v, want the first booking's %v", w.current["id"], first["id"])
	}
	return nil
}

func (w *world) locationPointsAtAppointment() error {
	id, err := w.currentID()
	if err != nil {
		return err
	}
	loc := w.header.Get("Location")
	if !strings.HasSuffix(loc, "/appointments/"+id) {
		return fmt.Errorf("the Location header is %q, want it to end with /appointments/%s", loc, id)
	}
	return nil
}

// ---- the appointment record ---------------------------------------------------------------

func (w *world) appointmentStatusAndVersion(status string, version int) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if a["status"] != status || fmt.Sprint(a["version"]) != fmt.Sprint(version) {
		return fmt.Errorf("status %v version %v, want %s version %d", a["status"], a["version"], status, version)
	}
	return nil
}

func (w *world) appointmentRuns(from, to, dayPhrase string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	wantStart, err := instant(dayPhrase, from)
	if err != nil {
		return err
	}
	wantEnd, err := instant(dayPhrase, to)
	if err != nil {
		return err
	}
	return sameInstants(a, "startAt", wantStart, "endAt", wantEnd)
}

func (w *world) appointmentIsFor(who, registration, service, dealer string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	c, err := customerNamed(who)
	if err != nil {
		return err
	}
	checks := []struct {
		path []string
		want string
	}{
		{[]string{"customer", "id"}, c.id},
		{[]string{"vehicle", "id"}, vehicles[registration]},
		{[]string{"vehicle", "registration"}, registration},
		{[]string{"serviceType", "id"}, serviceTypes[service]},
		{[]string{"serviceType", "code"}, service},
		{[]string{"dealershipId"}, dealerships[dealer]},
		{[]string{"dealership", "code"}, dealer},
	}
	for _, ch := range checks {
		if got := field(a, ch.path...); got != ch.want {
			return fmt.Errorf("%s is %v, want %s", strings.Join(ch.path, "."), got, ch.want)
		}
	}
	return nil
}

func (w *world) appointmentOnPair(bay, tech string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if got := field(a, "serviceBay", "id"); got != bays[bay] {
		return fmt.Errorf("serviceBay is %s, want %s", nameOf(bays, fmt.Sprint(got)), bay)
	}
	if tech != "" {
		if got := field(a, "technician", "id"); got != technicians[tech] {
			return fmt.Errorf("technician is %s, want %s", nameOf(technicians, fmt.Sprint(got)), tech)
		}
	}
	return nil
}

func (w *world) appointmentBayTypeIn(a1, a2 string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if got := field(a, "serviceBay", "type"); got != a1 && got != a2 {
		return fmt.Errorf("serviceBay.type is %v, want %s or %s", got, a1, a2)
	}
	return nil
}

func (w *world) appointmentTechnicianIn(t1, t2, t3 string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	got := field(a, "technician", "id")
	for _, name := range []string{t1, t2, t3} {
		if got == technicians[name] {
			return nil
		}
	}
	return fmt.Errorf("technician is %v, want one of %s, %s, %s", got, t1, t2, t3)
}

// appointmentOwnsCells checks cells.bay or cells.technician: a half-open range of n cells, with
// local times on the appointment's own day.
func (w *world) appointmentOwnsCells(n int, resource, from, to string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	start, err := parseInstant(a["startAt"], "startAt")
	if err != nil {
		return err
	}
	midnight := sameLocalDay(start)
	wantFrom, err := at(midnight, from)
	if err != nil {
		return err
	}
	wantTo, err := at(midnight, to)
	if err != nil {
		return err
	}
	cells, ok := field(a, "cells", resource).(map[string]any)
	if !ok {
		return fmt.Errorf("record has no cells.%s; record: %s", resource, mustJSON(a))
	}
	if err := sameInstants(cells, "from", wantFrom, "to", wantTo); err != nil {
		return fmt.Errorf("cells.%s: %w", resource, err)
	}
	if fmt.Sprint(cells["cells"]) != fmt.Sprint(n) {
		return fmt.Errorf("cells.%s.cells is %v, want %d", resource, cells["cells"], n)
	}
	return nil
}

func (w *world) appointmentKindAndChannel(kind, channel string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if a["kind"] != kind || a["channel"] != channel {
		return fmt.Errorf("kind %v channel %v, want %s through %s", a["kind"], a["channel"], kind, channel)
	}
	return nil
}

func (w *world) appointmentCustomerRequests(notes, courtesyCar string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if got := field(a, "customerRequests", "notes"); got != notes {
		return fmt.Errorf("customerRequests.notes is %v, want %q", got, notes)
	}
	if courtesyCar != "" && field(a, "customerRequests", "courtesyCar") != true {
		return fmt.Errorf("customerRequests.courtesyCar is %v, want true", field(a, "customerRequests", "courtesyCar"))
	}
	return nil
}

func (w *world) appointmentNotes(notes string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if a["notes"] != notes {
		return fmt.Errorf("notes is %v, want %q", a["notes"], notes)
	}
	return nil
}

// recordLacks checks a field is absent (or empty) on the last appointment.
func (w *world) recordLacks(name string) error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	if v, ok := a[name]; ok && v != nil && v != "" {
		return fmt.Errorf("record has %s = %s, want it absent", name, mustJSON(v))
	}
	return nil
}

func (w *world) technicianByIDOnly() error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	tech, ok := a["technician"].(map[string]any)
	if !ok || tech["id"] == nil {
		return fmt.Errorf("record has no technician.id: %s", mustJSON(a["technician"]))
	}
	if name, ok := tech["displayName"]; ok && name != "" {
		return fmt.Errorf("technician.displayName is %v, want it absent for a customer channel", name)
	}
	return nil
}

func (w *world) fetchedEqualsBooked() error {
	if w.booked == nil {
		return fmt.Errorf("no booking response in this scenario")
	}
	got, err := w.appointment()
	if err != nil {
		return err
	}
	fetched := map[string]any{}
	for k, v := range got {
		if k != "events" {
			fetched[k] = v
		}
	}
	if !reflect.DeepEqual(fetched, w.booked) {
		return fmt.Errorf("fetched record differs from the booking response\nfetched: %s\nbooked:  %s", mustJSON(fetched), mustJSON(w.booked))
	}
	return nil
}

func (w *world) auditTrailHasOne(eventType, channel string) error {
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
	if found[0]["channel"] != channel {
		return fmt.Errorf("%s event channel is %v, want %s", eventType, found[0]["channel"], channel)
	}
	return nil
}

// ---- alternatives and the existing appointment ---------------------------------------------

func (w *world) problemWithAlternatives(status int, code string, lo, hi int) error {
	if err := w.responseIsProblem(status, code); err != nil {
		return err
	}
	alts, err := w.alternatives()
	if err != nil {
		return err
	}
	if len(alts) < lo || len(alts) > hi {
		return fmt.Errorf("%d alternatives, want between %d and %d; body: %s", len(alts), lo, hi, w.body)
	}
	return nil
}

func (w *world) alternativesInclude(t1, t2, dayPhrase string) error {
	alts, err := w.alternatives()
	if err != nil {
		return err
	}
	for _, hhmm := range []string{t1, t2} {
		want, err := instant(dayPhrase, hhmm)
		if err != nil {
			return err
		}
		if !containsStart(alts, want) {
			return fmt.Errorf("alternatives do not include %s %s; body: %s", dayPhrase, hhmm, w.body)
		}
	}
	return nil
}

func (w *world) noAlternativeAt(hhmm, dayPhrase string) error {
	alts, err := w.alternatives()
	if err != nil {
		return err
	}
	want, err := instant(dayPhrase, hhmm)
	if err != nil {
		return err
	}
	if containsStart(alts, want) {
		return fmt.Errorf("an alternative starts at %s %s, the requested time; body: %s", dayPhrase, hhmm, w.body)
	}
	return nil
}

func (w *world) everyAlternativeLasts(minutes int) error {
	alts, err := w.alternatives()
	if err != nil {
		return err
	}
	for _, s := range alts {
		if got := s.end.Sub(s.start); got != time.Duration(minutes)*time.Minute {
			return fmt.Errorf("alternative %s lasts %s, want %d minutes", s.start, got, minutes)
		}
	}
	return nil
}

func (w *world) problemCarriesFirstBooking(serviceCode string) error {
	first, err := w.firstBooking()
	if err != nil {
		return err
	}
	p, err := w.last().json()
	if err != nil {
		return err
	}
	existing, ok := p["existing"].(map[string]any)
	if !ok {
		return fmt.Errorf("problem has no existing appointment; body: %s", w.body)
	}
	if existing["id"] != first["id"] {
		return fmt.Errorf("existing.id is %v, want the first booking's %v", existing["id"], first["id"])
	}
	if existing["serviceTypeCode"] != serviceCode {
		return fmt.Errorf("existing.serviceTypeCode is %v, want %s", existing["serviceTypeCode"], serviceCode)
	}
	for _, f := range []string{"status", "startAt", "endAt"} {
		if existing[f] == nil {
			return fmt.Errorf("existing.%s is missing; body: %s", f, w.body)
		}
	}
	return nil
}

func (w *world) lastTwoDisjoint() error {
	n := len(w.appointments)
	if n < 2 {
		return fmt.Errorf("%d appointments confirmed in this scenario, want at least 2", n)
	}
	a, b := w.appointments[n-2], w.appointments[n-1]
	if field(a, "serviceBay", "id") == field(b, "serviceBay", "id") {
		return fmt.Errorf("both appointments are on bay %v", field(a, "serviceBay", "id"))
	}
	if field(a, "technician", "id") == field(b, "technician", "id") {
		return fmt.Errorf("both appointments are with technician %v", field(a, "technician", "id"))
	}
	return nil
}

// ---- helpers --------------------------------------------------------------------------------

// appointment returns "the appointment": the last appointment a response carried.
func (w *world) appointment() (map[string]any, error) {
	if w.current == nil {
		return nil, fmt.Errorf("no appointment yet in this scenario; last response %d: %s", w.status, w.body)
	}
	return w.current, nil
}

func (w *world) firstBooking() (map[string]any, error) {
	if len(w.appointments) == 0 {
		return nil, fmt.Errorf("no appointment was confirmed in this scenario")
	}
	return w.appointments[0], nil
}

type slot struct{ start, end time.Time }

func (w *world) alternatives() ([]slot, error) {
	p, err := w.last().json()
	if err != nil {
		return nil, err
	}
	raw, ok := p["alternatives"].([]any)
	if !ok {
		return nil, fmt.Errorf("problem has no alternatives[]; body: %s", w.body)
	}
	slots := make([]slot, 0, len(raw))
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("alternatives[%d] is not an object", i)
		}
		start, err := parseInstant(m["startAt"], fmt.Sprintf("alternatives[%d].startAt", i))
		if err != nil {
			return nil, err
		}
		end, err := parseInstant(m["endAt"], fmt.Sprintf("alternatives[%d].endAt", i))
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot{start, end})
	}
	return slots, nil
}

func containsStart(slots []slot, t time.Time) bool {
	for _, s := range slots {
		if s.start.Equal(t) {
			return true
		}
	}
	return false
}

// field walks nested JSON objects; it returns nil when any step is missing.
func field(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = obj[p]
	}
	return v
}

// sameInstants compares two RFC 3339 fields of obj with the wanted instants.
func sameInstants(obj map[string]any, k1 string, want1 time.Time, k2 string, want2 time.Time) error {
	for _, c := range []struct {
		key  string
		want time.Time
	}{{k1, want1}, {k2, want2}} {
		got, err := parseInstant(obj[c.key], c.key)
		if err != nil {
			return err
		}
		if !got.Equal(c.want) {
			return fmt.Errorf("%s is %s, want %s", c.key, got.In(london()).Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
