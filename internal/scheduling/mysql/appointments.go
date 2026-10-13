package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"scheduler/internal/scheduling"
)

// appointmentColumns is the column list scanAppointment reads, in order.
const appointmentColumns = `id, tenant_id, dealership_id, customer_id, vehicle_id, service_type_id, kind,
	start_at, end_at, service_bay_id, technician_id, status, customer_requests, notes, channel,
	version, created_at, updated_at`

// InsertAppointment is the first statement of a booking attempt. Its unique index on the booking
// key (vehicle, customer, start, kind) is the idempotency guard: a duplicate key is
// scheduling.ErrDuplicateBooking, and a concurrent duplicate waits here until the first attempt
// commits or rolls back (design 5.2).
func (tx *Tx) InsertAppointment(a scheduling.Appointment) error {
	requests, err := json.Marshal(a.CustomerRequests)
	if err != nil {
		return fmt.Errorf("encode customer requests of appointment %s: %w", a.ID, err)
	}
	_, err = tx.tx.ExecContext(tx.ctx, `
		INSERT INTO appointment (`+appointmentColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TenantID, a.DealershipID, a.CustomerID, a.VehicleID, a.ServiceTypeID, a.Kind,
		a.StartAt, a.EndAt, a.ServiceBayID, a.TechnicianID, a.Status, string(requests),
		sql.NullString{String: a.Notes, Valid: a.Notes != ""}, a.Channel,
		a.Version, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert appointment %s: %w", a.ID, mapError(err))
	}
	return nil
}

// UpdateAppointment writes a transition's new state under the version check (research R1).
func (tx *Tx) UpdateAppointment(a scheduling.Appointment, expectedVersion int) error {
	panic("not implemented")
}

// DueNoShows returns the CONFIRMED appointments that start before before, oldest first.
func (s *Store) DueNoShows(ctx context.Context, before time.Time) ([]scheduling.Appointment, error) {
	panic("not implemented")
}

// Appointment loads one appointment; scheduling.ErrNotFound if there is none.
func (s *Store) Appointment(ctx context.Context, id string) (scheduling.Appointment, error) {
	a, err := scanAppointment(s.db.QueryRowContext(ctx,
		`SELECT `+appointmentColumns+` FROM appointment WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return scheduling.Appointment{}, fmt.Errorf("appointment %s: %w", id, scheduling.ErrNotFound)
	}
	if err != nil {
		return scheduling.Appointment{}, fmt.Errorf("read appointment %s: %w", id, err)
	}
	return a, nil
}

// AppointmentByBookingKey loads the appointment that owns key, in any status; false if none does.
func (s *Store) AppointmentByBookingKey(ctx context.Context, key scheduling.BookingKey) (scheduling.Appointment, bool, error) {
	a, err := scanAppointment(s.db.QueryRowContext(ctx, `
		SELECT `+appointmentColumns+` FROM appointment
		 WHERE vehicle_id = ? AND customer_id = ? AND start_at = ? AND kind = ?`,
		key.VehicleID, key.CustomerID, key.StartAt, key.Kind))
	if errors.Is(err, sql.ErrNoRows) {
		return scheduling.Appointment{}, false, nil
	}
	if err != nil {
		return scheduling.Appointment{}, false, fmt.Errorf("read appointment by booking key of vehicle %s: %w", key.VehicleID, err)
	}
	return a, true, nil
}

func scanAppointment(row *sql.Row) (scheduling.Appointment, error) {
	var a scheduling.Appointment
	var requests []byte
	var notes sql.NullString
	err := row.Scan(&a.ID, &a.TenantID, &a.DealershipID, &a.CustomerID, &a.VehicleID, &a.ServiceTypeID, &a.Kind,
		&a.StartAt, &a.EndAt, &a.ServiceBayID, &a.TechnicianID, &a.Status, &requests, &notes, &a.Channel,
		&a.Version, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return scheduling.Appointment{}, err
	}
	if err := json.Unmarshal(requests, &a.CustomerRequests); err != nil {
		return scheduling.Appointment{}, fmt.Errorf("decode customer requests: %w", err)
	}
	a.Notes = notes.String
	return a, nil
}
