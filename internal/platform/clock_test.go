package platform

import (
	"testing"
	"time"
)

func TestFakeClock_ReturnsTheTimeItWasSetTo(t *testing.T) {
	london, _ := time.LoadLocation("Europe/London")
	c := NewFakeClock(time.Date(2026, 10, 20, 9, 0, 0, 0, london))

	want := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	if got := c.Now(); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("Now() = %v, want %v in UTC", got, want)
	}

	c.Advance(15 * time.Minute)
	if got := c.Now(); !got.Equal(want.Add(15 * time.Minute)) {
		t.Errorf("after Advance, Now() = %v", got)
	}

	c.Set(want)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("after Set, Now() = %v", got)
	}
}

func TestRealClock_IsUTC(t *testing.T) {
	if loc := (RealClock{}).Now().Location(); loc != time.UTC {
		t.Errorf("location = %v, want UTC", loc)
	}
}
