package evaluate_test

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// TestEvaluate_PackagePurity_NoIOImports pins the AC15.6 requirement that the
// evaluate package remains free of I/O and side-effecting direct imports. The
// package is a pure function of its inputs: the same evidence always yields the
// same verdict, so re-evaluating a stored run's evidence after an assertion
// correction must not touch the filesystem, network, or any external resource.
//
// The test uses `go list -json` to inspect the evaluate package's direct
// imports at test time. Any I/O-bearing package in that set is a layering
// violation: retention I/O and all other side effects must live in the
// use-case layer (runner, suite), never in evaluate.
//
// If this test fails, locate the new import in the evaluate package and either
// remove it or move the I/O concern into the caller (runner or suite).
func TestEvaluate_PackagePurity_NoIOImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-json", "mosaic-agent-test/internal/evaluate").Output()
	if err != nil {
		t.Skipf("go list unavailable (go toolchain not on PATH): %v", err)
		return
	}

	var pkg struct {
		Imports []string `json:"Imports"`
	}
	if jsonErr := json.Unmarshal(out, &pkg); jsonErr != nil {
		t.Fatalf("parsing go list output: %v", jsonErr)
	}

	// I/O-bearing and side-effecting standard-library packages. Their presence
	// as a direct import of the evaluate package is a purity violation.
	forbidden := map[string]string{
		"os":           "filesystem and process I/O",
		"io":           "I/O primitives",
		"io/fs":        "filesystem abstraction",
		"net":          "network I/O",
		"net/http":     "HTTP client/server",
		"bufio":        "buffered I/O",
		"database/sql": "database I/O",
		"syscall":      "raw OS syscalls",
		"os/exec":      "subprocess execution",
	}

	for _, imp := range pkg.Imports {
		if reason, bad := forbidden[imp]; bad {
			t.Errorf(
				"evaluate package directly imports %q (%s) — this violates the purity requirement: "+
					"evaluate must be a pure function with no side effects; I/O belongs in the runner or suite layer",
				imp, reason,
			)
		}
	}
}
