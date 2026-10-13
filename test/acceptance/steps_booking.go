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

// defaultChannel is the channel a booking or fetch states unless a step says otherwise.
const defaultChannel = "ADVISOR_CONSOLE"

// bookingPath is the route of a booking (the contract's BookAppointmentRequest).
const bookingPath = "/appointments"

// preparedRequest is a POST request being built, for a booking or a lifecycle transition: the JSON
// body, the X-Channel header, which is empty when the request has none, and the route it is sent
// to.
type preparedRequest struct {
	body    map[string]any
	channel string
	path    string
}

// newBooking builds the default booking request, for POST bookingPath: dealership MAN01, kind
// DROP_OFF, channel ADVISOR_CONSOLE, no customer requests and no notes.
func newBooking(customerName, service, registration, dayPhrase, hhmm string) (*preparedRequest, error) {
	start, err := instant(dayPhrase, hhmm)
	if err != nil {
		return nil, err
	}
	return newBookingAt(customerName, service, registration, start)
}

// newBookingAt is newBooking for a start given as an instant.
func newBookingAt(customerName, service, registration string, start time.Time) (*preparedRequest, error) {
	c, err := customerNamed(customerName)
	if err != nil {
		return nil, err
	}
	serviceTypeID, err := lookup("service type", serviceTypes, service)
	if err != nil {
		return nil, err
	}
	vehicleID, err := lookup("vehicle", vehicles, registration)
	if err != nil {
		return nil, err
	}
	return &preparedRequest{
		path: bookingPath,
		body: map[string]any{
			"dealershipId":  dealerships["MAN01"],
			"serviceTypeId": serviceTypeID,
			"customerId":    c.id,
			"vehicleId":     vehicleID,
			"startAt":       start.Format("2006-01-02T15:04:05Z07:00"),
			"kind":          "DROP_OFF",
		},
		channel: defaultChannel,
	}, nil
}

// at sends the booking to dealership code instead of MAN01.
func (b *preparedRequest) at(code string) error {
	id, err := lookup("dealership", dealerships, code)
	if err != nil {
		return err
	}
	b.body["dealershipId"] = id
	return nil
}

// apply sets the fields of a two-column "with:" table: kind, notes, waiting, courtesyCar,
// collectionAndDelivery and requestNotes (customerRequests.notes).
func (b *preparedRequest) apply(table *godog.Table) error {
	requests := map[string]any{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("booking table rows have two cells: field and value")
		}
		field, value := row.Cells[0].Value, row.Cells[1].Value
		switch field {
		case "kind", "notes":
			b.body[field] = value
		case "requestNotes":
			requests["notes"] = value
		case "waiting", "courtesyCar", "collectionAndDelivery":
			v, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("booking table %s: %w", field, err)
			}
			requests[field] = v
		default:
			return fmt.Errorf("booking table: unknown field %q", field)
		}
	}
	if len(requests) > 0 {
		b.body["customerRequests"] = requests
	}
	return nil
}

func (b *preparedRequest) headers() map[string]string {
	if b.channel == "" {
		return nil
	}
	return map[string]string{"X-Channel": b.channel}
}

// post sends the prepared request to its route and returns the response without recording it.
func (w *world) post(ctx context.Context, b *preparedRequest) (response, error) {
	body, err := json.Marshal(b.body)
	if err != nil {
		return response{}, err
	}
	return w.do(ctx, http.MethodPost, b.path, body, b.headers())
}

// book sends the booking and records the response.
func (w *world) book(ctx context.Context, b *preparedRequest) error {
	resp, err := w.post(ctx, b)
	if err != nil {
		return err
	}
	w.record(resp)
	return nil
}

func registerBookingSteps(sc *godog.ScenarioContext, w *world) {
	const booking = `"([^"]*)" books "([^"]*)" for "([^"]*)" ` + dayPattern + ` at ` + timePattern +
		`(?: at dealership "([^"]*)")?(?: through "([^"]*)")?`

	sc.Step(`^the workshop fixture for dealership "([^"]*)"$`, w.workshopFixture)

	sc.Step(`^`+booking+`$`, func(ctx context.Context, who, service, reg, d, hhmm, dealer, channel string) error {
		b, err := buildBooking(who, service, reg, d, hhmm, dealer, channel, nil)
		if err != nil {
			return err
		}
		return w.bookAs(ctx, who, b)
	})
	sc.Step(`^`+booking+` with:$`, func(ctx context.Context, who, service, reg, d, hhmm, dealer, channel string, table *godog.Table) error {
		b, err := buildBooking(who, service, reg, d, hhmm, dealer, channel, table)
		if err != nil {
			return err
		}
		return w.bookAs(ctx, who, b)
	})
	const hasBooked = `"([^"]*)" has booked "([^"]*)" for "([^"]*)" ` + dayPattern + ` at ` + timePattern +
		`(?: at dealership "([^"]*)")?(?: through "([^"]*)")?`
	sc.Step(`^`+hasBooked+`$`,
		func(ctx context.Context, who, service, reg, d, hhmm, dealer, channel string) error {
			return w.hasBooked(ctx, who, service, reg, d, hhmm, dealer, channel, nil)
		})
	sc.Step(`^`+hasBooked+` with:$`,
		func(ctx context.Context, who, service, reg, d, hhmm, dealer, channel string, table *godog.Table) error {
			return w.hasBooked(ctx, who, service, reg, d, hhmm, dealer, channel, table)
		})
	const attempted = `"([^"]*)" has attempted to book "([^"]*)" for "([^"]*)" ` + dayPattern + ` at ` + timePattern +
		`(?: at dealership "([^"]*)")?(?: through "([^"]*)")? and been refused`
	sc.Step(`^`+attempted+`$`, func(ctx context.Context, who, service, reg, d, hhmm, dealer, channel string) error {
		b, err := buildBooking(who, service, reg, d, hhmm, dealer, channel, nil)
		if err != nil {
			return err
		}
		return w.refused(w.bookAs(ctx, who, b))
	})

	sc.Step(`^"([^"]*)" prepares a booking of "([^"]*)" for "([^"]*)" `+dayPattern+` at `+timePattern+`$`,
		func(who, service, reg, d, hhmm string) error {
			b, err := newBooking(who, service, reg, d, hhmm)
			w.prepared = b
			return err
		})
	sc.Step(`^the request has "([^"]*)" removed$`, func(field string) error {
		return w.editPrepared(func(b *preparedRequest) { delete(b.body, field) })
	})
	sc.Step(`^the request has "([^"]*)" set to "([^"]*)"$`, func(field, value string) error {
		return w.editPrepared(func(b *preparedRequest) { b.body[field] = value })
	})
	sc.Step(`^the request has "([^"]*)" set to (-?\d+)$`, func(field string, value int) error {
		return w.editPrepared(func(b *preparedRequest) { b.body[field] = value })
	})
	sc.Step(`^the request has no channel$`, func() error {
		return w.editPrepared(func(b *preparedRequest) { b.channel = "" })
	})
	sc.Step(`^the request has no changes$`, func() error {
		return w.editPrepared(func(*preparedRequest) {})
	})
	sc.Step(`^the booking request is sent$`, func(ctx context.Context) error {
		if w.prepared == nil {
			return fmt.Errorf("no booking request was prepared")
		}
		return w.book(ctx, w.prepared)
	})

	sc.Step(`^the appointment is fetched with its events$`, func(ctx context.Context) error {
		return w.fetchCurrent(ctx, defaultChannel, "?include=events")
	})
	sc.Step(`^the appointment is fetched through "([^"]*)"$`, func(ctx context.Context, channel string) error {
		return w.fetchCurrent(ctx, channel, "")
	})
	sc.Step(`^appointment "([^"]*)" is fetched$`, func(ctx context.Context, id string) error {
		return w.fetch(ctx, id, defaultChannel, "")
	})
	sc.Step(`^the appointment of "([^"]*)" is fetched through "([^"]*)"$`, func(ctx context.Context, who, channel string) error {
		id, err := w.appointmentOf(who)
		if err != nil {
			return err
		}
		return w.fetch(ctx, id, channel, "")
	})
}

func buildBooking(who, service, reg, d, hhmm, dealer, channel string, table *godog.Table) (*preparedRequest, error) {
	b, err := newBooking(who, service, reg, d, hhmm)
	if err != nil {
		return nil, err
	}
	if dealer != "" {
		if err := b.at(dealer); err != nil {
			return nil, err
		}
	}
	if channel != "" {
		b.channel = channel
	}
	if table != nil {
		if err := b.apply(table); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// workshopFixture checks the named dealership is in the fixture. The suite has already checked
// the service is healthy; the fixture itself is loaded by the database container.
func (w *world) workshopFixture(code string) error {
	_, err := lookup("dealership", dealerships, code)
	return err
}

// hasBooked books and requires a 201, so later steps can rely on the appointment.
func (w *world) hasBooked(ctx context.Context, who, service, reg, d, hhmm, dealer, channel string, table *godog.Table) error {
	b, err := buildBooking(who, service, reg, d, hhmm, dealer, channel, table)
	if err != nil {
		return err
	}
	return w.bookedOK(ctx, who, b)
}

// bookedOK books for who and requires a 201.
func (w *world) bookedOK(ctx context.Context, who string, b *preparedRequest) error {
	if err := w.bookAs(ctx, who, b); err != nil {
		return err
	}
	if w.status != http.StatusCreated {
		return fmt.Errorf("%s booking %s at %v: status %d, want 201; body: %s",
			who, nameOf(serviceTypes, fmt.Sprint(b.body["serviceTypeId"])), b.body["startAt"], w.status, w.body)
	}
	return nil
}

// refused checks that the request a Given step just sent was refused with a problem (4xx), so a
// scenario can build on a refusal it does not itself assert. err is the error of sending it.
func (w *world) refused(err error) error {
	if err != nil {
		return err
	}
	if w.status < http.StatusBadRequest || w.status >= http.StatusInternalServerError {
		return fmt.Errorf("status %d, want a refusal (4xx); body: %s", w.status, w.body)
	}
	return nil
}

// bookAs books for a named customer and remembers a confirmed booking as that customer's
// appointment, for "the appointment of "<customer>"".
func (w *world) bookAs(ctx context.Context, who string, b *preparedRequest) error {
	if err := w.book(ctx, b); err != nil {
		return err
	}
	if w.status == http.StatusCreated {
		if id, err := w.currentID(); err == nil {
			w.byCustomer[who] = id
		}
	}
	return nil
}

// appointmentOf is the id of the last appointment who booked in this scenario.
func (w *world) appointmentOf(who string) (string, error) {
	id, ok := w.byCustomer[who]
	if !ok {
		return "", fmt.Errorf("%s has booked nothing in this scenario", who)
	}
	return id, nil
}

func (w *world) editPrepared(edit func(*preparedRequest)) error {
	if w.prepared == nil {
		return fmt.Errorf("no booking request was prepared")
	}
	edit(w.prepared)
	return nil
}

// fetchCurrent fetches "the appointment".
func (w *world) fetchCurrent(ctx context.Context, channel, query string) error {
	id, err := w.currentID()
	if err != nil {
		return err
	}
	return w.fetch(ctx, id, channel, query)
}

func (w *world) fetch(ctx context.Context, id, channel, query string) error {
	resp, err := w.do(ctx, http.MethodGet, "/appointments/"+id+query, nil, map[string]string{"X-Channel": channel})
	if err != nil {
		return err
	}
	w.record(resp)
	return nil
}

func (w *world) currentID() (string, error) {
	if w.current == nil {
		return "", fmt.Errorf("no appointment yet in this scenario; last response %d: %s", w.status, w.body)
	}
	id, ok := w.current["id"].(string)
	if !ok {
		return "", fmt.Errorf("the appointment has no string id: %v", w.current["id"])
	}
	return id, nil
}
