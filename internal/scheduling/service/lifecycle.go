package service

import (
	"context"
)

// The lifecycle transitions after booking (design 4.3; research R1, R5, R6, R8): one transaction
// each, the version-guarded update of the appointment first, then the ledger change, then the
// audit event and the outbox row. Not implemented yet.

// Cancel releases every cell of a CONFIRMED or CHECKED_IN appointment.
func (s *service) Cancel(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}

// CheckIn checks a CONFIRMED appointment in, or a NO_SHOW in as a late arrival.
func (s *service) CheckIn(ctx context.Context, cmd CheckInCommand) (Record, error) {
	panic("not implemented")
}

// Start records the actual start of a CHECKED_IN appointment.
func (s *service) Start(ctx context.Context, cmd StartCommand) (Record, error) {
	panic("not implemented")
}

// Complete records the actual end and releases the cells from the current grid cell on.
func (s *service) Complete(ctx context.Context, cmd CompleteCommand) (Record, error) {
	panic("not implemented")
}

// MarkNoShow marks a CONFIRMED appointment NO_SHOW and releases every cell.
func (s *service) MarkNoShow(ctx context.Context, cmd TransitionCommand) (Record, error) {
	panic("not implemented")
}
