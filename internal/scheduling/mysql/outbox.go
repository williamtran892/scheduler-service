package mysql

import (
	"encoding/json"
	"fmt"
)

// AppendOutbox appends an outbox row inside the booking transaction, unpublished
// (published_at stays NULL; nothing relays it in this release). payload is stored as JSON.
func (tx *Tx) AppendOutbox(aggregateType, aggregateID, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s outbox row of %s %s: %w", eventType, aggregateType, aggregateID, err)
	}
	_, err = tx.tx.ExecContext(tx.ctx, `
		INSERT INTO outbox (aggregate_type, aggregate_id, event_type, payload)
		VALUES (?, ?, ?, ?)`, aggregateType, aggregateID, eventType, string(body))
	if err != nil {
		return fmt.Errorf("append %s outbox row of %s %s: %w", eventType, aggregateType, aggregateID, mapError(err))
	}
	return nil
}
