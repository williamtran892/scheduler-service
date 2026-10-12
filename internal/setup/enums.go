package setup

import (
	"database/sql/driver"
	"fmt"
)

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

func ParseBayType(s string) (BayType, error) {
	switch t := BayType(s); t {
	case BayGeneral, BayMOT, BayEVHV, BayAlignment, BayBodyshop, BayExpress:
		return t, nil
	}
	return "", fmt.Errorf("%w: bay type %q", ErrInvalidEnum, s)
}

func (t *BayType) Scan(src any) error {
	s, err := scanString(src)
	if err != nil {
		return err
	}
	v, err := ParseBayType(s)
	if err != nil {
		return err
	}
	*t = v
	return nil
}

func (t BayType) Value() (driver.Value, error) { return string(t), nil }

// BayStatus is service_bay.status.
type BayStatus string

const (
	BayActive       BayStatus = "ACTIVE"
	BayOutOfService BayStatus = "OUT_OF_SERVICE"
	BayRetired      BayStatus = "RETIRED"
)

func ParseBayStatus(s string) (BayStatus, error) {
	switch v := BayStatus(s); v {
	case BayActive, BayOutOfService, BayRetired:
		return v, nil
	}
	return "", fmt.Errorf("%w: bay status %q", ErrInvalidEnum, s)
}

func (s *BayStatus) Scan(src any) error {
	str, err := scanString(src)
	if err != nil {
		return err
	}
	v, err := ParseBayStatus(str)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

func (s BayStatus) Value() (driver.Value, error) { return string(s), nil }

// TechnicianStatus is technician.status.
type TechnicianStatus string

const (
	TechnicianActive   TechnicianStatus = "ACTIVE"
	TechnicianInactive TechnicianStatus = "INACTIVE"
)

func ParseTechnicianStatus(s string) (TechnicianStatus, error) {
	switch v := TechnicianStatus(s); v {
	case TechnicianActive, TechnicianInactive:
		return v, nil
	}
	return "", fmt.Errorf("%w: technician status %q", ErrInvalidEnum, s)
}

func (s *TechnicianStatus) Scan(src any) error {
	str, err := scanString(src)
	if err != nil {
		return err
	}
	v, err := ParseTechnicianStatus(str)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

func (s TechnicianStatus) Value() (driver.Value, error) { return string(s), nil }

// scanString reads the string a driver hands to Scan: text arrives as []byte or string.
func scanString(src any) (string, error) {
	switch v := src.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	}
	return "", fmt.Errorf("scan enumeration: cannot read a %T", src)
}
