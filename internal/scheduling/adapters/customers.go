package adapters

import (
	"context"

	"scheduler/internal/customers"
	"scheduler/internal/scheduling"
)

// NewCustomerReader returns Scheduling's view of Customer & Vehicle over repo.
func NewCustomerReader(repo customers.Repository) scheduling.CustomerReader { return customerReader{} }

// customerReader implements scheduling.CustomerReader over customers.Repository.
type customerReader struct{}

func (customerReader) Customer(ctx context.Context, id string) (scheduling.Customer, error) {
	panic("not implemented")
}

func (customerReader) Vehicle(ctx context.Context, id string) (scheduling.Vehicle, error) {
	panic("not implemented")
}
