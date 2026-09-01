package main

// Tests for suite-discovery default resolution. The suite-discovery root used
// by the TUI frontend must anchor to the running binary's own directory
// (selfDir/..), not to the current working directory, so the tool works
// correctly when invoked from any directory. These tests cover:
//
//   - resolveWiringConfig populates WiringConfig.SelfDir correctly (the
//     binary-relative anchor every default, including the suite default, flows
//     through)
//   - when --suites is provided, suitesRootFrom returns that value
//     unconditionally
//   - when --suites is absent, suitesRootFrom returns selfDir/.., not CWD
//   - the current working directory is never consulted for suite discovery
//
// FR-3 explicitly prohibits an environment variable override for suite
// discovery; only flag > default precedence applies. No env-tier test exists
// for suite discovery.

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// WiringConfig.SelfDir population
// ---------------------------------------------------------------------------

// TestResolveWiringConfig_SelfDir_PopulatedToBinaryDir asserts that
// resolveWiringConfig populates WiringConfig.SelfDir with the directory
// containing the running binary. SelfDir is the common anchor for all
// binary-relative defaults, including the TUI suite-discovery default; this
// test pins that the field is correctly propagated from resolveWiringConfig
// before any consumer of the field exists in the implementation.
func TestResolveWiringConfig_SelfDir_PopulatedToBinaryDir(t *testing.T) {
	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})
	want := expectedSelfDir(t)
	if cfg.SelfDir != want {
		t.Errorf("resolveWiringConfig(...).SelfDir = %q, want the binary directory %q (SelfDir must be populated to match the os.Executable()/filepath.Dir anchor used by every other binary-relative default)", cfg.SelfDir, want)
	}
}

// TestResolveWiringConfig_SelfDir_NonEmpty asserts that resolveWiringConfig
// always produces a non-empty SelfDir. An empty SelfDir would propagate to
// every binary-relative default and to the suite-discovery default,
// effectively anchoring all of them to the process working directory.
func TestResolveWiringConfig_SelfDir_NonEmpty(t *testing.T) {
	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})
	if cfg.SelfDir == "" {
		t.Error("resolveWiringConfig(...).SelfDir is empty; it must be the directory of the running binary so every binary-relative default — including the suite-discovery default — resolves correctly regardless of working directory")
	}
}

// ---------------------------------------------------------------------------
// suitesRootFrom: flag override
// ---------------------------------------------------------------------------

// TestSuitesRootFrom_FlagOverridesDefault asserts that when --suites is
// provided, its value is returned unconditionally in both the space-separated
// and equals-separated forms, regardless of the selfDir argument.
func TestSuitesRootFrom_FlagOverridesDefault(t *testing.T) {
	selfDir := expectedSelfDir(t)
	const flagPath = "C:/my-suites"

	cases := map[string][]string{
		"space":  {"--suites", flagPath},
		"equals": {"--suites=" + flagPath},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			got := suitesRootFrom(args, selfDir)
			if got != flagPath {
				t.Errorf("suitesRootFrom(%v, selfDir) = %q, want the flag value %q (--suites must override the selfDir-relative default unconditionally)", args, got, flagPath)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// suitesRootFrom: default is selfDir/..
// ---------------------------------------------------------------------------

// TestSuitesRootFrom_DefaultIsSelfDirParent asserts that when --suites is
// absent, the suite-discovery root defaults to selfDir/.. — one level above
// the running binary's directory. In a correctly staged distribution this is
// the AgentTest module root, making suite discovery consistent with every
// other binary-relative default in the tool.
func TestSuitesRootFrom_DefaultIsSelfDirParent(t *testing.T) {
	selfDir := expectedSelfDir(t)
	got := suitesRootFrom([]string{}, selfDir)
	want := filepath.Join(selfDir, "..")
	if got != want {
		t.Errorf("suitesRootFrom(no --suites flag, selfDir=%q) = %q, want selfDir/.. = %q (the suite-discovery default must anchor to the binary location, never to the current working directory)", selfDir, got, want)
	}
}

// TestSuitesRootFrom_DefaultIsSelfDirParent_OtherArgs asserts that incidental
// args (harness flags, workspace flags, etc.) do not cause suitesRootFrom to
// misread a flag value as --suites or to fall back to CWD.
func TestSuitesRootFrom_DefaultIsSelfDirParent_OtherArgs(t *testing.T) {
	selfDir := expectedSelfDir(t)
	args := []string{"--harness", "claude-code", "--workspace-root", "C:/workspaces"}
	got := suitesRootFrom(args, selfDir)
	want := filepath.Join(selfDir, "..")
	if got != want {
		t.Errorf("suitesRootFrom(%v, selfDir=%q) = %q, want selfDir/.. = %q (unrelated flags must not affect suite-discovery default resolution)", args, selfDir, got, want)
	}
}

// ---------------------------------------------------------------------------
// suitesRootFrom: CWD is not consulted
// ---------------------------------------------------------------------------

// TestSuitesRootFrom_DefaultDoesNotConsultCWD asserts that the suite-discovery
// default is independent of the current working directory. The test changes the
// working directory to a temporary directory and confirms the result does not
// equal that directory — proving CWD is never used as the suite-discovery
// fallback when --suites is absent.
func TestSuitesRootFrom_DefaultDoesNotConsultCWD(t *testing.T) {
	selfDir := expectedSelfDir(t)

	// Capture the original working directory so the process can be restored.
	origDir, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot determine working directory: %v", err)
	}

	// Change to a temporary directory that is guaranteed to differ from
	// selfDir so any CWD-based resolution would produce a different result
	// than the expected selfDir/.. default.
	tmpDir := t.TempDir()

	// Register the CWD-restore cleanup AFTER t.TempDir() so that Go's LIFO
	// cleanup order restores the working directory back to origDir before
	// TempDir attempts to remove tmpDir. On Windows a directory cannot be
	// deleted while it is the process working directory, so this ordering
	// avoids a "TempDir RemoveAll cleanup" error both during the RED phase
	// and after implementation in the GREEN phase.
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Skipf("cannot change working directory to %q: %v", tmpDir, err)
	}

	got := suitesRootFrom([]string{}, selfDir)

	// The result must never equal the temporary working directory. If it does,
	// the implementation is consulting CWD rather than selfDir.
	if got == tmpDir {
		t.Errorf("suitesRootFrom(no --suites flag) = %q, which equals the current working directory %q; the default must be selfDir-relative, never CWD-relative", got, tmpDir)
	}

	// The result must equal selfDir/.., confirming the binary-relative anchor.
	want := filepath.Join(selfDir, "..")
	if got != want {
		t.Errorf("suitesRootFrom(no --suites flag, selfDir=%q) = %q, want selfDir/.. = %q", selfDir, got, want)
	}
}
