package scheduling

import (
	"os/exec"
	"strings"
	"testing"
)

// The domain root imports only the standard library (implementation notes 2), so it survives
// any change of framework or storage. A regression guard: it passes while the rule holds.
func TestRootImportsOnlyTheStandardLibrary(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep != "scheduler/internal/scheduling" {
			t.Errorf("scheduling imports %s; the domain root may import only the standard library", dep)
		}
	}
}
