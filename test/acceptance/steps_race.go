package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/cucumber/godog"
)

// Race steps fire several bookings in parallel and count the outcomes. Each request is built
// first; the goroutines then wait on one channel and are released together by closing it.
// Which request wins is never asserted: the database decides (design 5.3, 11.3).

func registerRaceSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^"([^"]*)" submits the same booking of "([^"]*)" for "([^"]*)" `+dayPattern+` at `+timePattern+` (\d+) times at the same time$`,
		func(ctx context.Context, who, service, reg, d, hhmm string, n int) error {
			reqs := make([]*preparedRequest, n)
			for i := range reqs {
				b, err := newBooking(who, service, reg, d, hhmm)
				if err != nil {
					return err
				}
				reqs[i] = b
			}
			return w.race(ctx, reqs)
		})
	sc.Step(`^customers "([^"]*)" to "([^"]*)" each book "([^"]*)" for their own vehicle `+dayPattern+` at `+timePattern+` at the same time$`,
		func(ctx context.Context, first, last, service, d, hhmm string) error {
			names, err := customerRange(first, last)
			if err != nil {
				return err
			}
			return w.raceOwnVehicles(ctx, names, service, d, func(int) string { return hhmm })
		})
	sc.Step(`^"([^"]*)" and "([^"]*)" each book "([^"]*)" for their own vehicle `+dayPattern+` at `+timePattern+` at the same time$`,
		func(ctx context.Context, a, b, service, d, hhmm string) error {
			return w.raceOwnVehicles(ctx, []string{a, b}, service, d, func(int) string { return hhmm })
		})
	sc.Step(`^these customers each book "([^"]*)" for their own vehicle `+dayPattern+` at the same time:$`,
		func(ctx context.Context, service, d string, table *godog.Table) error {
			if len(table.Rows) < 2 || len(table.Rows[0].Cells) != 2 {
				return fmt.Errorf("table needs a header row (customer, start) and at least one row")
			}
			var names, starts []string
			for _, row := range table.Rows[1:] {
				names = append(names, row.Cells[0].Value)
				starts = append(starts, row.Cells[1].Value)
			}
			return w.raceOwnVehicles(ctx, names, service, d, func(i int) string { return starts[i] })
		})

	sc.Step(`^exactly (\d+) booking succeeded and (\d+) received (\d+) "([^"]*)"$`, w.exactlyOneSucceeded)
	sc.Step(`^both bookings succeeded$`, func() error { return w.allSucceeded(2) })
	sc.Step(`^all (\d+) bookings succeeded on bay "([^"]*)" with technician "([^"]*)"$`, w.allSucceededOnPair)
	sc.Step(`^all (\d+) responses are 201 with the same appointment id$`, w.allSameAppointment)
	sc.Step(`^the successful appointment is on bay "([^"]*)" with technician "([^"]*)"$`, w.appointmentOnPair)
	sc.Step(`^the successful appointment owns (\d+) (bay|technician) cells from `+timePattern+` to `+timePattern+`$`, w.appointmentOwnsCells)
}

// customerRange expands "Customer 01" to "Customer 20".
func customerRange(first, last string) ([]string, error) {
	const prefix = "Customer "
	from, err1 := strconv.Atoi(strings.TrimPrefix(first, prefix))
	to, err2 := strconv.Atoi(strings.TrimPrefix(last, prefix))
	if !strings.HasPrefix(first, prefix) || !strings.HasPrefix(last, prefix) || err1 != nil || err2 != nil || from > to {
		return nil, fmt.Errorf("want a range like \"Customer 01\" to \"Customer 20\", got %q to %q", first, last)
	}
	var names []string
	for i := from; i <= to; i++ {
		names = append(names, fmt.Sprintf("Customer %02d", i))
	}
	return names, nil
}

func (w *world) raceOwnVehicles(ctx context.Context, names []string, service, d string, startOf func(int) string) error {
	reqs := make([]*preparedRequest, len(names))
	for i, name := range names {
		c, err := customerNamed(name)
		if err != nil {
			return err
		}
		b, err := newBooking(name, service, c.registration, d, startOf(i))
		if err != nil {
			return err
		}
		reqs[i] = b
	}
	return w.race(ctx, reqs)
}

// race sends every request at once and records each outcome; successful bookings are added to
// the scenario's appointments, so "the two appointments" and "the successful appointment"
// steps see them.
func (w *world) race(ctx context.Context, reqs []*preparedRequest) error {
	results := make([]response, len(reqs))
	errs := make([]error, len(reqs))
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i, b := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			results[i], errs[i] = w.post(ctx, b)
		}()
	}
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("request %d of %d: %w", i+1, len(reqs), err)
		}
	}
	w.raced = results
	for _, r := range results {
		if r.status == http.StatusCreated {
			w.record(r)
		}
	}
	return nil
}

func (w *world) exactlyOneSucceeded(ok, failed, status int, code string) error {
	var created, refused []response
	for _, r := range w.raced {
		switch r.status {
		case http.StatusCreated:
			created = append(created, r)
		case status:
			p, err := r.json()
			if err != nil {
				return err
			}
			if p["code"] != code {
				return fmt.Errorf("a %d response has code %v, want %s; body: %s", status, p["code"], code, r.body)
			}
			refused = append(refused, r)
		default:
			return fmt.Errorf("unexpected response %d: %s", r.status, r.body)
		}
	}
	if len(created) != ok || len(refused) != failed {
		return fmt.Errorf("%d succeeded and %d received %s, want %d and %d", len(created), len(refused), code, ok, failed)
	}
	if ok == 1 {
		w.selectResponse(created[0]) // "the successful appointment"; race already recorded it
	}
	return nil
}

func (w *world) allSucceeded(n int) error {
	if len(w.raced) != n {
		return fmt.Errorf("%d requests were raced, want %d", len(w.raced), n)
	}
	for _, r := range w.raced {
		if r.status != http.StatusCreated {
			return fmt.Errorf("a booking got %d, want 201: %s", r.status, r.body)
		}
	}
	return nil
}

func (w *world) allSucceededOnPair(n int, bay, tech string) error {
	if err := w.allSucceeded(n); err != nil {
		return err
	}
	for _, r := range w.raced {
		a, err := r.json()
		if err != nil {
			return err
		}
		if field(a, "serviceBay", "id") != bays[bay] || field(a, "technician", "id") != technicians[tech] {
			return fmt.Errorf("a booking is on %v with %v, want %s with %s",
				nameOf(bays, fmt.Sprint(field(a, "serviceBay", "id"))), nameOf(technicians, fmt.Sprint(field(a, "technician", "id"))), bay, tech)
		}
	}
	return nil
}

func (w *world) allSameAppointment(n int) error {
	if err := w.allSucceeded(n); err != nil {
		return err
	}
	ids := map[any]int{}
	for _, r := range w.raced {
		a, err := r.json()
		if err != nil {
			return err
		}
		ids[a["id"]]++
	}
	if len(ids) != 1 {
		return fmt.Errorf("%d different appointment ids, want 1: %v", len(ids), ids)
	}
	return nil
}
