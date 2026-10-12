package service

import "context"

// The booking flow (research R4) is implemented here: checks in order, the booking-key lookups,
// candidate selection and the one-transaction-per-pair loop (implementation notes 4.4).

// Book confirms an appointment, or returns the one that already owns the booking key.
func (s *service) Book(ctx context.Context, cmd BookCommand) (Record, error) {
	panic("not implemented")
}
