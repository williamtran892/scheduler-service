package acceptance

import (
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// The dealership every scenario books at keeps local time in Europe/London; scenarios name
// dates relative to "today" there and times as local HH:MM. The fixture's ledger cells start
// at the database's CURRENT_DATE (UTC), so the London date is at most one day ahead of it.

// dayPattern is the step-text form of a relative date.
const dayPattern = `(tomorrow|yesterday|in \d+ days)`

// timePattern is the step-text form of a local time.
const timePattern = `(\d{2}:\d{2})`

var london = sync.OnceValue(func() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(fmt.Sprintf("load Europe/London: %v", err))
	}
	return loc
})

// today is fixed once per run, so a run that crosses midnight still books on consistent days.
var today = sync.OnceValue(func() time.Time {
	now := time.Now().In(london())
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, london())
})

var inNDays = regexp.MustCompile(`^in (\d+) days$`)

// day turns "tomorrow", "yesterday" or "in N days" into local midnight of that day.
func day(phrase string) (time.Time, error) {
	t := today()
	switch phrase {
	case "tomorrow":
		return t.AddDate(0, 0, 1), nil
	case "yesterday":
		return t.AddDate(0, 0, -1), nil
	}
	m := inNDays.FindStringSubmatch(phrase)
	if m == nil {
		return time.Time{}, fmt.Errorf("unknown day %q: want tomorrow, yesterday or in N days", phrase)
	}
	n, _ := strconv.Atoi(m[1])
	return t.AddDate(0, 0, n), nil
}

// at returns the instant of local time hhmm on the local day that midnight starts.
func at(midnight time.Time, hhmm string) (time.Time, error) {
	clock, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, fmt.Errorf("time %q: want HH:MM", hhmm)
	}
	return time.Date(midnight.Year(), midnight.Month(), midnight.Day(), clock.Hour(), clock.Minute(), 0, 0, london()), nil
}

// instant combines a relative day and a local time.
func instant(dayPhrase, hhmm string) (time.Time, error) {
	d, err := day(dayPhrase)
	if err != nil {
		return time.Time{}, err
	}
	return at(d, hhmm)
}

// sameLocalDay returns local midnight of the day t falls on in London.
func sameLocalDay(t time.Time) time.Time {
	l := t.In(london())
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, london())
}

// parseInstant reads an RFC 3339 instant from a JSON value.
func parseInstant(v any, what string) (time.Time, error) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("%s is %v, want an RFC 3339 string", what, v)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q: %w", what, s, err)
	}
	return t, nil
}
