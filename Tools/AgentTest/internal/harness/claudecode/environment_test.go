package claudecode_test

// Tests for the environment pre-flight check, now reached through
// domain.HarnessAdapter.CheckEnvironment (Stage 3's port method) rather than
// a claudecode-local method: this adapter's existing check moves behind the
// port without losing any check it already performs (AC3.4).
//
// CheckEnvironment integrates interpreter resolution, bundle readability,
// binary-path resolution and outside-scope inspection into one report. A
// wrong interpreter, an unreadable bundle, or a competing outside-scope hook
// must all surface here — before any agent is spawned, never after — with a
// Detail naming what was tried and what to do.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// TestCheckEnvironment_UnresolvableInterpreterFails asserts that an
// adapter configured with an interpreter that cannot run fails the
// environment check with ProblemInterpreterUnresolved and a Detail naming
// what was tried, per AC10.6. This is the path AC10.6 and T10.3 name
// explicitly: a wrong interpreter yields a run with no logs and therefore no
// cost, so it must surface here rather than at spawn time.
func TestCheckEnvironment_UnresolvableInterpreterFails(t *testing.T) {
	const bogus = "definitely-not-a-real-interpreter-binary-xyz"

	adapter := claudecode.New(claudecode.Options{Interpreter: bogus})

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if report.OK() {
		t.Fatalf("CheckEnvironment: OK() = true, want false for an unresolvable interpreter")
	}

	var found bool
	for _, p := range report.Problems {
		if p.Kind != domain.ProblemInterpreterUnresolved {
			continue
		}
		found = true
		if p.Detail == "" {
			t.Errorf("CheckEnvironment: interpreter problem has empty Detail; must name what was tried")
		}
		if !strings.Contains(p.Detail, bogus) {
			t.Errorf("CheckEnvironment: interpreter problem Detail %q does not name what was tried (%q)", p.Detail, bogus)
		}
	}
	if !found {
		t.Errorf("CheckEnvironment: Problems = %+v, want a ProblemInterpreterUnresolved entry", report.Problems)
	}
}

// TestCheckEnvironment_UnreadableBundleFails asserts that an adapter
// configured with a logger bundle directory that cannot be read fails the
// environment check with ProblemBundleUnreadable, independent of whether the
// interpreter itself resolves.
func TestCheckEnvironment_UnreadableBundleFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist", "bundle")

	adapter := claudecode.New(claudecode.Options{
		Interpreter:     "py",
		LoggerBundleDir: missing,
	})

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if report.OK() {
		t.Fatalf("CheckEnvironment: OK() = true, want false for an unreadable bundle directory")
	}

	var found bool
	for _, p := range report.Problems {
		if p.Kind != domain.ProblemBundleUnreadable {
			continue
		}
		found = true
		if p.Detail == "" {
			t.Errorf("CheckEnvironment: bundle problem has empty Detail; must name what was tried")
		}
	}
	if !found {
		t.Errorf("CheckEnvironment: Problems = %+v, want a ProblemBundleUnreadable entry", report.Problems)
	}
}

// TestCheckEnvironment_MissingBundle_DetailNamesExpectedLayout asserts that
// an absent logger-bundle directory produces a Detail naming the expected
// bundle layout — the manifest file and the per-variant directory shape —
// rather than the bare file-not-found text hookbundle.Load's own error
// carries today. AC6.2 requires this for both an absent bundle and a
// misshapen one; this covers the absent case.
func TestCheckEnvironment_MissingBundle_DetailNamesExpectedLayout(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist", "bundle")

	adapter := claudecode.New(claudecode.Options{
		Interpreter:     "py",
		LoggerBundleDir: missing,
	})

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}

	var found bool
	for _, p := range report.Problems {
		if p.Kind != domain.ProblemBundleUnreadable && p.Kind != domain.ProblemBundleMisshapen {
			continue
		}
		found = true
		if !strings.Contains(p.Detail, "hook.yaml") {
			t.Errorf("CheckEnvironment: bundle problem Detail %q does not name the manifest file (%q); AC6.2 requires the expected layout, not a bare file-not-found", p.Detail, "hook.yaml")
		}
		if !strings.Contains(p.Detail, missing) {
			t.Errorf("CheckEnvironment: bundle problem Detail %q does not name the searched path %q", p.Detail, missing)
		}
		if !strings.Contains(p.Detail, "--logger-bundle") {
			t.Errorf("CheckEnvironment: bundle problem Detail %q does not name the --logger-bundle override", p.Detail)
		}
	}
	if !found {
		t.Errorf("CheckEnvironment: Problems = %+v, want a bundle problem for a missing directory", report.Problems)
	}
}

// TestCheckEnvironment_MisshapenBundle_IsDistinctFromUnreadableAndNamesLayout
// asserts that a bundle directory that exists but whose manifest cannot be
// parsed is classified ProblemBundleMisshapen — distinct from an absent or
// otherwise unreadable directory — and that its Detail names the expected
// layout the same way the absent case does.
func TestCheckEnvironment_MisshapenBundle_IsDistinctFromUnreadableAndNamesLayout(t *testing.T) {
	dir := t.TempDir()
	malformed := "not: [valid, yaml: structure"
	if err := os.WriteFile(filepath.Join(dir, "hook.yaml"), []byte(malformed), 0o644); err != nil {
		t.Fatalf("writing malformed hook.yaml: %v", err)
	}

	adapter := claudecode.New(claudecode.Options{
		Interpreter:     "py",
		LoggerBundleDir: dir,
	})

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}

	var found bool
	for _, p := range report.Problems {
		if p.Kind != domain.ProblemBundleMisshapen {
			continue
		}
		found = true
		if !strings.Contains(p.Detail, "hook.yaml") {
			t.Errorf("CheckEnvironment: misshapen-bundle Detail %q does not name the manifest file", p.Detail)
		}
		if !strings.Contains(p.Detail, dir) {
			t.Errorf("CheckEnvironment: misshapen-bundle Detail %q does not name the searched path %q", p.Detail, dir)
		}
	}
	if !found {
		t.Errorf("CheckEnvironment: Problems = %+v, want a ProblemBundleMisshapen entry for a bundle directory whose manifest cannot be parsed — distinct from ProblemBundleUnreadable, which names an absent or unreadable directory", report.Problems)
	}
}

// TestCheckEnvironment_CompetingOutsideScopeRewriterFails asserts that a
// configuration scope outside the workspace registering a competing
// input-rewriting hook fails the environment check with
// ProblemCompetingRewriter, so the refusal AC10.7 requires happens at
// pre-flight rather than only during Provision.
func TestCheckEnvironment_CompetingOutsideScopeRewriterFails(t *testing.T) {
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user": readScopeFixture(t, "user-settings-with-rewriter.json"),
		},
	}

	adapter := claudecode.New(claudecode.Options{
		Interpreter: "py",
		ScopeProbe:  probe,
	})

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if report.OK() {
		t.Fatalf("CheckEnvironment: OK() = true, want false when an outside scope has a competing input-rewriting hook")
	}

	var found bool
	for _, p := range report.Problems {
		if p.Kind != domain.ProblemCompetingRewriter {
			continue
		}
		found = true
		if p.Detail == "" {
			t.Errorf("CheckEnvironment: competing-rewriter problem has empty Detail; must name what was found")
		}
	}
	if !found {
		t.Errorf("CheckEnvironment: Problems = %+v, want a ProblemCompetingRewriter entry", report.Problems)
	}
}

// TestCheckEnvironment_HealthyConfigurationIsOK asserts the positive case:
// an adapter with an honoured interpreter, a readable bundle and no
// competing outside-scope hooks reports OK() true with no problems, so the
// negative-path tests above are proven against a check that can actually
// succeed rather than one that always fails.
func TestCheckEnvironment_HealthyConfigurationIsOK(t *testing.T) {
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user": readScopeFixture(t, "user-settings-clean.json"),
		},
	}

	adapter := claudecode.New(claudecode.Options{
		Interpreter:     "py",
		LoggerBundleDir: fixtureBundleDir(t, "fixture-bundle"),
		ScopeProbe:      probe,
	})

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if !report.OK() {
		t.Errorf("CheckEnvironment: OK() = false, want true for a resolvable interpreter, a readable bundle and no competing outside-scope hooks; problems=%+v", report.Problems)
	}

	// claudecode always runs an interpreter, so a healthy check must resolve
	// and report it — an adapter that ran no interpreter would leave both
	// zero-valued, per InterpreterApplicable's honesty obligation.
	if !report.InterpreterApplicable {
		t.Errorf("CheckEnvironment: InterpreterApplicable = false, want true — claudecode always runs an interpreter")
	}
	if report.InterpreterCmd != "py" {
		t.Errorf("CheckEnvironment: InterpreterCmd = %q, want %q", report.InterpreterCmd, "py")
	}
}
