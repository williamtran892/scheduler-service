package scheduling

import "time"

// Key returns the appointment's booking key.
func (a Appointment) Key() BookingKey {
	return BookingKey{VehicleID: a.VehicleID, CustomerID: a.CustomerID, StartAt: a.StartAt, Kind: a.Kind}
}

// Keeps reports whether the cell of kind and resourceID starting at slotStart stays with the
// appointment.
func (k Keep) Keeps(kind ResourceKind, resourceID string, slotStart time.Time) bool {
	if !k.Before.IsZero() && slotStart.Before(k.Before) {
		return true
	}
	switch kind {
	case ResourceBay:
		return k.Bay.holds(resourceID, slotStart)
	case ResourceTechnician:
		return k.Technician.holds(resourceID, slotStart)
	}
	return false
}

// holds reports whether the cell starting at slotStart on resourceID lies inside the run; a nil
// run holds nothing.
func (r *ResourceRun) holds(resourceID string, slotStart time.Time) bool {
	return r != nil && r.ResourceID == resourceID &&
		!slotStart.Before(r.Run.First) && slotStart.Before(r.Run.End())
}
