package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"scheduler/internal/scheduling"
)

// ClaimPair is the only code that marks cells BOOKED. It claims the bay run, then the technician
// run, always in that order, so every claimer takes its locks in the same order and plain claims
// cannot deadlock each other (design 11.3; notes 4.5).
func (tx *tx) ClaimPair(dealershipID, bayID string, bayRun scheduling.CellRun,
	techID string, techRun scheduling.CellRun, appointmentID string) error {
	if err := tx.claimRun(dealershipID, scheduling.ResourceBay, bayID, bayRun, appointmentID, scheduling.ErrBayNotOpen); err != nil {
		return err
	}
	return tx.claimRun(dealershipID, scheduling.ResourceTechnician, techID, techRun, appointmentID, scheduling.ErrTechnicianNotWorking)
}

// ReleaseCells frees every cell the appointment owns that keep does not keep and returns how many
// it freed. It locks the appointment's cells, filters them in Go with Keep.Keeps, and frees the
// rest with one UPDATE per resource.
func (tx *tx) ReleaseCells(appointmentID string, keep scheduling.Keep) (int, error) {
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

// cell is one time_slot row as the failed claim reads it back.
type cell struct {
	start         time.Time
	status        scheduling.CellStatus
	appointmentID sql.NullString
}

// claimRun marks the cells of one run BOOKED with one conditional UPDATE:
//
//	UPDATE time_slot SET status = BOOKED, appointment_id = ?, version = version + 1
//	 WHERE <the run's resource and range> AND (status = FREE OR appointment_id = ?)
//
// A cell the appointment already owns counts as claimable, so a reschedule can claim runs that
// overlap its own cells. The run is claimed when the UPDATE affects exactly run.Cells rows.
//
// Why that is safe without a read before it:
//   - An UPDATE is a locking current read. It waits for the lock on each row of the range, in
//     slot_start order along the primary key, and tests its WHERE clause on the latest committed
//     row once it holds the lock, never on a snapshot. Two claimers of overlapping cells therefore
//     serialise on the first shared row, and the loser, once the winner has committed, finds those
//     cells BOOKED by somebody else: its UPDATE matches fewer rows than the run has cells, and
//     the claim is refused. At REPEATABLE READ the UPDATE keeps the locks of every row it
//     scanned, matched or not, until the transaction ends.
//   - The order of the locks is unchanged: the bay run, then the technician run (ClaimPair), and
//     ascending slot_start within a run, so the argument that plain claims cannot deadlock each
//     other holds as before.
//   - version = version + 1 always changes a matched row, so RowsAffected counts every matched
//     cell, even one whose status and appointment_id are already the claimant's. MySQL counts
//     changed rows unless the connection sets clientFoundRows, which the DSN must not do: it would
//     count matched rows, and the check below would mean nothing.
//
// A claim that affects fewer rows than the run needs may already have changed the rows it did
// match, so the transaction must be rolled back; every caller returns the error and InTx rolls
// back. Only on that failure path does claimRun read, to name the failure (see refusal).
func (tx *tx) claimRun(dealershipID string, kind scheduling.ResourceKind, id string, run scheduling.CellRun,
	appointmentID string, missing error) error {
	res, err := tx.tx.ExecContext(tx.ctx, `
		UPDATE time_slot SET status = ?, appointment_id = ?, version = version + 1
		 WHERE resource_kind = ? AND resource_id = ? AND dealership_id = ? AND slot_start BETWEEN ? AND ?
		   AND (status = ? OR appointment_id = ?)`,
		scheduling.CellBooked, appointmentID, kind, id, dealershipID, run.First, run.Last(),
		scheduling.CellFree, appointmentID)
	if err != nil {
		return fmt.Errorf("book %s %s cells: %w", kind, id, mapError(err))
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("book %s %s cells: %w", kind, id, err)
	}
	if claimed == int64(run.Cells) {
		return nil
	}
	return tx.refusal(dealershipID, kind, id, run, appointmentID, missing, claimed)
}

// refusal says why a claim of run affected claimed rows instead of run.Cells. It runs only on that
// failure path, inside the claim's transaction, which still holds the locks of the range: it
// reads the range with FOR UPDATE, so nothing changes under it, and that read also sees the rows
// the failed UPDATE changed, now owned by the claimant, which it ignores like any owned cell.
// Fewer rows than the run needs is the missing error, with the counts; a row BOOKED by another
// appointment is scheduling.ErrSlotTaken, naming the cell and the appointment.
func (tx *tx) refusal(dealershipID string, kind scheduling.ResourceKind, id string, run scheduling.CellRun,
	appointmentID string, missing error, claimed int64) error {
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
	cells, err := scanCells(rows) // reads every row, so the connection is free again
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
	// Every cell exists and is claimable, yet the UPDATE did not match exactly run.Cells of them:
	// cells off the run's grid inside its range. Never answer success for it.
	return fmt.Errorf("book %s %s cells: claimed %d rows for a run of %d cells and found no missing or taken cell",
		kind, id, claimed, run.Cells)
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

// GetAvailableResources returns, of the given bays and technicians only, the ids of the bays whose
// every cell of bayRun is free and of the technicians whose every cell of techRun is free. One
// query counts, per resource, the free cells inside its own run.
//
// A cell is free when its status is FREE or owner owns it, so a reschedule sees the appointment's
// own time as available (design 5.4). owner is "" for a new booking, which no appointment id
// equals. A FREE cell has a NULL appointment_id (ck_slot_owner), so the owner comparison is NULL
// for it and the status alone makes it free; a BOOKED cell is free only when owner owns it.
//
// A run is held when the count of its free cells equals its cell count: the primary key makes the
// cells of a resource unique, so no cell is counted twice, and a cell missing from the ledger or
// booked by somebody else leaves the count short. A resource with no free cell in its run has no
// row and is not available. The result follows the order the rows come back in.
func (s *store) GetAvailableResources(ctx context.Context, dealershipID string, bayIDs []string, bayRun scheduling.CellRun,
	techIDs []string, techRun scheduling.CellRun, owner string) (availableBays, availableTechs []string, err error) {
	ids := slices.Concat(bayIDs, techIDs)
	if len(ids) == 0 {
		return nil, nil, nil
	}
	args := []any{dealershipID, scheduling.CellFree, owner,
		scheduling.ResourceBay, bayRun.First, bayRun.Last(),
		scheduling.ResourceTechnician, techRun.First, techRun.Last()}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT resource_kind, resource_id, COUNT(*)
		  FROM time_slot
		 WHERE dealership_id = ? AND (status = ? OR appointment_id = ?)
		   AND ((resource_kind = ? AND slot_start BETWEEN ? AND ?)
		     OR (resource_kind = ? AND slot_start BETWEEN ? AND ?))
		   AND resource_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)
		 GROUP BY resource_kind, resource_id`, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("available resources of dealership %s: %w", dealershipID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind scheduling.ResourceKind
		var id string
		var free int
		if err := rows.Scan(&kind, &id, &free); err != nil {
			return nil, nil, fmt.Errorf("available resources of dealership %s: %w", dealershipID, err)
		}
		switch {
		case kind == scheduling.ResourceBay && free == bayRun.Cells:
			availableBays = append(availableBays, id)
		case kind == scheduling.ResourceTechnician && free == techRun.Cells:
			availableTechs = append(availableTechs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("available resources of dealership %s: %w", dealershipID, err)
	}
	return availableBays, availableTechs, nil
}

// GetResourceLoad returns, of the given resources only, the number of BOOKED cells each has in the
// day's UTC window: the load that orders the candidate pairs. Cells owned by owner are not
// counted, so a reschedule does not weigh the appointment's own time against its resources;
// owner is "" for a new booking. A BOOKED cell always has an appointment_id (ck_slot_owner), so
// the comparison with owner is never NULL. A resource with no such cell has no row and is absent
// from the result, which reads as a load of 0.
func (s *store) GetResourceLoad(ctx context.Context, dealershipID string, day scheduling.DayWindow,
	resourceIDs []string, owner string) (scheduling.Loads, error) {
	loads := scheduling.Loads{}
	if len(resourceIDs) == 0 {
		return loads, nil
	}
	args := []any{dealershipID, day.From, day.To, scheduling.CellBooked, owner}
	for _, id := range resourceIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT resource_id, COUNT(*)
		  FROM time_slot
		 WHERE dealership_id = ? AND slot_start >= ? AND slot_start < ?
		   AND status = ? AND appointment_id <> ?
		   AND resource_id IN (?`+strings.Repeat(", ?", len(resourceIDs)-1)+`)
		 GROUP BY resource_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("resource load of dealership %s on %s: %w", dealershipID, day.LocalDate, err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var load int
		if err := rows.Scan(&id, &load); err != nil {
			return nil, fmt.Errorf("resource load of dealership %s on %s: %w", dealershipID, day.LocalDate, err)
		}
		loads[id] = load
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resource load of dealership %s on %s: %w", dealershipID, day.LocalDate, err)
	}
	return loads, nil
}

// WorkingCellCounts returns, for each of the two runs, how many of its cells the dealership has a
// ledger cell for, on any resource and in any status: the dealership's working time (the ledger
// is the working time in this release). It counts the distinct cell starts between the first and
// the last cell of each run, in one query over the range that covers both.
func (s *store) WorkingCellCounts(ctx context.Context, dealershipID string, bayRun, techRun scheduling.CellRun) (bayCells, techCells int, err error) {
	from, to := bayRun.First, bayRun.End()
	if techRun.First.Before(from) {
		from = techRun.First
	}
	if techRun.End().After(to) {
		to = techRun.End()
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT CASE WHEN slot_start BETWEEN ? AND ? THEN slot_start END),
		       COUNT(DISTINCT CASE WHEN slot_start BETWEEN ? AND ? THEN slot_start END)
		  FROM time_slot
		 WHERE dealership_id = ? AND slot_start >= ? AND slot_start < ?`,
		bayRun.First, bayRun.Last(), techRun.First, techRun.Last(), dealershipID, from, to).Scan(&bayCells, &techCells)
	if err != nil {
		return 0, 0, fmt.Errorf("count working cells of dealership %s: %w", dealershipID, err)
	}
	return bayCells, techCells, nil
}

// OwnedCells returns the cell ranges an appointment owns on its bay and on its technician, as
// half-open intervals (contract CellRange). The end of a range is its last cell plus one cell,
// so this query joins dealership.slot_minutes: the one column this package reads from another
// domain's table, through time_slot's own foreign key.
func (s *store) OwnedCells(ctx context.Context, appointmentID string) (scheduling.OwnedCells, error) {
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
