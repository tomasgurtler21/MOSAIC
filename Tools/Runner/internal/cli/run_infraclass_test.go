package cli_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// TestInfraClassFlag_SingleMapping_ParsedIntoRunConfig verifies that a single
// key=value pair supplied via --infra-class is parsed into
// RunConfig.InfraClassSelections with the correct class name and agent name.
func TestInfraClassFlag_SingleMapping_ParsedIntoRunConfig(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := append(baseHarnessArgs(),
		"--infra-class", "checkpoint=checkpoint-manager-git",
	)
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}

	got := sess.config.InfraClassSelections
	if got == nil {
		t.Fatal("InfraClassSelections is nil; want a non-nil map populated from --infra-class")
	}
	if got["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections[checkpoint] = %q, want %q",
			got["checkpoint"], "checkpoint-manager-git")
	}
}

// TestInfraClassFlag_MultipleClassMappings_AllParsed verifies that when multiple
// class=agent pairs are provided (either comma-separated in one flag or via multiple
// --infra-class flags), all pairs are present in RunConfig.InfraClassSelections.
func TestInfraClassFlag_MultipleClassMappings_AllParsed(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	// Comma-separated format: checkpoint=name1,commit=name2
	args := append(baseHarnessArgs(),
		"--infra-class", "checkpoint=checkpoint-manager-git,commit=commit-manager-git",
	)
	code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}

	got := sess.config.InfraClassSelections
	if got == nil {
		t.Fatal("InfraClassSelections is nil; want non-nil map with both class mappings")
	}
	if got["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections[checkpoint] = %q, want %q",
			got["checkpoint"], "checkpoint-manager-git")
	}
	if got["commit"] != "commit-manager-git" {
		t.Errorf("InfraClassSelections[commit] = %q, want %q",
			got["commit"], "commit-manager-git")
	}
}

// TestInfraClassFlag_MalformedValue_RejectsWithUsageError verifies that an
// --infra-class value that is not in "class=agent" format (no "=" separator)
// causes the CLI to reject with ExitUsage and not call session.Start.
func TestInfraClassFlag_MalformedValue_RejectsWithUsageError(t *testing.T) {
	sess := &scriptedSession{}
	args := append(baseHarnessArgs(),
		"--infra-class", "not-a-valid-pair",
	)
	code, _, _ := runCLI(t, args, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for malformed --infra-class value",
			code, cli.ExitUsage)
	}
	if sess.called {
		t.Error("session.Start must not be called when --infra-class value is malformed")
	}
}

// TestInfraClassFlag_HelpTextMentionsFlag verifies that the --infra-class flag
// appears in the run subcommand's help text after implementation.
func TestInfraClassFlag_HelpTextMentionsFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--infra-class") {
		t.Errorf("help text does not mention --infra-class; got:\n%s", errOut)
	}
}

// TestInputFlag_ZeroOccurrences_SeedInputsIsNil verifies that omitting --input
// entirely leaves RunConfig.SeedInputs nil. This is a baseline / regression guard:
// runs without any seed input must behave exactly as before.
func TestInputFlag_ZeroOccurrences_SeedInputsIsNil(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, _ := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess", code)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.SeedInputs != nil {
		t.Errorf("SeedInputs = %v, want nil when --input is not supplied", sess.config.SeedInputs)
	}
}

// TestInputFlag_OneOccurrence_SingleValueReachesSeedInputs verifies that a single
// --input occurrence produces a one-element SeedInputs slice containing the
// supplied path verbatim.
func TestInputFlag_OneOccurrence_SingleValueReachesSeedInputs(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--input", "/path/to/seed.md",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if len(sess.config.SeedInputs) != 1 {
		t.Fatalf("len(SeedInputs) = %d, want 1; got %v", len(sess.config.SeedInputs), sess.config.SeedInputs)
	}
	if sess.config.SeedInputs[0] != "/path/to/seed.md" {
		t.Errorf("SeedInputs[0] = %q, want %q", sess.config.SeedInputs[0], "/path/to/seed.md")
	}
}

// TestInputFlag_MultipleOccurrences_AllValuesReachSeedInputsInOrder verifies that
// repeated --input occurrences append all paths to SeedInputs in the order given.
func TestInputFlag_MultipleOccurrences_AllValuesReachSeedInputsInOrder(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--input", "/alpha/first.md",
		"--input", "/beta/second.md",
		"--input", "/gamma/third.md",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	want := []string{"/alpha/first.md", "/beta/second.md", "/gamma/third.md"}
	if len(sess.config.SeedInputs) != len(want) {
		t.Fatalf("len(SeedInputs) = %d, want %d; got %v",
			len(sess.config.SeedInputs), len(want), sess.config.SeedInputs)
	}
	for i, w := range want {
		if sess.config.SeedInputs[i] != w {
			t.Errorf("SeedInputs[%d] = %q, want %q", i, sess.config.SeedInputs[i], w)
		}
	}
}

// TestInputFlag_PathWithSpaces_PreservedVerbatim verifies that a path containing
// spaces is stored as a single element with the exact original content.
func TestInputFlag_PathWithSpaces_PreservedVerbatim(t *testing.T) {
	const pathWithSpaces = "/path/with spaces/seed file.md"
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--input", pathWithSpaces,
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if len(sess.config.SeedInputs) != 1 || sess.config.SeedInputs[0] != pathWithSpaces {
		t.Errorf("SeedInputs = %v, want [%q]; path with spaces must be preserved verbatim",
			sess.config.SeedInputs, pathWithSpaces)
	}
}

// TestInputFlag_PathWithComma_PreservedVerbatim verifies that a path containing a
// comma is stored as a single element. A delimiter-split flag shape (like the
// --infra-class flag) would break such a path into two elements; StringArrayVar
// is required to prevent this.
func TestInputFlag_PathWithComma_PreservedVerbatim(t *testing.T) {
	const pathWithComma = "/path/with,comma/seed.md"
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--input", pathWithComma,
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if len(sess.config.SeedInputs) != 1 || sess.config.SeedInputs[0] != pathWithComma {
		t.Errorf("SeedInputs = %v, want [%q]; comma in path must not split the element",
			sess.config.SeedInputs, pathWithComma)
	}
}

// TestInputFlag_HelpTextMentionsFlag verifies that the run subcommand's help text
// names --input after the flag is registered (I2.2).
func TestInputFlag_HelpTextMentionsFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--input") {
		t.Errorf("help text does not mention --input; got:\n%s", errOut)
	}
}

// TestInputAndRunFlags_MutuallyExclusive_RejectsWithUsageError verifies that
// supplying both --input and --run yields ExitUsage with a message naming the
// mutual exclusion, and that session.Start is never called.
func TestInputAndRunFlags_MutuallyExclusive_RejectsWithUsageError(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when --input and --run are both supplied",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not contain \"mutually exclusive\"; the error must name the conflict",
			errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when --input and --run are both supplied")
	}
}

// TestInputAndRunFlags_MutuallyExclusive_ErrorNamesInputFlag verifies that the
// mutual-exclusion error explicitly names --input so the user can identify which
// flags are in conflict.
func TestInputAndRunFlags_MutuallyExclusive_ErrorNamesInputFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	// Both conditions must hold: the error must be a mutual-exclusion message AND
	// it must name --input. Checking "mutually exclusive" prevents this test from
	// passing coincidentally when --input is not yet registered and cobra emits
	// "unknown flag: --input" (which happens to contain the --input substring but
	// is not the intended mutual-exclusion error).
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not contain \"mutually exclusive\"; the error must be the mutual-exclusion message, not cobra's unknown-flag rejection", errOut)
	}
	if !strings.Contains(errOut, "--input") {
		t.Errorf("stderr %q does not name --input in the mutual-exclusion error", errOut)
	}
}

// TestInputAndRunFlags_MutuallyExclusive_ErrorNamesRunFlag verifies that the
// mutual-exclusion error explicitly names --run so the user can identify which
// flags are in conflict.
func TestInputAndRunFlags_MutuallyExclusive_ErrorNamesRunFlag(t *testing.T) {
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", testRunID,
	}, sess)

	if !strings.Contains(errOut, "--run") {
		t.Errorf("stderr %q does not name --run in the mutual-exclusion error", errOut)
	}
}
