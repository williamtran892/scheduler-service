package mysql

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writesTimeSlot matches a statement that changes the ledger.
var writesTimeSlot = regexp.MustCompile(`(?i)\b(UPDATE\s+time_slot|INSERT\s+(IGNORE\s+)?INTO\s+time_slot|DELETE\s+FROM\s+time_slot)\b`)

// TestOnlyLedgerGoWritesTimeSlot pins every ledger write to ledger.go (implementation notes 4.5,
// rule 3), and requires ledger.go to hold the claim with its locking read (rule 1): without
// FOR UPDATE a claim under REPEATABLE READ reads a snapshot and double books.
func TestOnlyLedgerGoWritesTimeSlot(t *testing.T) {
	ledger, err := os.ReadFile("ledger.go")
	if err != nil {
		t.Fatalf("ledger.go must hold the claim: %v", err)
	}
	for _, want := range []string{"FOR UPDATE", "UPDATE time_slot"} {
		if !strings.Contains(string(ledger), want) {
			t.Errorf("ledger.go has no %q", want)
		}
	}

	root := filepath.Join("..", "..", "..")
	self, _ := filepath.Abs("ledger.go")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin" || d.Name() == ".specify") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		abs, _ := filepath.Abs(path)
		if abs == self {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if loc := writesTimeSlot.Find(src); loc != nil {
			t.Errorf("%s writes time_slot (%q); only internal/scheduling/mysql/ledger.go may", path, loc)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
