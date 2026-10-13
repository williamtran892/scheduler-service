package acceptance

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cucumber/godog"
	_ "github.com/go-sql-driver/mysql" // the driver of the @clock backdating step
)

// Time steps for scenarios whose outcome depends on the real time (tag @clock). The service runs
// on the real clock and the suite has no clock or trigger to turn, so an appointment's time is
// moved instead: the backdating step is the only write the suite ever makes to the database, and
// nothing is ever read from it to assert. Outcomes are read from the public record, polled until
// a deadline.

// gridCell is the length of a cell on the fixture dealerships' grid.
const gridCell = 15 * time.Minute

// pollEvery is how often a polling step fetches the record.
const pollEvery = 200 * time.Millisecond

func registerClockSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the appointment(?: of "([^"]*)")? is backdated by (\d+) days$`, w.backdate)
	sc.Step(`^the appointment(?: of "([^"]*)")? becomes "([^"]*)" within (\d+) seconds$`, w.becomes)
	sc.Step(`^the appointment starts at the current grid cell$`, w.startsAtCurrentCell)
}

// database opens the compose MySQL from SCHEDULER_TEST_DSN, once per run, on first use.
func (t *target) database() (*sql.DB, error) {
	t.dbOnce.Do(func() {
		dsn := os.Getenv("SCHEDULER_TEST_DSN")
		if dsn == "" {
			t.dbErr = fmt.Errorf("SCHEDULER_TEST_DSN is not set: @clock scenarios need the compose database; run them with `make test-integration`")
			return
		}
		t.db, t.dbErr = sql.Open("mysql", dsn)
	})
	return t.db, t.dbErr
}

// backdate moves an appointment and every cell it owns n days into the past, in one
// transaction, so appointment and ledger agree. Moving by whole days that land before the
// fixture's first cell (CURRENT_DATE) keeps the cells' primary keys free; status, version and
// every other row are untouched.
func (w *world) backdate(ctx context.Context, who string, days int) error {
	id, err := w.targetOf(who)
	if err != nil {
		return err
	}
	db, err := w.target.database()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("backdate appointment %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op after Commit

	res, err := tx.ExecContext(ctx, `
		UPDATE appointment SET start_at = start_at - INTERVAL ? DAY, end_at = end_at - INTERVAL ? DAY
		 WHERE id = ?`, days, days, id)
	if err != nil {
		return fmt.Errorf("backdate appointment %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("backdate appointment %s: %w", id, err)
	}
	if n != 1 {
		return fmt.Errorf("backdate appointment %s: %d rows changed, want 1", id, n)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE time_slot SET slot_start = slot_start - INTERVAL ? DAY
		 WHERE appointment_id = ?`, days, id); err != nil {
		return fmt.Errorf("backdate the cells of appointment %s: %w", id, err)
	}
	return tx.Commit()
}

// becomes polls the staff record until its status is want or the deadline passes. The last
// fetch becomes "the appointment".
func (w *world) becomes(ctx context.Context, who, want string, seconds int) error {
	id, err := w.targetOf(who)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		if err := w.fetch(ctx, id, defaultChannel, ""); err != nil {
			return err
		}
		if w.status == http.StatusOK && w.current["status"] == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("appointment %s did not become %s within %d seconds; last response %d: %s",
				id, want, seconds, w.status, w.body)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// startsAtCurrentCell checks the appointment starts at the grid cell that contained the moment
// its last transition was sent or answered. One extra cell before is allowed for a small
// difference between the suite's clock and the container's.
func (w *world) startsAtCurrentCell() error {
	a, err := w.appointment()
	if err != nil {
		return err
	}
	start, err := parseInstant(a["startAt"], "startAt")
	if err != nil {
		return err
	}
	if w.sentAt.IsZero() {
		return fmt.Errorf("no transition was sent in this scenario")
	}
	earliest := w.sentAt.UTC().Truncate(gridCell).Add(-gridCell)
	latest := w.answeredAt.UTC().Truncate(gridCell)
	if !start.Equal(start.Truncate(gridCell)) || start.Before(earliest) || start.After(latest) {
		return fmt.Errorf("startAt is %s, want the grid cell of the request, %s to %s",
			start.UTC().Format(time.RFC3339), earliest.Format(time.RFC3339), latest.Format(time.RFC3339))
	}
	return nil
}
