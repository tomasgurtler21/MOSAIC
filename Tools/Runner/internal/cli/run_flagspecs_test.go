package cli_test

import (
	"testing"

	"github.com/spf13/pflag"

	"mosaic-run/internal/cli"
)

// TestRunFlagSpecs_IsNonEmpty verifies that RunFlagSpecs returns at least one
// entry. An empty slice means the arity map is empty, which would cause
// hasPositionalArg to treat every flag value as positional.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_IsNonEmpty(t *testing.T) {
	specs := cli.RunFlagSpecs()
	if len(specs) == 0 {
		t.Fatal("RunFlagSpecs() returned empty slice; want at least one FlagSpec entry")
	}
}

// TestRunFlagSpecs_ContainsTUIFlag verifies that RunFlagSpecs includes an entry
// for "--tui" with TakesValue: false. The --tui flag is the one entry-point-only
// flag (not registered on the run subcommand) that RunFlagSpecs must append
// explicitly.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_ContainsTUIFlag(t *testing.T) {
	specs := cli.RunFlagSpecs()
	for _, s := range specs {
		if s.Name == "--tui" {
			if s.TakesValue {
				t.Error("RunFlagSpecs()[--tui].TakesValue = true, want false; --tui is a boolean flag")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain an entry for \"--tui\"; " +
		"it is the entry-point-only flag that RunFlagSpecs must append explicitly")
}

// TestRunFlagSpecs_ExecutablePathIsValueBearing verifies that "--executable-path" appears
// in RunFlagSpecs with TakesValue: true. This flag is the one directly involved
// in the mode-detection bug and must be in the value-bearing set so hasPositionalArg
// skips its value token.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_ExecutablePathIsValueBearing(t *testing.T) {
	specs := cli.RunFlagSpecs()
	for _, s := range specs {
		if s.Name == "--executable-path" {
			if !s.TakesValue {
				t.Error("RunFlagSpecs()[--executable-path].TakesValue = false, want true; " +
					"--executable-path consumes a following argument and must be value-bearing")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain an entry for \"--executable-path\"; " +
		"it is a value-bearing flag and must be declared in the arity map")
}

// TestRunFlagSpecs_PreConsultIsBoolean verifies that "--pre-consult" appears in
// RunFlagSpecs with TakesValue: false. If it were mistakenly marked as
// value-bearing, the token following --pre-consult would be skipped, and a
// genuine positional argument after it would be invisible to hasPositionalArg.
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_PreConsultIsBoolean(t *testing.T) {
	specs := cli.RunFlagSpecs()
	for _, s := range specs {
		if s.Name == "--pre-consult" {
			if s.TakesValue {
				t.Error("RunFlagSpecs()[--pre-consult].TakesValue = true, want false; " +
					"--pre-consult is a boolean flag and must not be value-bearing (AC5.5)")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain an entry for \"--pre-consult\"")
}

// TestRunFlagSpecs_KnownArities verifies that a representative set of flags
// from both the value-bearing and boolean categories carry the correct arity.
// This is a table-driven spot-check that catches systematic errors
// (e.g. all flags marked value-bearing by mistake).
//
// RED: RunFlagSpecs currently panics.
func TestRunFlagSpecs_KnownArities(t *testing.T) {
	wantValueBearing := []string{
		"--workflow",
		"--task",
		"--mode",
		"--checkpoints",
		"--commits",
		"--commit-branch",
		"--run",
		"--harness",
		"--timeout",
		"--executable-path",
		"--infra-class",
		"--input",
		"--review-loop-limit",
	}
	wantBoolean := []string{
		"--allow-version-drift",
		"--pre-consult",
		"--manual-resolution",
		"--new-run",
		"--tui",
	}

	specs := cli.RunFlagSpecs()
	specsMap := make(map[string]cli.FlagSpec, len(specs))
	for _, s := range specs {
		specsMap[s.Name] = s
	}

	for _, name := range wantValueBearing {
		spec, ok := specsMap[name]
		if !ok {
			t.Errorf("flag %q is absent from RunFlagSpecs(); it is value-bearing and must be declared", name)
			continue
		}
		if !spec.TakesValue {
			t.Errorf("RunFlagSpecs()[%q].TakesValue = false, want true", name)
		}
	}

	for _, name := range wantBoolean {
		spec, ok := specsMap[name]
		if !ok {
			t.Errorf("flag %q is absent from RunFlagSpecs(); it is boolean and must be declared", name)
			continue
		}
		if spec.TakesValue {
			t.Errorf("RunFlagSpecs()[%q].TakesValue = true, want false; boolean flags must not be value-bearing", name)
		}
	}
}

// TestValueBearingFlagNames_ContainsExpectedFlags verifies that
// ValueBearingFlagNames returns every flag known to be value-bearing. A missing
// entry means hasPositionalArg would not skip that flag's value and would
// falsely classify the value token as a positional argument.
//
// RED: ValueBearingFlagNames currently panics.
func TestValueBearingFlagNames_ContainsExpectedFlags(t *testing.T) {
	wantPresent := []string{
		"--workflow",
		"--task",
		"--mode",
		"--checkpoints",
		"--commits",
		"--commit-branch",
		"--run",
		"--harness",
		"--timeout",
		"--executable-path",
		"--infra-class",
		"--input",
		"--review-loop-limit",
		"--ghcp-permission-mode",
	}

	names := cli.ValueBearingFlagNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, want := range wantPresent {
		if !nameSet[want] {
			t.Errorf("ValueBearingFlagNames() does not contain %q; "+
				"hasPositionalArg will treat its value token as a positional argument", want)
		}
	}
}

// TestValueBearingFlagNames_DoesNotContainBooleanFlags verifies that
// ValueBearingFlagNames excludes every boolean flag. Including a boolean flag
// would cause hasPositionalArg to skip the token following it, hiding genuine
// positional arguments and inverting the bug (AC5.5).
//
// RED: ValueBearingFlagNames currently panics.
func TestValueBearingFlagNames_DoesNotContainBooleanFlags(t *testing.T) {
	boolFlags := []string{
		"--allow-version-drift",
		"--pre-consult",
		"--manual-resolution",
		"--new-run",
		"--tui",
	}

	names := cli.ValueBearingFlagNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, flag := range boolFlags {
		if nameSet[flag] {
			t.Errorf("ValueBearingFlagNames() contains %q; "+
				"boolean flags must not be in the value-bearing set — "+
				"including one would cause hasPositionalArg to skip the following token (AC5.5)", flag)
		}
	}
}

// TestValueBearingFlagNames_ConsistentWithRunFlagSpecs verifies that
// ValueBearingFlagNames() returns exactly the names from RunFlagSpecs() where
// TakesValue is true. If the two functions disagree, a pre-scan consumer using
// ValueBearingFlagNames and a consumer iterating RunFlagSpecs directly would
// make different decisions for the same args.
//
// RED: both functions currently panic.
func TestValueBearingFlagNames_ConsistentWithRunFlagSpecs(t *testing.T) {
	specs := cli.RunFlagSpecs()
	names := cli.ValueBearingFlagNames()

	wantNames := make(map[string]bool)
	for _, s := range specs {
		if s.TakesValue {
			wantNames[s.Name] = true
		}
	}

	gotNames := make(map[string]bool, len(names))
	for _, n := range names {
		gotNames[n] = true
	}

	for name := range wantNames {
		if !gotNames[name] {
			t.Errorf("ValueBearingFlagNames() is missing %q, which RunFlagSpecs() declares as TakesValue=true", name)
		}
	}
	for name := range gotNames {
		if !wantNames[name] {
			t.Errorf("ValueBearingFlagNames() contains %q, which RunFlagSpecs() does not declare as TakesValue=true", name)
		}
	}
}

// TestRunFlagSpecs_DriftFromActualRegistration is the structural drift test.
// It uses cli.RegisterRunFlags to populate a fresh pflag.FlagSet — the same
// function that cli.Run() calls internally — and then walks the registered flags
// via VisitAll, comparing each flag's actual arity (bool vs non-bool) against
// what RunFlagSpecs() declares.
//
// This test cannot be satisfied by updating a literal list in the test file:
// it passes only when RunFlagSpecs derives from the same declaration as the
// registration, ensuring both are automatically in sync when a flag is added
// or removed. That is the single-source-of-truth property Stage 5 requires.
//
// Expected asymmetries (not drift):
//   - "--tui" appears in RunFlagSpecs but not in the run subcommand registration;
//     it is the one entry-point-only flag, documented in the design.
//
// RED: cli.RegisterRunFlags and cli.RunFlagSpecs both currently panic.
func TestRunFlagSpecs_DriftFromActualRegistration(t *testing.T) {
	// Populate a fresh FlagSet using the same registration function that
	// cli.Run() calls. After the Stage 5 refactor, RegisterRunFlags is the
	// single place all run flags are declared.
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	cli.RegisterRunFlags(fs)

	specs := cli.RunFlagSpecs()
	specsMap := make(map[string]cli.FlagSpec, len(specs))
	for _, s := range specs {
		specsMap[s.Name] = s
	}

	// Every flag registered on the run subcommand must appear in RunFlagSpecs
	// with the correct arity (bool vs non-bool).
	fs.VisitAll(func(f *pflag.Flag) {
		name := "--" + f.Name
		spec, ok := specsMap[name]
		if !ok {
			t.Errorf("flag %q is registered on the run subcommand but absent from RunFlagSpecs(); "+
				"adding a flag without updating RunFlagSpecs restores the two-list drift this stage eliminates", name)
			return
		}
		takesValue := f.Value.Type() != "bool"
		if spec.TakesValue != takesValue {
			t.Errorf("flag %q: RunFlagSpecs().TakesValue = %v, registration says %v (type=%q); "+
				"arity mismatch between the declared spec and the actual cobra registration",
				name, spec.TakesValue, takesValue, f.Value.Type())
		}
	})

	// "--tui" is the one expected specs-only flag. Verify it is present in specs
	// with the correct arity and is genuinely absent from the run subcommand registration.
	if _, ok := specsMap["--tui"]; !ok {
		t.Error("RunFlagSpecs() does not contain \"--tui\"; " +
			"it is the entry-point-only boolean flag that RunFlagSpecs must append explicitly")
	}
	if fs.Lookup("tui") != nil {
		t.Error("\"--tui\" is registered on the run subcommand FlagSet; " +
			"it must be entry-point-only and absent from the run subcommand registration")
	}
}

// TestInfrastructureFlag_InRunFlagSpecs verifies that the --infrastructure
// flag appears in RunFlagSpecs() with TakesValue=true (it is a string flag
// that consumes the following token when passed in two-token form).
//
// TDD RED: fails until I5.1 registers the flag in RegisterRunFlags.
func TestInfrastructureFlag_InRunFlagSpecs(t *testing.T) {
	for _, s := range cli.RunFlagSpecs() {
		if s.Name == "--infrastructure" {
			if !s.TakesValue {
				t.Error("RunFlagSpecs()[\"--infrastructure\"].TakesValue = false, want true (string flag)")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain \"--infrastructure\"")
}

// TestDevTestModeFlag_InRunFlagSpecs_TakesValueFalse verifies that the hidden
// --dev-test-mode flag appears in RunFlagSpecs() with TakesValue=false (it is
// a bool flag). RunFlagSpecs uses VisitAll, which visits hidden flags, so
// --dev-test-mode must appear there even though it is hidden from help.
//
// TDD RED: fails until I5.1 registers the flag in RegisterRunFlags.
func TestDevTestModeFlag_InRunFlagSpecs_TakesValueFalse(t *testing.T) {
	for _, s := range cli.RunFlagSpecs() {
		if s.Name == "--dev-test-mode" {
			if s.TakesValue {
				t.Error("RunFlagSpecs()[\"--dev-test-mode\"].TakesValue = true, want false (bool flag)")
			}
			return
		}
	}
	t.Error("RunFlagSpecs() does not contain \"--dev-test-mode\"; " +
		"VisitAll visits hidden flags, so --dev-test-mode must appear in RunFlagSpecs")
}

// TestDevTestModeFlag_NotInAllValueBearingFlagNames verifies that --dev-test-mode
// does NOT appear in AllValueBearingFlagNames. Including a boolean flag in the
// value-bearing set would cause the pre-scan to incorrectly skip the following
// token, misidentifying it as a value.
//
// This test is trivially GREEN with the current implementation (the flag is not
// registered yet) and becomes a regression guard once I5.1 adds it.
func TestDevTestModeFlag_NotInAllValueBearingFlagNames(t *testing.T) {
	for _, name := range cli.AllValueBearingFlagNames() {
		if name == "--dev-test-mode" {
			t.Error("AllValueBearingFlagNames() contains \"--dev-test-mode\"; " +
				"boolean flags must not be in the value-bearing set (would cause pre-scan to skip the next token)")
			return
		}
	}
}

// TestInfrastructureFlag_InAllValueBearingFlagNames verifies that --infrastructure
// appears in AllValueBearingFlagNames (it is a value-bearing string flag whose
// value must not be misidentified as a positional argument by the pre-scan).
//
// TDD RED: fails until I5.1 registers the flag in RegisterRunFlags.
func TestInfrastructureFlag_InAllValueBearingFlagNames(t *testing.T) {
	for _, name := range cli.AllValueBearingFlagNames() {
		if name == "--infrastructure" {
			return
		}
	}
	t.Error("AllValueBearingFlagNames() does not contain \"--infrastructure\"; " +
		"string flags must be in the value-bearing set so the pre-scan skips the following token")
}
