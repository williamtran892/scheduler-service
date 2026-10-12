package setup

import (
	"errors"
	"testing"
)

func TestEnums_EveryConstantRoundTrips(t *testing.T) {
	for _, v := range []BayType{BayGeneral, BayMOT, BayEVHV, BayAlignment, BayBodyshop, BayExpress} {
		got, err := ParseBayType(string(v))
		var scanned BayType
		scanErr := scanned.Scan([]byte(v))
		if err != nil || got != v || scanErr != nil || scanned != v {
			t.Errorf("BayType %s: parse %v %v, scan %v %v", v, got, err, scanned, scanErr)
		}
	}
	for _, v := range []BayStatus{BayActive, BayOutOfService, BayRetired} {
		got, err := ParseBayStatus(string(v))
		var scanned BayStatus
		scanErr := scanned.Scan(string(v))
		if err != nil || got != v || scanErr != nil || scanned != v {
			t.Errorf("BayStatus %s: parse %v %v, scan %v %v", v, got, err, scanned, scanErr)
		}
	}
	for _, v := range []TechnicianStatus{TechnicianActive, TechnicianInactive} {
		got, err := ParseTechnicianStatus(string(v))
		var scanned TechnicianStatus
		scanErr := scanned.Scan(string(v))
		if err != nil || got != v || scanErr != nil || scanned != v {
			t.Errorf("TechnicianStatus %s: parse %v %v, scan %v %v", v, got, err, scanned, scanErr)
		}
	}
}

func TestEnums_UnknownValuesAreRejected(t *testing.T) {
	if _, err := ParseBayType("LIFT"); !errors.Is(err, ErrInvalidEnum) {
		t.Errorf("ParseBayType(LIFT) = %v, want ErrInvalidEnum", err)
	}
	if _, err := ParseBayStatus("BROKEN"); !errors.Is(err, ErrInvalidEnum) {
		t.Errorf("ParseBayStatus(BROKEN) = %v, want ErrInvalidEnum", err)
	}
	if _, err := ParseTechnicianStatus("ON_LEAVE"); !errors.Is(err, ErrInvalidEnum) {
		t.Errorf("ParseTechnicianStatus(ON_LEAVE) = %v, want ErrInvalidEnum", err)
	}
	var b BayType
	if err := b.Scan(nil); err == nil {
		t.Error("BayType.Scan(nil) succeeded, want an error")
	}
}
