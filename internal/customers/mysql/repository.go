// Package mysql is Customer & Vehicle's read access on database/sql: customers and vehicles.
// Its Repository implements scheduling.CustomerReader and returns Scheduling's own types.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"scheduler/internal/scheduling"
)

// Repository reads the customer and vehicle tables in Scheduling's terms. It implements
// scheduling.CustomerReader.
type Repository struct {
	db *sql.DB
}

var _ scheduling.CustomerReader = (*Repository)(nil)

// New returns a repository over db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Customer(ctx context.Context, id string) (scheduling.Customer, error) {
	var c scheduling.Customer
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, display_name FROM customer WHERE id = ?`, id).
		Scan(&c.ID, &c.TenantID, &c.DisplayName)
	if err != nil {
		return scheduling.Customer{}, readError("customer", id, err)
	}
	return c, nil
}

func (r *Repository) Vehicle(ctx context.Context, id string) (scheduling.Vehicle, error) {
	var v scheduling.Vehicle
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, customer_id, registration, COALESCE(make, ''), COALESCE(model, '')
		  FROM vehicle WHERE id = ?`, id).
		Scan(&v.ID, &v.TenantID, &v.CustomerID, &v.Registration, &v.Make, &v.Model)
	if err != nil {
		return scheduling.Vehicle{}, readError("vehicle", id, err)
	}
	return v, nil
}

// readError gives a failed read of what/id its context: no row is scheduling.ErrNotFound.
func readError(what, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s: %w", what, id, scheduling.ErrNotFound)
	}
	return fmt.Errorf("read %s %s: %w", what, id, err)
}
