package main

// Tests for the importcheck boundary guard.
//
// The boundary verification test (T11.2) runs runChecks against the actual
// module source tree and asserts there are no violations. This serves as a
// build-time regression guard: if any package violates a boundary rule, this
// test fails with a specific message naming the file and the forbidden import.
//
// The test runs from the package directory (tools/importcheck/), so the
// module root is two levels up ("../..").

import (
	"testing"
)

// TestImportBoundaries_NoViolations verifies that all import-boundary rules
// pass with zero violations against the current source tree.
//
// A failure here means a package has imported something it should not:
//   - domain importing an internal package
//   - engine importing a non-domain internal package or an I/O package
//   - session importing tui or cli
//   - tui importing cli (or vice versa)
//   - an internal production package importing harness directly
//
// Fix the import in the offending file, not this test.
func TestImportBoundaries_NoViolations(t *testing.T) {
	moduleRoot := "../.."

	violations, errs := runChecks(moduleRoot)

	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("tool error: %s", e)
		}
		t.Fatal("import-check tool encountered errors; fix the errors above")
	}

	if len(violations) > 0 {
		t.Errorf("import boundary violations (%d):", len(violations))
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
		t.Fatal("fix the imports above to restore the dependency direction")
	}
}

// TestCheckImport_ForbidAllModuleImports verifies that forbidAllModuleImports
// correctly flags any import that starts with the module prefix.
func TestCheckImport_ForbidAllModuleImports(t *testing.T) {
	r := rule{
		dir:                    "internal/domain",
		desc:                   "domain must not import any module package",
		forbidAllModuleImports: true,
	}

	// An internal import should be flagged.
	got := checkImport("mosaic-run/internal/engine", r, "engine.go")
	if got == "" {
		t.Error("expected violation for module-internal import when forbidAllModuleImports=true, got none")
	}

	// A stdlib import should be allowed.
	got = checkImport("context", r, "ports.go")
	if got != "" {
		t.Errorf("unexpected violation for stdlib import: %s", got)
	}

	// A third-party import should be allowed.
	got = checkImport("mosaic-common/interaction", r, "ports.go")
	if got != "" {
		t.Errorf("unexpected violation for mosaic-common import (not forbidden): %s", got)
	}
}

// TestCheckImport_ForbidPrefix verifies that prefix-based bans catch exact
// matches and sub-packages, but allow other packages.
func TestCheckImport_ForbidPrefix(t *testing.T) {
	r := rule{
		dir:          "internal/session",
		desc:         "session must not import frontends",
		forbidPrefix: []string{"mosaic-run/internal/tui", "mosaic-run/internal/cli"},
	}

	tests := []struct {
		imp       string
		wantViol  bool
	}{
		{"mosaic-run/internal/tui", true},           // exact match
		{"mosaic-run/internal/tui/screens", true},   // sub-package
		{"mosaic-run/internal/cli", true},           // exact match
		{"mosaic-run/internal/domain", false},       // allowed
		{"mosaic-run/internal/engine", false},       // allowed
		{"context", false},                          // stdlib
	}

	for _, tt := range tests {
		got := checkImport(tt.imp, r, "session.go")
		if tt.wantViol && got == "" {
			t.Errorf("checkImport(%q): expected violation, got none", tt.imp)
		}
		if !tt.wantViol && got != "" {
			t.Errorf("checkImport(%q): unexpected violation: %s", tt.imp, got)
		}
	}
}

// TestCheckImport_ForbidExact verifies that exact bans match only the exact
// import path.
func TestCheckImport_ForbidExact(t *testing.T) {
	r := rule{
		dir:         "internal/engine",
		desc:        "engine must not import syscall",
		forbidExact: []string{"syscall"},
	}

	got := checkImport("syscall", r, "engine.go")
	if got == "" {
		t.Error("expected violation for exact-banned import 'syscall', got none")
	}

	// 'syscall/js' is not an exact match for 'syscall', so it should NOT be flagged.
	// (The exact ban only matches the literal string "syscall", not sub-packages.)
	got = checkImport("syscall/js", r, "engine.go")
	if got != "" {
		t.Errorf("unexpected violation for 'syscall/js' with exact ban on 'syscall': %s", got)
	}
}

// TestEngineIObans verifies that the engine rule forbids the specific I/O
// packages listed in the design (os, net, math/rand, crypto/rand, syscall).
func TestEngineIObans(t *testing.T) {
	// Find the engine rule.
	var engineRule rule
	found := false
	for _, r := range rules {
		if r.dir == "internal/engine" {
			engineRule = r
			found = true
			break
		}
	}
	if !found {
		t.Fatal("engine rule not found in rules list")
	}

	ioBanned := []string{"os", "os/exec", "os/signal", "net", "net/http", "math/rand", "crypto/rand"}
	for _, pkg := range ioBanned {
		got := checkImport(pkg, engineRule, "engine.go")
		if got == "" {
			t.Errorf("engine rule: expected violation for I/O package %q, got none", pkg)
		}
	}

	// "time" is allowed (used for time.Time parameter type only).
	got := checkImport("time", engineRule, "engine.go")
	if got != "" {
		t.Errorf("engine rule: unexpected violation for 'time' (should be allowed for time.Time): %s", got)
	}

	// "fmt" and "strings" are allowed (no I/O).
	for _, pkg := range []string{"fmt", "strings"} {
		got := checkImport(pkg, engineRule, "engine.go")
		if got != "" {
			t.Errorf("engine rule: unexpected violation for %q: %s", pkg, got)
		}
	}
}
