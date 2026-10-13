package service

import (
	"context"
)

// Reschedule moves a CONFIRMED appointment, reusing booking's plan with the appointment as the
// owner whose cells count as free (design 5.4; research R4). Not implemented yet.
func (s *service) Reschedule(ctx context.Context, cmd RescheduleCommand) (Record, error) {
	panic("not implemented")
}
