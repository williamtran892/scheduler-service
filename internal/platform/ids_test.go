package platform

import (
	"testing"

	"github.com/google/uuid"
)

func TestUUIDs_AreCanonicalAndDistinct(t *testing.T) {
	ids := UUIDs{}
	a, b := ids.New(), ids.New()
	for _, id := range []string{a, b} {
		if len(id) != 36 {
			t.Errorf("%q has length %d, want 36", id, len(id))
		}
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("%q is not a UUID: %v", id, err)
		}
	}
	if a == b {
		t.Errorf("two calls returned the same id %q", a)
	}
}
