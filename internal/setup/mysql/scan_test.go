package mysql

import (
	"database/sql"
	"reflect"
	"testing"
)

// The skill codes and bay types of a service type, and the skills of a technician, come back as
// one JSON array per row; the scanner must hand Scheduling the same list whichever type the
// driver uses for it.
func TestStringList_ScansJSONArrayAggAsASortedList(t *testing.T) {
	for name, src := range map[string]any{
		"bytes":  []byte(`["GENERAL_SERVICE", "EV_HV_L3"]`),
		"string": `["GENERAL_SERVICE", "EV_HV_L3"]`,
	} {
		var got stringList
		if err := got.Scan(src); err != nil {
			t.Fatalf("%s: Scan: %v", name, err)
		}
		if want := (stringList{"EV_HV_L3", "GENERAL_SERVICE"}); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Scan = %v, want %v", name, got, want)
		}
	}
}

func TestStringList_NullIsAnEmptyList(t *testing.T) {
	got := stringList{"left over"}
	if err := got.Scan(nil); err != nil || len(got) != 0 {
		t.Errorf("Scan(nil) = %v, %v; want an empty list", got, err)
	}
}

func TestStringList_RejectsWhatIsNotAJSONArrayOfStrings(t *testing.T) {
	for name, src := range map[string]any{"not json": "GENERAL_SERVICE", "numbers": "[1, 2]", "an integer": int64(7)} {
		var got stringList
		if err := got.Scan(src); err == nil {
			t.Errorf("%s: Scan accepted %v as %v", name, src, got)
		}
	}
}

func TestStringList_ScansIntoAStringSliceField(t *testing.T) {
	var skills []string
	var s sql.Scanner = (*stringList)(&skills)
	if err := s.Scan([]byte(`["MOT_TESTER"]`)); err != nil || !reflect.DeepEqual(skills, []string{"MOT_TESTER"}) {
		t.Errorf("skills = %v, %v", skills, err)
	}
}

func TestZone_LoadsTheDealershipsLocation(t *testing.T) {
	for name, src := range map[string]any{"bytes": []byte("Europe/London"), "string": "Europe/London"} {
		var z zone
		if err := z.Scan(src); err != nil || z.loc == nil || z.loc.String() != "Europe/London" {
			t.Errorf("%s: Scan = %v, %v; want Europe/London", name, z.loc, err)
		}
	}
}

func TestZone_UnknownTimeZoneIsAnError(t *testing.T) {
	for name, src := range map[string]any{"unknown name": "Mars/Olympus", "null": nil} {
		var z zone
		if err := z.Scan(src); err == nil {
			t.Errorf("%s: Scan accepted %v as %v", name, src, z.loc)
		}
	}
}
