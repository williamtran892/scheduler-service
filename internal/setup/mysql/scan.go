package mysql

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// stringList scans the JSON array that JSON_ARRAYAGG returns into a sorted slice. The function
// has no ORDER BY, so the order is set here; NULL, which is what it returns when there is
// nothing to aggregate, is an empty list.
type stringList []string

func (l *stringList) Scan(src any) error {
	if src == nil {
		*l = nil
		return nil
	}
	s, err := text(src)
	if err != nil {
		return fmt.Errorf("scan string list: %w", err)
	}
	var list []string
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		return fmt.Errorf("scan string list: %w", err)
	}
	slices.Sort(list)
	*l = list
	return nil
}

// zone scans an IANA time zone name into a Location, so a row with a name this host does not
// know fails when it is read instead of leaving the dealership without a Location.
type zone struct{ loc *time.Location }

func (z *zone) Scan(src any) error {
	name, err := text(src)
	if err != nil {
		return fmt.Errorf("scan time zone: %w", err)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("scan time zone: %w", err)
	}
	z.loc = loc
	return nil
}

// text returns the value of a text column, which the driver sends as []byte or string.
func text(src any) (string, error) {
	switch v := src.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	}
	return "", fmt.Errorf("cannot read a %T", src)
}
