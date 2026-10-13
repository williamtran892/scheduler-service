package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"scheduler/internal/scheduling"
)

// AppendEvent appends an appointment_event row inside the transaction. payload is
// stored as JSON. An empty channel, the no-show sweeper's, is stored as NULL.
func (tx *tx) AppendEvent(appointmentID, eventType, actor string, channel scheduling.Channel, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s event of appointment %s: %w", eventType, appointmentID, err)
	}
	_, err = tx.tx.ExecContext(tx.ctx, `
		INSERT INTO appointment_event (appointment_id, event_type, payload, actor, channel)
		VALUES (?, ?, ?, ?, ?)`, appointmentID, eventType, string(body), actor,
		sql.NullString{String: string(channel), Valid: channel != ""})
	if err != nil {
		return fmt.Errorf("append %s event of appointment %s: %w", eventType, appointmentID, mapError(err))
	}
	return nil
}

// Events returns an appointment's audit trail, oldest first.
func (s *store) Events(ctx context.Context, appointmentID string) ([]scheduling.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_type, actor, channel, occurred_at, payload
		  FROM appointment_event WHERE appointment_id = ? ORDER BY occurred_at, id`, appointmentID)
	if err != nil {
		return nil, fmt.Errorf("read events of appointment %s: %w", appointmentID, err)
	}
	defer rows.Close()

	var events []scheduling.Event
	for rows.Next() {
		var e scheduling.Event
		var channel sql.NullString
		if err := rows.Scan(&e.ID, &e.Type, &e.Actor, &channel, &e.OccurredAt, &e.Payload); err != nil {
			return nil, fmt.Errorf("read events of appointment %s: %w", appointmentID, err)
		}
		if channel.Valid {
			if e.Channel, err = scheduling.ParseChannel(channel.String); err != nil {
				return nil, fmt.Errorf("read events of appointment %s: %w", appointmentID, err)
			}
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events of appointment %s: %w", appointmentID, err)
	}
	return events, nil
}
