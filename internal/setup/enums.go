package setup

import "database/sql/driver"

// BayType is service_bay.type and service_type_bay_type.bay_type (deviation D3).
type BayType string

const (
	BayGeneral   BayType = "GENERAL"
	BayMOT       BayType = "MOT"
	BayEVHV      BayType = "EV_HV"
	BayAlignment BayType = "ALIGNMENT"
	BayBodyshop  BayType = "BODYSHOP"
	BayExpress   BayType = "EXPRESS"
)

func ParseBayType(s string) (BayType, error)   { panic("not implemented") }
func (t *BayType) Scan(src any) error          { panic("not implemented") }
func (t BayType) Value() (driver.Value, error) { panic("not implemented") }

// BayStatus is service_bay.status.
type BayStatus string

const (
	BayActive       BayStatus = "ACTIVE"
	BayOutOfService BayStatus = "OUT_OF_SERVICE"
	BayRetired      BayStatus = "RETIRED"
)

func ParseBayStatus(s string) (BayStatus, error) { panic("not implemented") }
func (s *BayStatus) Scan(src any) error          { panic("not implemented") }
func (s BayStatus) Value() (driver.Value, error) { panic("not implemented") }

// TechnicianStatus is technician.status.
type TechnicianStatus string

const (
	TechnicianActive   TechnicianStatus = "ACTIVE"
	TechnicianInactive TechnicianStatus = "INACTIVE"
)

func ParseTechnicianStatus(s string) (TechnicianStatus, error) { panic("not implemented") }
func (s *TechnicianStatus) Scan(src any) error                 { panic("not implemented") }
func (s TechnicianStatus) Value() (driver.Value, error)        { panic("not implemented") }
