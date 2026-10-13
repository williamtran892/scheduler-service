package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"scheduler/internal/scheduling"
)

// Every reader method that reads one row passes its failure through readError, so an unknown id
// reaches the service as scheduling.ErrNotFound, whichever table it was looked up in.
func TestReadError_NoRowIsSchedulingNotFound(t *testing.T) {
	noRow := fmt.Errorf("scan: %w", sql.ErrNoRows)
	for _, what := range []string{"dealership", "service type", "bay", "technician"} {
		err := readError(what, "x", noRow)
		if !errors.Is(err, scheduling.ErrNotFound) {
			t.Errorf("%s: error %v, want scheduling.ErrNotFound", what, err)
		}
		if want := what + " x: not found"; err.Error() != want {
			t.Errorf("%s: error %q, want %q", what, err, want)
		}
	}
}

func TestReadError_OtherFailuresKeepTheirCause(t *testing.T) {
	cause := errors.New("connection reset")

	err := readError("bay", "x", cause)

	if errors.Is(err, scheduling.ErrNotFound) || !errors.Is(err, cause) || err.Error() != "read bay x: connection reset" {
		t.Errorf("error %v, want %q wrapping the cause and not scheduling.ErrNotFound", err, "read bay x: connection reset")
	}
}
