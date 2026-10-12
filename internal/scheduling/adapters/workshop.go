// Package adapters implements Scheduling's reader interfaces over the upstream domains'
// repositories: the anti-corruption layer and the only place Scheduling imports another domain
// (implementation notes 2).
package adapters

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"scheduler/internal/scheduling"
	"scheduler/internal/setup"
)

//go:generate go tool mockgen -destination=mock_setup_test.go -package=adapters -mock_names=Repository=MockSetupRepository scheduler/internal/setup Repository
//go:generate go tool mockgen -destination=mock_customers_test.go -package=adapters -mock_names=Repository=MockCustomersRepository scheduler/internal/customers Repository

// NewWorkshopReader returns Scheduling's view of Workshop Setup over repo.
func NewWorkshopReader(repo setup.Repository) scheduling.WorkshopReader {
	return workshopReader{repo: repo}
}

// workshopReader implements scheduling.WorkshopReader over setup.Repository.
type workshopReader struct {
	repo setup.Repository
}

func (r workshopReader) DealershipConfig(ctx context.Context, dealershipID string) (scheduling.DealershipConfig, error) {
	d, err := r.repo.Dealership(ctx, dealershipID)
	if err != nil {
		return scheduling.DealershipConfig{}, translate("dealership", dealershipID, err, setup.ErrNotFound)
	}
	loc, err := time.LoadLocation(d.Timezone)
	if err != nil {
		return scheduling.DealershipConfig{}, fmt.Errorf("dealership %s: time zone: %w", dealershipID, err)
	}
	return scheduling.DealershipConfig{
		DealershipID:       d.ID,
		TenantID:           d.TenantID,
		Code:               d.Code,
		Name:               d.Name,
		Location:           loc,
		Grid:               scheduling.Grid{SlotMinutes: d.SlotMinutes},
		BookingHorizonDays: d.BookingHorizonDays,
	}, nil
}

func (r workshopReader) Requirement(ctx context.Context, serviceTypeID string) (scheduling.ServiceRequirement, error) {
	st, err := r.repo.ServiceType(ctx, serviceTypeID)
	if err != nil {
		return scheduling.ServiceRequirement{}, translate("service type", serviceTypeID, err, setup.ErrNotFound)
	}
	bayTypes := make([]string, len(st.BayTypes))
	for i, t := range st.BayTypes {
		bayTypes[i] = string(t)
	}
	return scheduling.ServiceRequirement{
		ServiceTypeID:   st.ID,
		TenantID:        st.TenantID,
		Code:            st.Code,
		Name:            st.Name,
		Duration:        time.Duration(st.DurationMinutes) * time.Minute,
		BufferBefore:    time.Duration(st.BufferBeforeMinutes) * time.Minute,
		BufferAfter:     time.Duration(st.BufferAfterMinutes) * time.Minute,
		RequiredSkills:  slices.Clone(st.RequiredSkills),
		AllowedBayTypes: bayTypes,
		OnlineBookable:  st.OnlineBookable,
		Active:          st.Active,
	}, nil
}

func (r workshopReader) Resources(ctx context.Context, dealershipID, localDate string) ([]scheduling.ServiceBay, []scheduling.Technician, error) {
	bays, techs, err := r.repo.ActiveResources(ctx, dealershipID, localDate)
	if err != nil {
		return nil, nil, translate("resources of dealership", dealershipID, err, setup.ErrNotFound)
	}
	outBays := make([]scheduling.ServiceBay, len(bays))
	for i, b := range bays {
		outBays[i] = bayOf(b)
	}
	outTechs := make([]scheduling.Technician, len(techs))
	for i, t := range techs {
		outTechs[i] = technicianOf(t)
	}
	return outBays, outTechs, nil
}

func (r workshopReader) Bay(ctx context.Context, id string) (scheduling.ServiceBay, error) {
	b, err := r.repo.Bay(ctx, id)
	if err != nil {
		return scheduling.ServiceBay{}, translate("bay", id, err, setup.ErrNotFound)
	}
	return bayOf(b), nil
}

func (r workshopReader) Technician(ctx context.Context, id string) (scheduling.Technician, error) {
	t, err := r.repo.Technician(ctx, id)
	if err != nil {
		return scheduling.Technician{}, translate("technician", id, err, setup.ErrNotFound)
	}
	return technicianOf(t), nil
}

func bayOf(b setup.ServiceBay) scheduling.ServiceBay {
	return scheduling.ServiceBay{ID: b.ID, Name: b.Name, Type: string(b.Type)}
}

func technicianOf(t setup.Technician) scheduling.Technician {
	return scheduling.Technician{ID: t.ID, Name: t.DisplayName, Skills: slices.Clone(t.Skills)}
}

// translate gives an upstream error Scheduling's vocabulary: the upstream's "not found" becomes
// scheduling.ErrNotFound, anything else keeps its cause with context.
func translate(what, id string, err, upstreamNotFound error) error {
	if errors.Is(err, upstreamNotFound) {
		return fmt.Errorf("%s %s: %w", what, id, scheduling.ErrNotFound)
	}
	return fmt.Errorf("read %s %s: %w", what, id, err)
}
