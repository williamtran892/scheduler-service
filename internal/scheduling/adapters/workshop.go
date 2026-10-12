// Package adapters implements Scheduling's reader interfaces over the upstream domains'
// repositories: the anti-corruption layer and the only place Scheduling imports another domain
// (implementation notes 2).
package adapters

import (
	"context"

	"scheduler/internal/scheduling"
	"scheduler/internal/setup"
)

//go:generate go tool mockgen -destination=mock_setup_test.go -package=adapters -mock_names=Repository=MockSetupRepository scheduler/internal/setup Repository
//go:generate go tool mockgen -destination=mock_customers_test.go -package=adapters -mock_names=Repository=MockCustomersRepository scheduler/internal/customers Repository

// NewWorkshopReader returns Scheduling's view of Workshop Setup over repo.
func NewWorkshopReader(repo setup.Repository) scheduling.WorkshopReader { return workshopReader{} }

// workshopReader implements scheduling.WorkshopReader over setup.Repository.
type workshopReader struct{}

func (workshopReader) DealershipConfig(ctx context.Context, dealershipID string) (scheduling.DealershipConfig, error) {
	panic("not implemented")
}

func (workshopReader) Requirement(ctx context.Context, serviceTypeID string) (scheduling.ServiceRequirement, error) {
	panic("not implemented")
}

func (workshopReader) Resources(ctx context.Context, dealershipID, localDate string) ([]scheduling.ServiceBay, []scheduling.Technician, error) {
	panic("not implemented")
}

func (workshopReader) Bay(ctx context.Context, id string) (scheduling.ServiceBay, error) {
	panic("not implemented")
}

func (workshopReader) Technician(ctx context.Context, id string) (scheduling.Technician, error) {
	panic("not implemented")
}
