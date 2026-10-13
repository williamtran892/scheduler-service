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

// conditionalClaim matches the claim statement: one raw-string statement that updates time_slot,
// bumps version = version + 1 and is conditioned on the cell being FREE or owned by the claimant,
// (status = ? OR appointment_id = ?). It does not match ReleaseCells' UPDATE, which clears
// appointment_id and has no owner alternative.
var conditionalClaim = regexp.MustCompile(
	`(?is)UPDATE\s+time_slot\s+SET[^` + "`" + `]*version\s*=\s*version\s*\+\s*1[^` + "`" + `]*` +
		`\(\s*status\s*=\s*\?\s+OR\s+appointment_id\s*=\s*\?\s*\)`)

// TestOnlyLedgerGoWritesTimeSlot pins every ledger write to ledger.go (implementation notes 4.5,
// rule 3), and requires ledger.go to hold the claim as one conditional UPDATE (rule 1): the
// UPDATE is the locking current read that serialises overlapping claimers, and its condition,
// (status = ? OR appointment_id = ?), is what refuses a taken cell. A claim that only read first
// at REPEATABLE READ would see a snapshot and double book.
func TestOnlyLedgerGoWritesTimeSlot(t *testing.T) {
	ledger, err := os.ReadFile("ledger.go")
	if err != nil {
		t.Fatalf("ledger.go must hold the claim: %v", err)
	}
	if !conditionalClaim.Match(ledger) {
		t.Error("ledger.go has no conditional claim: UPDATE time_slot SET ... version = version + 1 ... " +
			"(status = ? OR appointment_id = ?)")
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

// TestConditionalClaimPattern proves the claim check tells the conditional claim from the
// statements that must not satisfy it: the unconditional UPDATE the claim used to run after its
// locking read, and ReleaseCells' UPDATE.
func TestConditionalClaimPattern(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"the claim", "tx.ExecContext(ctx, `\n\t\tUPDATE time_slot SET status = ?, appointment_id = ?, version = version + 1\n" +
			"\t\t WHERE resource_kind = ? AND slot_start BETWEEN ? AND ?\n\t\t   AND (status = ? OR appointment_id = ?)`, a)", true},
		{"unconditional update after a locking read", "`SELECT slot_start FROM time_slot WHERE x = ? FOR UPDATE`\n" +
			"`UPDATE time_slot SET status = ?, appointment_id = ?, version = version + 1 WHERE x = ? AND slot_start BETWEEN ? AND ?`", false},
		{"release", "`UPDATE time_slot SET status = ?, appointment_id = NULL, version = version + 1\n" +
			" WHERE appointment_id = ? AND resource_kind = ? AND slot_start IN (?)`", false},
		{"condition in another statement", "`UPDATE time_slot SET version = version + 1 WHERE x = ?`\n" +
			"`SELECT 1 FROM time_slot WHERE (status = ? OR appointment_id = ?)`", false},
	}
	for _, c := range cases {
		if got := conditionalClaim.MatchString(c.src); got != c.want {
			t.Errorf("%s: matches = %v, want %v", c.name, got, c.want)
		}
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
