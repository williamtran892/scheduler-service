// Package scheduling is the Scheduling domain root: the ledger's grid algebra, the booking
// types, the candidate-pair search and the data-access contract (Store, Tx) together with what
// Scheduling needs from other domains (WorkshopReader, CustomerReader). It imports only the
// standard library (implementation notes 2).
package scheduling

import "time"

// Grid is the dealership's booking grid: cells of SlotMinutes anchored to the top of the UTC
// hour (design 11.1).
type Grid struct {
	SlotMinutes int
}

// Slot is the length of one cell.
func (g Grid) Slot() time.Duration { panic("not implemented") }

// Align returns t if it starts a cell (a whole multiple of the slot from the top of the UTC
// hour, zero seconds), otherwise ErrOffGrid.
func (g Grid) Align(t time.Time) (time.Time, error) { panic("not implemented") }

// CellsFor returns the number of cells needed to cover d, rounded up.
func (g Grid) CellsFor(d time.Duration) int { panic("not implemented") }

// CellRun is a run of consecutive cells on one resource.
type CellRun struct {
	First time.Time
	Cells int
	Slot  time.Duration
}

// Last is the start of the run's last cell.
func (r CellRun) Last() time.Time { panic("not implemented") }

// End is the end of the run's last cell (exclusive).
func (r CellRun) End() time.Time { panic("not implemented") }

// Within reports whether the whole run lies inside the island [From, To).
func (r CellRun) Within(i Island) bool { panic("not implemented") }
