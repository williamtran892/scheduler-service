// Package customers is the Customer & Vehicle domain root: snapshots of DMS data. In this
// release it is read-only; its rows come from the fixture. It imports only the standard
// library (implementation notes 2).
package customers

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("customers: not found")

// Customer is one row of customer.
type Customer struct {
	ID          string
	TenantID    string
	DisplayName string
	Email       string
	Phone       string
}

// Vehicle is one row of vehicle.
type Vehicle struct {
	ID           string
	TenantID     string
	CustomerID   string
	Registration string
	Make         string
	Model        string
}

// Repository is Customer & Vehicle's read access; customers/mysql implements it.
type Repository interface {
	Customer(ctx context.Context, id string) (Customer, error)
	Vehicle(ctx context.Context, id string) (Vehicle, error)
}
