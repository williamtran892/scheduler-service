package mysql

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

	self, _ := filepath.Abs("ledger.go")
	writers, err := ledgerWriters(filepath.Join("..", "..", ".."), self)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range writers {
		t.Errorf("%s; only internal/scheduling/mysql/ledger.go may write time_slot", w)
	}
}

// TestLedgerGuardSkipsOnlyTestCode proves the guard still catches a production writer while it
// ignores test code: _test.go files and the test/ tree, where the acceptance suite's @clock step
// backdates cells.
func TestLedgerGuardSkipsOnlyTestCode(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) string {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	const stmt = "package x\nconst q = `UPDATE time_slot SET status = 'FREE'`\n"
	self := write("internal/scheduling/mysql/ledger.go", stmt)
	write("internal/scheduling/mysql/ledger_test.go", stmt)
	write("test/acceptance/steps_clock.go", stmt)
	write("internal/scheduling/service/sneaky.go", stmt)
	write("cmd/scheduler/main.go", "package main\n")

	selfAbs, _ := filepath.Abs(self)
	writers, err := ledgerWriters(root, selfAbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(writers) != 1 || !strings.Contains(writers[0], filepath.Join("internal", "scheduling", "service", "sneaky.go")) {
		t.Errorf("writers = %v, want only internal/scheduling/service/sneaky.go", writers)
	}
}

// ledgerWriters walks the production Go files under root and reports every one, other than
// self, that writes time_slot. Test code is not production code and is skipped: _test.go files,
// and the test/ tree at the root (the acceptance suite).
func ledgerWriters(root, self string) ([]string, error) {
	testTree, _ := filepath.Abs(filepath.Join(root, "test"))
	var writers []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		abs, _ := filepath.Abs(path)
		if d.IsDir() {
			if slices.Contains([]string{".git", "bin", ".specify"}, d.Name()) || abs == testTree {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || abs == self {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if loc := writesTimeSlot.Find(src); loc != nil {
			writers = append(writers, path+" writes time_slot ("+string(loc)+")")
		}
		return nil
	})
	return writers, err
}
