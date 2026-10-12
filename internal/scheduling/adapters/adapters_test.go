package adapters

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"scheduler/internal/customers"
	"scheduler/internal/scheduling"
	"scheduler/internal/setup"
)

var ctx = context.Background()

func TestWorkshopReader_RequirementTranslatesTheServiceType(t *testing.T) {
	repo := NewMockSetupRepository(gomock.NewController(t))
	repo.EXPECT().ServiceType(gomock.Any(), "major").Return(setup.ServiceType{
		ID: "major", TenantID: "tenant", Code: "MAJOR", Name: "Major Service",
		DurationMinutes: 90, BufferBeforeMinutes: 15, BufferAfterMinutes: 10,
		OnlineBookable: false, Active: true,
		RequiredSkills: []string{"GENERAL_SERVICE"}, BayTypes: []setup.BayType{setup.BayGeneral, setup.BayMOT},
	}, nil)

	got, err := NewWorkshopReader(repo).Requirement(ctx, "major")

	want := scheduling.ServiceRequirement{
		ServiceTypeID: "major", TenantID: "tenant", Code: "MAJOR", Name: "Major Service",
		Duration: 90 * time.Minute, BufferBefore: 15 * time.Minute, BufferAfter: 10 * time.Minute,
		RequiredSkills: []string{"GENERAL_SERVICE"}, AllowedBayTypes: []string{"GENERAL", "MOT"},
		OnlineBookable: false, Active: true,
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Requirement = %+v, %v\nwant %+v", got, err, want)
	}
}

func TestWorkshopReader_DealershipConfigLoadsTheTimeZoneAndGrid(t *testing.T) {
	repo := NewMockSetupRepository(gomock.NewController(t))
	repo.EXPECT().Dealership(gomock.Any(), "man01").Return(setup.Dealership{
		ID: "man01", TenantID: "tenant", Code: "MAN01", Name: "Manchester Central",
		Timezone: "Europe/London", SlotMinutes: 15, BookingHorizonDays: 28,
	}, nil)

	got, err := NewWorkshopReader(repo).DealershipConfig(ctx, "man01")

	if err != nil {
		t.Fatalf("DealershipConfig: %v", err)
	}
	if got.DealershipID != "man01" || got.TenantID != "tenant" || got.Code != "MAN01" || got.Name != "Manchester Central" ||
		got.Grid.SlotMinutes != 15 || got.BookingHorizonDays != 28 || got.Location == nil || got.Location.String() != "Europe/London" {
		t.Errorf("DealershipConfig = %+v", got)
	}
}

func TestWorkshopReader_UnknownTimeZoneIsAnError(t *testing.T) {
	repo := NewMockSetupRepository(gomock.NewController(t))
	repo.EXPECT().Dealership(gomock.Any(), "x").Return(setup.Dealership{ID: "x", Timezone: "Mars/Olympus", SlotMinutes: 15}, nil)

	if _, err := NewWorkshopReader(repo).DealershipConfig(ctx, "x"); err == nil {
		t.Error("DealershipConfig accepted an unknown time zone")
	}
}

func TestWorkshopReader_ResourcesTranslateBaysAndTechnicians(t *testing.T) {
	repo := NewMockSetupRepository(gomock.NewController(t))
	repo.EXPECT().ActiveResources(gomock.Any(), "man01", "2026-10-20").Return(
		[]setup.ServiceBay{{ID: "bay-1", Name: "Bay 1", Type: setup.BayGeneral, Status: setup.BayActive}},
		[]setup.Technician{{ID: "sam", DisplayName: "Sam", Status: setup.TechnicianActive, Skills: []string{"GENERAL_SERVICE", "EV_HV_L3"}}},
		nil)

	bays, techs, err := NewWorkshopReader(repo).Resources(ctx, "man01", "2026-10-20")

	if err != nil ||
		!reflect.DeepEqual(bays, []scheduling.ServiceBay{{ID: "bay-1", Name: "Bay 1", Type: "GENERAL"}}) ||
		!reflect.DeepEqual(techs, []scheduling.Technician{{ID: "sam", Name: "Sam", Skills: []string{"GENERAL_SERVICE", "EV_HV_L3"}}}) {
		t.Errorf("Resources = %+v, %+v, %v", bays, techs, err)
	}
}

func TestWorkshopReader_BayAndTechnicianForTheRecord(t *testing.T) {
	repo := NewMockSetupRepository(gomock.NewController(t))
	repo.EXPECT().Bay(gomock.Any(), "bay-3").Return(setup.ServiceBay{ID: "bay-3", Name: "Bay 3", Type: setup.BayEVHV, Status: setup.BayActive}, nil)
	repo.EXPECT().Technician(gomock.Any(), "sam").Return(setup.Technician{ID: "sam", DisplayName: "Sam", Status: setup.TechnicianActive}, nil)
	r := NewWorkshopReader(repo)

	bay, err1 := r.Bay(ctx, "bay-3")
	tech, err2 := r.Technician(ctx, "sam")

	if err1 != nil || err2 != nil || bay != (scheduling.ServiceBay{ID: "bay-3", Name: "Bay 3", Type: "EV_HV"}) || tech.ID != "sam" || tech.Name != "Sam" {
		t.Errorf("Bay = %+v %v, Technician = %+v %v", bay, err1, tech, err2)
	}
}

func TestWorkshopReader_NotFoundBecomesSchedulingNotFound(t *testing.T) {
	repo := NewMockSetupRepository(gomock.NewController(t))
	notFound := fmt.Errorf("select: %w", setup.ErrNotFound)
	repo.EXPECT().Dealership(gomock.Any(), gomock.Any()).Return(setup.Dealership{}, notFound)
	repo.EXPECT().ServiceType(gomock.Any(), gomock.Any()).Return(setup.ServiceType{}, notFound)
	repo.EXPECT().Bay(gomock.Any(), gomock.Any()).Return(setup.ServiceBay{}, notFound)
	repo.EXPECT().Technician(gomock.Any(), gomock.Any()).Return(setup.Technician{}, notFound)
	r := NewWorkshopReader(repo)

	_, e1 := r.DealershipConfig(ctx, "x")
	_, e2 := r.Requirement(ctx, "x")
	_, e3 := r.Bay(ctx, "x")
	_, e4 := r.Technician(ctx, "x")
	for i, err := range []error{e1, e2, e3, e4} {
		if !errors.Is(err, scheduling.ErrNotFound) {
			t.Errorf("call %d: error %v, want scheduling.ErrNotFound", i+1, err)
		}
	}
}

func TestCustomerReader_TranslatesCustomersAndVehicles(t *testing.T) {
	repo := NewMockCustomersRepository(gomock.NewController(t))
	repo.EXPECT().Customer(gomock.Any(), "alice").Return(customers.Customer{ID: "alice", TenantID: "tenant", DisplayName: "Alice", Email: "a@example.com"}, nil)
	repo.EXPECT().Vehicle(gomock.Any(), "yk21").Return(customers.Vehicle{ID: "yk21", TenantID: "tenant", CustomerID: "alice",
		Registration: "YK21 ABC", Make: "Volkswagen", Model: "ID.4"}, nil)
	r := NewCustomerReader(repo)

	c, err1 := r.Customer(ctx, "alice")
	v, err2 := r.Vehicle(ctx, "yk21")

	if err1 != nil || c != (scheduling.Customer{ID: "alice", TenantID: "tenant", DisplayName: "Alice"}) {
		t.Errorf("Customer = %+v, %v", c, err1)
	}
	if err2 != nil || v != (scheduling.Vehicle{ID: "yk21", TenantID: "tenant", CustomerID: "alice", Registration: "YK21 ABC", Make: "Volkswagen", Model: "ID.4"}) {
		t.Errorf("Vehicle = %+v, %v", v, err2)
	}
}

func TestCustomerReader_NotFoundBecomesSchedulingNotFound(t *testing.T) {
	repo := NewMockCustomersRepository(gomock.NewController(t))
	repo.EXPECT().Customer(gomock.Any(), gomock.Any()).Return(customers.Customer{}, customers.ErrNotFound)
	repo.EXPECT().Vehicle(gomock.Any(), gomock.Any()).Return(customers.Vehicle{}, customers.ErrNotFound)
	r := NewCustomerReader(repo)

	_, e1 := r.Customer(ctx, "x")
	_, e2 := r.Vehicle(ctx, "x")
	if !errors.Is(e1, scheduling.ErrNotFound) || !errors.Is(e2, scheduling.ErrNotFound) {
		t.Errorf("errors = %v, %v; want scheduling.ErrNotFound", e1, e2)
	}
}
