package setup

import "context"

// Repository is Workshop Setup's read access; setup/mysql implements it.
type Repository interface {
	Dealership(ctx context.Context, id string) (Dealership, error)
	ServiceType(ctx context.Context, id string) (ServiceType, error)
	// ActiveResources returns the ACTIVE bays and ACTIVE technicians of a dealership, each
	// technician with the skills valid on onDate (YYYY-MM-DD): valid_from <= date and valid_to
	// null or >= date (design 11.4).
	ActiveResources(ctx context.Context, dealershipID, onDate string) ([]ServiceBay, []Technician, error)
	Bay(ctx context.Context, id string) (ServiceBay, error)
	Technician(ctx context.Context, id string) (Technician, error)
}
