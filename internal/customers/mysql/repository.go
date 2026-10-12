// Package mysql is Customer & Vehicle's read access on database/sql: customers and vehicles.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"scheduler/internal/customers"
)

// Repository reads the customer and vehicle tables. It implements customers.Repository.
type Repository struct {
	db *sql.DB
}

var _ customers.Repository = (*Repository)(nil)

// New returns a repository over db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Customer(ctx context.Context, id string) (customers.Customer, error) {
	var c customers.Customer
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, display_name, COALESCE(email, ''), COALESCE(phone, '')
		  FROM customer WHERE id = ?`, id).
		Scan(&c.ID, &c.TenantID, &c.DisplayName, &c.Email, &c.Phone)
	if err != nil {
		return customers.Customer{}, readError("customer", id, err)
	}
	return c, nil
}

func (r *Repository) Vehicle(ctx context.Context, id string) (customers.Vehicle, error) {
	var v customers.Vehicle
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, customer_id, registration, COALESCE(make, ''), COALESCE(model, '')
		  FROM vehicle WHERE id = ?`, id).
		Scan(&v.ID, &v.TenantID, &v.CustomerID, &v.Registration, &v.Make, &v.Model)
	if err != nil {
		return customers.Vehicle{}, readError("vehicle", id, err)
	}
	return v, nil
}

// readError gives a failed read of what/id its context: no row is customers.ErrNotFound.
func readError(what, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s: %w", what, id, customers.ErrNotFound)
	}
	return fmt.Errorf("read %s %s: %w", what, id, err)
}
