package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"scheduler/internal/setup"
)

// Repository reads Workshop Setup's tables. It implements setup.Repository.
type Repository struct {
	db *sql.DB
}

var _ setup.Repository = (*Repository)(nil)

// New returns a repository over db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Dealership(ctx context.Context, id string) (setup.Dealership, error) {
	var d setup.Dealership
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, timezone, slot_minutes, booking_horizon_days
		  FROM dealership WHERE id = ?`, id).
		Scan(&d.ID, &d.TenantID, &d.Code, &d.Name, &d.Timezone, &d.SlotMinutes, &d.BookingHorizonDays)
	if err != nil {
		return setup.Dealership{}, readError("dealership", id, err)
	}
	return d, nil
}

func (r *Repository) ServiceType(ctx context.Context, id string) (setup.ServiceType, error) {
	var st setup.ServiceType
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, duration_minutes, buffer_before_minutes, buffer_after_minutes,
		       online_bookable, active
		  FROM service_type WHERE id = ?`, id).
		Scan(&st.ID, &st.TenantID, &st.Code, &st.Name, &st.DurationMinutes, &st.BufferBeforeMinutes,
			&st.BufferAfterMinutes, &st.OnlineBookable, &st.Active)
	if err != nil {
		return setup.ServiceType{}, readError("service type", id, err)
	}

	if st.RequiredSkills, err = r.column(ctx, `
		SELECT s.code FROM service_type_skill sts JOIN skill s ON s.id = sts.skill_id
		 WHERE sts.service_type_id = ? ORDER BY s.code`, id); err != nil {
		return setup.ServiceType{}, readError("skills of service type", id, err)
	}
	codes, err := r.column(ctx, `
		SELECT bay_type FROM service_type_bay_type WHERE service_type_id = ? ORDER BY bay_type`, id)
	if err != nil {
		return setup.ServiceType{}, readError("bay types of service type", id, err)
	}
	for _, c := range codes {
		t, err := setup.ParseBayType(c)
		if err != nil {
			return setup.ServiceType{}, readError("bay types of service type", id, err)
		}
		st.BayTypes = append(st.BayTypes, t)
	}
	return st, nil
}

func (r *Repository) ActiveResources(ctx context.Context, dealershipID, onDate string) ([]setup.ServiceBay, []setup.Technician, error) {
	bays, err := r.activeBays(ctx, dealershipID)
	if err != nil {
		return nil, nil, readError("bays of dealership", dealershipID, err)
	}
	techs, err := r.activeTechnicians(ctx, dealershipID, onDate)
	if err != nil {
		return nil, nil, readError("technicians of dealership", dealershipID, err)
	}
	return bays, techs, nil
}

func (r *Repository) activeBays(ctx context.Context, dealershipID string) ([]setup.ServiceBay, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, dealership_id, name, type, status FROM service_bay
		 WHERE dealership_id = ? AND status = ? ORDER BY name`, dealershipID, setup.BayActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bays []setup.ServiceBay
	for rows.Next() {
		var b setup.ServiceBay
		if err := rows.Scan(&b.ID, &b.DealershipID, &b.Name, &b.Type, &b.Status); err != nil {
			return nil, err
		}
		bays = append(bays, b)
	}
	return bays, rows.Err()
}

// activeTechnicians returns the ACTIVE technicians with the skills valid on onDate: valid_from
// is not after the date and valid_to is null or not before it (design 11.4).
func (r *Repository) activeTechnicians(ctx context.Context, dealershipID, onDate string) ([]setup.Technician, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, dealership_id, display_name, status FROM technician
		 WHERE dealership_id = ? AND status = ? ORDER BY display_name`, dealershipID, setup.TechnicianActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var techs []setup.Technician
	index := map[string]int{} // technician id -> position in techs
	for rows.Next() {
		var t setup.Technician
		if err := rows.Scan(&t.ID, &t.DealershipID, &t.DisplayName, &t.Status); err != nil {
			return nil, err
		}
		index[t.ID] = len(techs)
		techs = append(techs, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	skills, err := r.db.QueryContext(ctx, `
		SELECT ts.technician_id, s.code
		  FROM technician_skill ts
		  JOIN skill s ON s.id = ts.skill_id
		  JOIN technician t ON t.id = ts.technician_id
		 WHERE t.dealership_id = ? AND t.status = ?
		   AND ts.valid_from <= ? AND (ts.valid_to IS NULL OR ts.valid_to >= ?)
		 ORDER BY s.code`, dealershipID, setup.TechnicianActive, onDate, onDate)
	if err != nil {
		return nil, err
	}
	defer skills.Close()
	for skills.Next() {
		var techID, code string
		if err := skills.Scan(&techID, &code); err != nil {
			return nil, err
		}
		i, ok := index[techID]
		if !ok {
			return nil, fmt.Errorf("skill of unknown technician %s", techID)
		}
		techs[i].Skills = append(techs[i].Skills, code)
	}
	return techs, skills.Err()
}

func (r *Repository) Bay(ctx context.Context, id string) (setup.ServiceBay, error) {
	var b setup.ServiceBay
	err := r.db.QueryRowContext(ctx, `
		SELECT id, dealership_id, name, type, status FROM service_bay WHERE id = ?`, id).
		Scan(&b.ID, &b.DealershipID, &b.Name, &b.Type, &b.Status)
	if err != nil {
		return setup.ServiceBay{}, readError("bay", id, err)
	}
	return b, nil
}

// Technician returns one technician in any status, without skills.
func (r *Repository) Technician(ctx context.Context, id string) (setup.Technician, error) {
	var t setup.Technician
	err := r.db.QueryRowContext(ctx, `
		SELECT id, dealership_id, display_name, status FROM technician WHERE id = ?`, id).
		Scan(&t.ID, &t.DealershipID, &t.DisplayName, &t.Status)
	if err != nil {
		return setup.Technician{}, readError("technician", id, err)
	}
	return t, nil
}

// column runs a query that selects one text column and returns its values.
func (r *Repository) column(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
