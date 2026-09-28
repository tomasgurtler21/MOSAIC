// Tests for buildRunArgs pre-consult, infrastructure, checkpoints and commits flags.
package invoker

import (
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

// TestBuildRunArgs_PreConsultTrue_EmitsFlag verifies that when
// RunInvocation.PreConsult is true, buildRunArgs emits --pre-consult=true in
// the argument list.
func TestBuildRunArgs_PreConsultTrue_EmitsFlag(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
		PreConsult:  true,
	}
	args := buildRunArgs(inv)

	wantFlag := "--pre-consult=true"
	found := false
	for _, a := range args {
		if a == wantFlag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("buildRunArgs with PreConsult=true: %q not found in args %v\n(--pre-consult flag must always be emitted with the boolean value from testrun.RunInvocation.PreConsult)",
			wantFlag, args)
	}

	// Also ensure the false variant is absent so both variants are unambiguous.
	for _, a := range args {
		if a == "--pre-consult=false" {
			t.Errorf("buildRunArgs with PreConsult=true: --pre-consult=false unexpectedly present in args %v", args)
		}
	}
}

// TestBuildRunArgs_PreConsultFalse_EmitsFlag verifies that when
// RunInvocation.PreConsult is false, buildRunArgs emits --pre-consult=false in
// the argument list.
func TestBuildRunArgs_PreConsultFalse_EmitsFlag(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
		PreConsult:  false,
	}
	args := buildRunArgs(inv)

	wantFlag := "--pre-consult=false"
	found := false
	for _, a := range args {
		if a == wantFlag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("buildRunArgs with PreConsult=false: %q not found in args %v\n(--pre-consult flag must always be emitted with the boolean value from testrun.RunInvocation.PreConsult)",
			wantFlag, args)
	}

	// Also ensure the true variant is absent.
	for _, a := range args {
		if a == "--pre-consult=true" {
			t.Errorf("buildRunArgs with PreConsult=false: --pre-consult=true unexpectedly present in args %v", args)
		}
	}
}

// TestBuildRunArgs_PreConsult_UsesEqualsFormat verifies that the --pre-consult
// flag uses the = separator format (--pre-consult=true or --pre-consult=false)
// rather than a space-separated value (--pre-consult true). This is the format
// expected by the subprocess's scanBoolFlagDefault parser.
func TestBuildRunArgs_PreConsult_UsesEqualsFormat(t *testing.T) {
	for _, preConsult := range []bool{true, false} {
		inv := testrun.RunInvocation{
			WorkflowID:  "wf-a",
			Mode:        "auto",
			Harness:     "auto",
			FixturePath: "/fixtures/wf-a",
			Task:        "Test: wf-a / auto / auto",
			PreConsult:  preConsult,
		}
		args := buildRunArgs(inv)

		// "--pre-consult" must not appear as a standalone arg (space-separated format).
		for _, a := range args {
			if a == "--pre-consult" {
				t.Errorf("PreConsult=%v: --pre-consult appears as standalone arg (without = separator); want --pre-consult=true or --pre-consult=false; full args: %v",
					preConsult, args)
			}
		}

		// The = format must be present.
		wantFlag := "--pre-consult=" + strconv.FormatBool(preConsult)
		found := false
		for _, a := range args {
			if a == wantFlag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("PreConsult=%v: %q not found in args %v", preConsult, wantFlag, args)
		}
	}
}

// TestBuildRunArgs_Infrastructure_Nil_OmitsFlag verifies that when
// RunInvocation.InfrastructureKeys is nil, buildRunArgs does NOT include any
// --infrastructure token in the args slice (backwards-compat: callers that do
// not set the field behave as before).
//
// TDD RED: fails until I3.2 updates buildRunArgs to emit --infrastructure.
func TestBuildRunArgs_Infrastructure_Nil_OmitsFlag(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: nil, // nil: must omit flag entirely
	}
	args := buildRunArgs(inv)

	for _, a := range args {
		if strings.HasPrefix(a, "--infrastructure") {
			t.Errorf("InfrastructureKeys=nil: --infrastructure must be omitted; got arg %q in %v",
				a, args)
		}
	}
}

// TestBuildRunArgs_Infrastructure_NonNilEmpty_EmitsSingleTokenEmptyValue verifies
// that when InfrastructureKeys is non-nil but empty ([]string{}), buildRunArgs
// appends the single token "--infrastructure=" (with empty value after the =).
// This single-token form is required because pflag parses --infrastructure=
// as Changed()==true with value "", distinguishing it from "flag not passed".
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_NonNilEmpty_EmitsSingleTokenEmptyValue(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{}, // non-nil empty: emit --infrastructure=
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--infrastructure=") {
		t.Errorf("InfrastructureKeys=[]string{}: want --infrastructure= (single token with empty value) in args %v", args)
	}
}

// TestBuildRunArgs_Infrastructure_SingleKey_EmitsSingleTokenWithValue verifies
// that a single InfrastructureKey is emitted as "--infrastructure=key" (= form).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_SingleKey_EmitsSingleTokenWithValue(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{"mosaictest-review"},
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--infrastructure=mosaictest-review") {
		t.Errorf("InfrastructureKeys=[\"mosaictest-review\"]: want --infrastructure=mosaictest-review in args %v", args)
	}
}

// TestBuildRunArgs_Infrastructure_MultipleKeys_EmitsCommaJoined verifies that
// multiple InfrastructureKeys are joined with commas in the single token:
// "--infrastructure=k1,k2" (not two separate tokens).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_MultipleKeys_EmitsCommaJoined(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{"mosaictest-checkpoint", "mosaictest-review"},
	}
	args := buildRunArgs(inv)

	want := "--infrastructure=mosaictest-checkpoint,mosaictest-review"
	if !containsArg(args, want) {
		t.Errorf("InfrastructureKeys=[checkpoint, review]: want %q in args %v", want, args)
	}
}

// TestBuildRunArgs_Infrastructure_WorkflowWithoutAgents_EmitsEmptyToken covers
// the case where a workflow has no infrastructure_agents in its frontmatter.
// Stage 2 guarantees CatalogEntry.InfrastructureAgents is []string{} (non-nil
// empty) for such workflows, so Orchestrator.Run sets InfrastructureKeys to
// that same non-nil empty slice, and buildRunArgs must emit --infrastructure=.
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_WorkflowWithoutAgents_EmitsEmptyToken(t *testing.T) {
	// InfrastructureKeys is explicitly set to non-nil empty to simulate the
	// output of Orchestrator.Run for a workflow without infrastructure_agents.
	inv := testrun.RunInvocation{
		WorkflowID:         "simple-workflow",
		Mode:               "auto",
		Harness:            "claude-code",
		FixturePath:        "/fixtures/simple-workflow",
		Task:               "Test: simple-workflow / auto / claude-code",
		InfrastructureKeys: []string{},
	}
	args := buildRunArgs(inv)

	// The subprocess must receive --infrastructure= so pflag sets
	// Changed("infrastructure")==true with value "", activating the empty filter
	// (no agents active). Without this token the subprocess uses all declared agents.
	if !containsArg(args, "--infrastructure=") {
		t.Errorf("workflow without infrastructure_agents (InfrastructureKeys=[]string{}): "+
			"want --infrastructure= in args %v\n"+
			"(absence would leave all deployed agents active for this subprocess)", args)
	}
}

// TestBuildRunArgs_Checkpoints_Enabled_EmitsEnabled verifies that when
// RunInvocation.Checkpoints is "enabled", buildRunArgs emits
// "--checkpoints" followed by "enabled" as two consecutive tokens.
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Checkpoints_Enabled_EmitsEnabled(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Checkpoints: "enabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--checkpoints" && i+1 < len(args) && args[i+1] == "enabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Checkpoints=enabled: want --checkpoints enabled in args %v", args)
	}
}

// TestBuildRunArgs_Checkpoints_Disabled_EmitsDisabled verifies that when
// RunInvocation.Checkpoints is "disabled", buildRunArgs emits
// "--checkpoints" followed by "disabled".
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Checkpoints_Disabled_EmitsDisabled(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Checkpoints: "disabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--checkpoints" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Checkpoints=disabled: want --checkpoints disabled in args %v", args)
	}
}

// TestBuildRunArgs_Checkpoints_EmptyString_EmitsDisabled verifies that when
// RunInvocation.Checkpoints is "" (empty string), buildRunArgs emits
// "--checkpoints disabled" (empty string maps to "disabled" as defense-in-depth).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Checkpoints_EmptyString_EmitsDisabled(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Checkpoints: "", // empty: must default to "disabled"
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--checkpoints" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Checkpoints=\"\" (empty): want --checkpoints disabled in args %v\n"+
			"(empty string must map to \"disabled\" to avoid the subprocess rejecting an empty value)",
			args)
	}
}

// TestBuildRunArgs_Commits_Enabled_EmitsEnabled verifies that when
// RunInvocation.Commits is "enabled", buildRunArgs emits "--commits" followed
// by "enabled" as two consecutive tokens.
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Commits_Enabled_EmitsEnabled(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Commits:     "enabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--commits" && i+1 < len(args) && args[i+1] == "enabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commits=enabled: want --commits enabled in args %v", args)
	}
}

// TestBuildRunArgs_Commits_Disabled_EmitsDisabled verifies that when
// RunInvocation.Commits is "disabled", buildRunArgs emits "--commits" followed
// by "disabled".
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Commits_Disabled_EmitsDisabled(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Commits:     "disabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--commits" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commits=disabled: want --commits disabled in args %v", args)
	}
}

// TestBuildRunArgs_Commits_EmptyString_EmitsDisabled verifies that when
// RunInvocation.Commits is "" (empty string), buildRunArgs emits
// "--commits disabled" (empty string maps to "disabled" as defense-in-depth).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Commits_EmptyString_EmitsDisabled(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Commits:     "", // empty: must default to "disabled"
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--commits" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commits=\"\" (empty): want --commits disabled in args %v\n"+
			"(empty string must map to \"disabled\" to avoid the subprocess rejecting an empty value)",
			args)
	}
}
