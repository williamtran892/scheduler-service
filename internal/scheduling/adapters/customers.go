package adapters

import (
	"context"

	"scheduler/internal/customers"
	"scheduler/internal/scheduling"
)

// NewCustomerReader returns Scheduling's view of Customer & Vehicle over repo.
func NewCustomerReader(repo customers.Repository) scheduling.CustomerReader {
	return customerReader{repo: repo}
}

// customerReader implements scheduling.CustomerReader over customers.Repository.
type customerReader struct {
	repo customers.Repository
}

func (r customerReader) Customer(ctx context.Context, id string) (scheduling.Customer, error) {
	c, err := r.repo.Customer(ctx, id)
	if err != nil {
		return scheduling.Customer{}, translate("customer", id, err, customers.ErrNotFound)
	}
	return scheduling.Customer{ID: c.ID, TenantID: c.TenantID, DisplayName: c.DisplayName}, nil
}

func (r customerReader) Vehicle(ctx context.Context, id string) (scheduling.Vehicle, error) {
	v, err := r.repo.Vehicle(ctx, id)
	if err != nil {
		return scheduling.Vehicle{}, translate("vehicle", id, err, customers.ErrNotFound)
	}
	return scheduling.Vehicle{ID: v.ID, TenantID: v.TenantID, CustomerID: v.CustomerID,
		Registration: v.Registration, Make: v.Make, Model: v.Model}, nil
}
