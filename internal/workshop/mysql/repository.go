package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"scheduler/internal/scheduling"
)

// Repository reads Workshop Setup's tables in Scheduling's terms. It implements
// scheduling.WorkshopReader.
type Repository struct {
	db *sql.DB
}

var _ scheduling.WorkshopReader = (*Repository)(nil)

// New returns a repository over db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) DealershipConfig(ctx context.Context, dealershipID string) (scheduling.DealershipConfig, error) {
	var (
		c  scheduling.DealershipConfig
		tz zone
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, timezone, slot_minutes, booking_horizon_days
		  FROM dealership WHERE id = ?`, dealershipID).
		Scan(&c.DealershipID, &c.TenantID, &c.Code, &c.Name, &tz, &c.Grid.SlotMinutes, &c.BookingHorizonDays)
	if err != nil {
		return scheduling.DealershipConfig{}, readError("dealership", dealershipID, err)
	}
	c.Location = tz.loc
	return c, nil
}

// Requirement returns the service type with the skills it requires and the bay types it allows,
// in one query.
func (r *Repository) Requirement(ctx context.Context, serviceTypeID string) (scheduling.ServiceRequirement, error) {
	var (
		req                     scheduling.ServiceRequirement
		duration, before, after int // minutes
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT st.id, st.tenant_id, st.code, st.name,
		       st.duration_minutes, st.buffer_before_minutes, st.buffer_after_minutes,
		       st.online_bookable, st.active,
		       (SELECT JSON_ARRAYAGG(s.code)
		          FROM service_type_skill sts JOIN skill s ON s.id = sts.skill_id
		         WHERE sts.service_type_id = st.id),
		       (SELECT JSON_ARRAYAGG(bt.bay_type)
		          FROM service_type_bay_type bt
		         WHERE bt.service_type_id = st.id)
		  FROM service_type st WHERE st.id = ?`, serviceTypeID).
		Scan(&req.ServiceTypeID, &req.TenantID, &req.Code, &req.Name,
			&duration, &before, &after,
			&req.OnlineBookable, &req.Active,
			(*stringList)(&req.RequiredSkills), (*stringList)(&req.AllowedBayTypes))
	if err != nil {
		return scheduling.ServiceRequirement{}, readError("service type", serviceTypeID, err)
	}
	req.Duration = time.Duration(duration) * time.Minute
	req.BufferBefore = time.Duration(before) * time.Minute
	req.BufferAfter = time.Duration(after) * time.Minute
	return req, nil
}

// Resources returns the ACTIVE bays and the ACTIVE technicians of the dealership, each
// technician with the skills valid on localDate.
func (r *Repository) Resources(ctx context.Context, dealershipID, localDate string) ([]scheduling.ServiceBay, []scheduling.Technician, error) {
	bays, err := r.activeBays(ctx, dealershipID)
	if err != nil {
		return nil, nil, fmt.Errorf("read bays of dealership %s: %w", dealershipID, err)
	}
	techs, err := r.activeTechnicians(ctx, dealershipID, localDate)
	if err != nil {
		return nil, nil, fmt.Errorf("read technicians of dealership %s: %w", dealershipID, err)
	}
	return bays, techs, nil
}

func (r *Repository) activeBays(ctx context.Context, dealershipID string) ([]scheduling.ServiceBay, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, type FROM service_bay
		 WHERE dealership_id = ? AND status = 'ACTIVE' ORDER BY name`, dealershipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bays []scheduling.ServiceBay
	for rows.Next() {
		var b scheduling.ServiceBay
		if err := rows.Scan(&b.ID, &b.Name, &b.Type); err != nil {
			return nil, err
		}
		bays = append(bays, b)
	}
	return bays, rows.Err()
}

// activeTechnicians returns the skills valid on localDate: valid_from is not after the date and
// valid_to is null or not before it (design 11.4).
func (r *Repository) activeTechnicians(ctx context.Context, dealershipID, localDate string) ([]scheduling.Technician, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.display_name,
		       (SELECT JSON_ARRAYAGG(s.code)
		          FROM technician_skill ts JOIN skill s ON s.id = ts.skill_id
		         WHERE ts.technician_id = t.id
		           AND ts.valid_from <= ? AND (ts.valid_to IS NULL OR ts.valid_to >= ?))
		  FROM technician t
		 WHERE t.dealership_id = ? AND t.status = 'ACTIVE' ORDER BY t.display_name`,
		localDate, localDate, dealershipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var techs []scheduling.Technician
	for rows.Next() {
		var t scheduling.Technician
		if err := rows.Scan(&t.ID, &t.Name, (*stringList)(&t.Skills)); err != nil {
			return nil, err
		}
		techs = append(techs, t)
	}
	return techs, rows.Err()
}

// Bay returns one bay in any status.
func (r *Repository) Bay(ctx context.Context, id string) (scheduling.ServiceBay, error) {
	var b scheduling.ServiceBay
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, type FROM service_bay WHERE id = ?`, id).
		Scan(&b.ID, &b.Name, &b.Type)
	if err != nil {
		return scheduling.ServiceBay{}, readError("bay", id, err)
	}
	return b, nil
}

// Technician returns one technician in any status, without skills.
func (r *Repository) Technician(ctx context.Context, id string) (scheduling.Technician, error) {
	var t scheduling.Technician
	err := r.db.QueryRowContext(ctx, `
		SELECT id, display_name FROM technician WHERE id = ?`, id).
		Scan(&t.ID, &t.Name)
	if err != nil {
		return scheduling.Technician{}, readError("technician", id, err)
	}
	return t, nil
}
