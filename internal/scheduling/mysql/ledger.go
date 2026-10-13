package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"scheduler/internal/scheduling"
)

// ClaimPair is the only code that marks cells BOOKED. It claims the bay run, then the technician
// run, always in that order, so every claimer takes its locks in the same order and plain claims
// cannot deadlock each other (design 11.3; notes 4.5).
func (tx *Tx) ClaimPair(dealershipID, bayID string, bayRun scheduling.CellRun,
	techID string, techRun scheduling.CellRun, appointmentID string) error {
	if err := tx.claimRun(dealershipID, scheduling.ResourceBay, bayID, bayRun, appointmentID, scheduling.ErrBayNotOpen); err != nil {
		return err
	}
	return tx.claimRun(dealershipID, scheduling.ResourceTechnician, techID, techRun, appointmentID, scheduling.ErrTechnicianNotWorking)
}

// ReleaseCells frees every cell the appointment owns that keep does not keep and returns how many
// it freed. It locks the appointment's cells, filters them in Go with Keep.Keeps, and frees the
// rest with one UPDATE per resource.
func (tx *Tx) ReleaseCells(appointmentID string, keep scheduling.Keep) (int, error) {
	rows, err := tx.tx.QueryContext(tx.ctx, `
		SELECT resource_kind, resource_id, slot_start
		  FROM time_slot
		 WHERE appointment_id = ?
		 ORDER BY resource_kind, resource_id, slot_start
		   FOR UPDATE`, appointmentID)
	if err != nil {
		return 0, fmt.Errorf("lock cells of appointment %s: %w", appointmentID, mapError(err))
	}
	defer rows.Close()

	type resource struct {
		kind scheduling.ResourceKind
		id   string
	}
	var order []resource
	released := map[resource][]time.Time{}
	for rows.Next() {
		var r resource
		var start time.Time
		if err := rows.Scan(&r.kind, &r.id, &start); err != nil {
			return 0, fmt.Errorf("lock cells of appointment %s: %w", appointmentID, err)
		}
		if keep.Keeps(r.kind, r.id, start) {
			continue
		}
		if _, seen := released[r]; !seen {
			order = append(order, r)
		}
		released[r] = append(released[r], start)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("lock cells of appointment %s: %w", appointmentID, mapError(err))
	}
	// rows is exhausted, hence closed: the connection is free for the UPDATEs.

	freed := 0
	for _, r := range order {
		starts := released[r]
		args := []any{scheduling.CellFree, appointmentID, r.kind, r.id}
		for _, s := range starts {
			args = append(args, s)
		}
		_, err := tx.tx.ExecContext(tx.ctx, `
			UPDATE time_slot SET status = ?, appointment_id = NULL, version = version + 1
			 WHERE appointment_id = ? AND resource_kind = ? AND resource_id = ?
			   AND slot_start IN (?`+strings.Repeat(", ?", len(starts)-1)+`)`, args...)
		if err != nil {
			return freed, fmt.Errorf("free %s %s cells of appointment %s: %w", r.kind, r.id, appointmentID, mapError(err))
		}
		freed += len(starts)
	}
	return freed, nil
}

// cell is one time_slot row as the claim reads it.
type cell struct {
	start         time.Time
	status        scheduling.CellStatus
	appointmentID sql.NullString
}

// claimRun locks the cells of one run, checks in Go that every one exists and is FREE, then marks
// them BOOKED. A cell the appointment already owns counts as free, so a reschedule can claim runs
// that overlap its own cells. The locking read is a current read: two claimers of overlapping
// cells serialise on the first shared row, and the loser sees BOOKED and stops before writing.
// Without FOR UPDATE a read at REPEATABLE READ would see a snapshot and double book.
func (tx *Tx) claimRun(dealershipID string, kind scheduling.ResourceKind, id string, run scheduling.CellRun,
	appointmentID string, missing error) error {
	rows, err := tx.tx.QueryContext(tx.ctx, `
		SELECT slot_start, status, appointment_id
		  FROM time_slot
		 WHERE resource_kind = ? AND resource_id = ? AND dealership_id = ? AND slot_start BETWEEN ? AND ?
		 ORDER BY slot_start
		   FOR UPDATE`, kind, id, dealershipID, run.First, run.Last())
	if err != nil {
		return fmt.Errorf("lock %s %s cells: %w", kind, id, mapError(err))
	}
	defer rows.Close()
	cells, err := scanCells(rows) // reads every row, so the connection is free for the UPDATE
	if err != nil {
		return fmt.Errorf("lock %s %s cells: %w", kind, id, mapError(err))
	}

	if len(cells) < run.Cells {
		return fmt.Errorf("%s %s has %d of the %d cells from %s: %w",
			kind, id, len(cells), run.Cells, run.First.UTC().Format(time.RFC3339), missing)
	}
	for _, c := range cells {
		if c.status != scheduling.CellFree && c.appointmentID.String != appointmentID {
			return fmt.Errorf("%s %s cell %s is booked by appointment %s: %w",
				kind, id, c.start.UTC().Format(time.RFC3339), c.appointmentID.String, scheduling.ErrSlotTaken)
		}
	}

	_, err = tx.tx.ExecContext(tx.ctx, `
		UPDATE time_slot SET status = ?, appointment_id = ?, version = version + 1
		 WHERE resource_kind = ? AND resource_id = ? AND dealership_id = ? AND slot_start BETWEEN ? AND ?`,
		scheduling.CellBooked, appointmentID, kind, id, dealershipID, run.First, run.Last())
	if err != nil {
		return fmt.Errorf("book %s %s cells: %w", kind, id, mapError(err))
	}
	return nil
}

func scanCells(rows *sql.Rows) ([]cell, error) {
	var cells []cell
	for rows.Next() {
		var c cell
		if err := rows.Scan(&c.start, &c.status, &c.appointmentID); err != nil {
			return nil, err
		}
		cells = append(cells, c)
	}
	return cells, rows.Err()
}

// DayLedger returns, for one local day at a dealership, the islands of consecutive FREE cells per
// resource (design 11.4, "free runs per resource") and the BOOKED cell count per resource.
//
// The island query needs the length of a cell to tell consecutive cells from a gap. The
// signature carries no grid, so it joins dealership.slot_minutes: the one column this package
// reads from another domain's table, through time_slot's own foreign key.
//
// The cells booked by owner count as free: they join the islands and are left out of the loads, so
// a reschedule sees the appointment's own time as available (design 5.4). owner is "" for a new
// booking, which no appointment id equals.
func (s *Store) DayLedger(ctx context.Context, dealershipID string, day scheduling.DayWindow, owner string) ([]scheduling.Island, scheduling.Loads, error) {
	islands, err := s.islands(ctx, dealershipID, day, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("free runs of dealership %s on %s: %w", dealershipID, day.LocalDate, err)
	}
	loads, err := s.loads(ctx, dealershipID, day, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("booked cells of dealership %s on %s: %w", dealershipID, day.LocalDate, err)
	}
	return islands, loads, nil
}

func (s *Store) islands(ctx context.Context, dealershipID string, day scheduling.DayWindow, owner string) ([]scheduling.Island, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH free AS (
		  SELECT ts.resource_kind, ts.resource_id, ts.slot_start, d.slot_minutes,
		         -- Subtracting the row number times the slot length from a cell's start gives
		         -- adjacent cells the same run_key: each next cell starts one slot later and has a
		         -- row number one higher. A gap shifts the key, so grouping by it yields the islands.
		         ts.slot_start - INTERVAL d.slot_minutes * ROW_NUMBER() OVER (
		             PARTITION BY ts.resource_kind, ts.resource_id ORDER BY ts.slot_start) MINUTE AS run_key
		    FROM time_slot ts
		    JOIN dealership d ON d.id = ts.dealership_id
		   WHERE ts.dealership_id = ? AND (ts.status = ? OR ts.appointment_id = ?)
		     AND ts.slot_start >= ? AND ts.slot_start < ?)
		SELECT resource_kind, resource_id, MIN(slot_start), MAX(slot_start) + INTERVAL slot_minutes MINUTE
		  FROM free
		 GROUP BY resource_kind, resource_id, slot_minutes, run_key
		 ORDER BY resource_kind, resource_id, MIN(slot_start)`,
		dealershipID, scheduling.CellFree, owner, day.From, day.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var islands []scheduling.Island
	for rows.Next() {
		var i scheduling.Island
		if err := rows.Scan(&i.Kind, &i.ResourceID, &i.From, &i.To); err != nil {
			return nil, err
		}
		islands = append(islands, i)
	}
	return islands, rows.Err()
}

func (s *Store) loads(ctx context.Context, dealershipID string, day scheduling.DayWindow, owner string) (scheduling.Loads, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT resource_id, COUNT(*) FROM time_slot
		 WHERE dealership_id = ? AND status = ? AND appointment_id <> ? AND slot_start >= ? AND slot_start < ?
		 GROUP BY resource_kind, resource_id`,
		dealershipID, scheduling.CellBooked, owner, day.From, day.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	loads := scheduling.Loads{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		loads[id] = n
	}
	return loads, rows.Err()
}

// WorkingCells counts the distinct cell starts in [from, to) for which the dealership has a
// ledger cell on any resource, in any status: the dealership's working time (the ledger is the working time in this release).
func (s *Store) WorkingCells(ctx context.Context, dealershipID string, from, to time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT slot_start) FROM time_slot
		 WHERE dealership_id = ? AND slot_start >= ? AND slot_start < ?`, dealershipID, from, to).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count working cells of dealership %s: %w", dealershipID, err)
	}
	return n, nil
}

// OwnedCells returns the cell ranges an appointment owns on its bay and on its technician, as
// half-open intervals (contract CellRange). The end of a range is its last cell plus one cell,
// so this query too joins dealership.slot_minutes (see DayLedger).
func (s *Store) OwnedCells(ctx context.Context, appointmentID string) (scheduling.OwnedCells, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts.resource_kind, MIN(ts.slot_start), MAX(ts.slot_start) + INTERVAL d.slot_minutes MINUTE, COUNT(*)
		  FROM time_slot ts
		  JOIN dealership d ON d.id = ts.dealership_id
		 WHERE ts.appointment_id = ?
		 GROUP BY ts.resource_kind, d.slot_minutes`, appointmentID)
	if err != nil {
		return scheduling.OwnedCells{}, fmt.Errorf("read cells of appointment %s: %w", appointmentID, err)
	}
	defer rows.Close()

	var owned scheduling.OwnedCells
	for rows.Next() {
		var kind scheduling.ResourceKind
		var r scheduling.CellRange
		if err := rows.Scan(&kind, &r.From, &r.To, &r.Cells); err != nil {
			return scheduling.OwnedCells{}, fmt.Errorf("read cells of appointment %s: %w", appointmentID, err)
		}
		switch kind {
		case scheduling.ResourceBay:
			owned.Bay = &r
		case scheduling.ResourceTechnician:
			owned.Technician = &r
		}
	}
	if err := rows.Err(); err != nil {
		return scheduling.OwnedCells{}, fmt.Errorf("read cells of appointment %s: %w", appointmentID, err)
	}
	return owned, nil
}
