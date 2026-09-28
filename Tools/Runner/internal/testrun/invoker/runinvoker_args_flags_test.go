// Tests for buildRunArgs core flags, executable path, GHCP permission mode and dev-test mode.
package invoker

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun"
)

// TestBuildRunArgs_ExecutablePath_IncludedWhenSet verifies that when
// RunInvocation.ExecutablePath is non-empty, buildRunArgs appends
// "--executable-path" followed by the path as consecutive elements in the
// returned args slice.
func TestBuildRunArgs_ExecutablePath_IncludedWhenSet(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:     "smoke-single",
		Mode:           "auto",
		Harness:        "claude-code",
		FixturePath:    "/catalog/Fixtures/smoke-single",
		Task:           "Test: smoke-single / auto / claude-code",
		ExecutablePath: "/usr/local/bin/claude",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--executable-path" && i+1 < len(args) && args[i+1] == "/usr/local/bin/claude" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args missing --executable-path /usr/local/bin/claude; full args: %v", args)
	}
}

// TestBuildRunArgs_ExecutablePath_OmittedWhenEmpty verifies that when
// RunInvocation.ExecutablePath is empty, --executable-path does not appear in
// the returned args slice (backwards-compatible zero value).
func TestBuildRunArgs_ExecutablePath_OmittedWhenEmpty(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:     "smoke-single",
		Mode:           "auto",
		Harness:        "claude-code",
		FixturePath:    "/catalog/Fixtures/smoke-single",
		Task:           "Test: smoke-single / auto / claude-code",
		ExecutablePath: "", // empty: must not produce the flag
	}
	args := buildRunArgs(inv)

	for _, a := range args {
		if a == "--executable-path" {
			t.Errorf("args contain --executable-path when ExecutablePath is empty; full args: %v", args)
		}
	}
}

// TestBuildRunArgs_ExecutablePath_SpacesPreservedAsSingleElement verifies that
// a path containing spaces (e.g. C:\Program Files\npm\claude.cmd) is emitted as
// a single argv element, not split on whitespace. This is the critical property
// that prevents shell word-splitting from corrupting the path when the child
// process is invoked.
func TestBuildRunArgs_ExecutablePath_SpacesPreservedAsSingleElement(t *testing.T) {
	pathWithSpaces := `C:\Program Files\npm\claude.cmd`
	inv := testrun.RunInvocation{
		WorkflowID:     "smoke-single",
		Mode:           "auto",
		Harness:        "claude-code",
		FixturePath:    "/catalog/Fixtures/smoke-single",
		Task:           "Test: smoke-single / auto / claude-code",
		ExecutablePath: pathWithSpaces,
	}
	args := buildRunArgs(inv)

	// Find --executable-path and verify the immediately following element is
	// the full path value (not split on spaces).
	for i, a := range args {
		if a == "--executable-path" {
			if i+1 >= len(args) {
				t.Fatalf("--executable-path is the last element; no value follows; full args: %v", args)
			}
			if args[i+1] != pathWithSpaces {
				t.Errorf("args[%d] (value after --executable-path) = %q, want %q\n(spaces in ExecutablePath must be preserved as a single argv element)",
					i+1, args[i+1], pathWithSpaces)
			}
			return
		}
	}
	t.Errorf("--executable-path flag not found in args %v", args)
}

// TestBuildRunArgs_ExecutablePath_EmittedBeforeGHCPPermissionMode verifies the
// ordering contract: when both ExecutablePath and GHCPPermissionMode are set,
// --executable-path appears before --ghcp-permission-mode in the args slice.
func TestBuildRunArgs_ExecutablePath_EmittedBeforeGHCPPermissionMode(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-ghcp",
		Mode:               "auto",
		Harness:            "ghcp-cli",
		FixturePath:        "/catalog/Fixtures/wf-ghcp",
		Task:               "Test: wf-ghcp / auto / ghcp-cli",
		ExecutablePath:     "/usr/local/bin/copilot",
		GHCPPermissionMode: "blanket",
	}
	args := buildRunArgs(inv)

	execPathIdx := -1
	ghcpIdx := -1
	for i, a := range args {
		if a == "--executable-path" && execPathIdx == -1 {
			execPathIdx = i
		}
		if a == "--ghcp-permission-mode" && ghcpIdx == -1 {
			ghcpIdx = i
		}
	}

	if execPathIdx == -1 {
		t.Fatalf("--executable-path not found in args %v", args)
	}
	if ghcpIdx == -1 {
		t.Fatalf("--ghcp-permission-mode not found in args %v", args)
	}
	if execPathIdx >= ghcpIdx {
		t.Errorf("--executable-path (index %d) must appear before --ghcp-permission-mode (index %d); full args: %v",
			execPathIdx, ghcpIdx, args)
	}
}

// TestBuildRunArgs_AllRequiredFlagsPresent verifies that buildRunArgs always
// includes the required flags: run, --workflow, --mode, --harness, --new-run,
// --input, --task.
func TestBuildRunArgs_AllRequiredFlagsPresent(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
	}
	args := buildRunArgs(inv)

	wantPairs := []struct{ flag, value string }{
		{"--workflow", "smoke-single"},
		{"--mode", "auto"},
		{"--harness", "claude-code"},
		{"--input", "/catalog/Fixtures/smoke-single"},
		{"--task", "Test: smoke-single / auto / claude-code"},
	}
	for _, pair := range wantPairs {
		foundFlag := false
		for i, a := range args {
			if a == pair.flag && i+1 < len(args) && args[i+1] == pair.value {
				foundFlag = true
				break
			}
		}
		if !foundFlag {
			t.Errorf("args missing %s %s; full args: %v", pair.flag, pair.value, args)
		}
	}

	// First element must be the "run" subcommand.
	if len(args) == 0 || args[0] != "run" {
		t.Errorf("args[0] = %q, want \"run\"", func() string {
			if len(args) > 0 {
				return args[0]
			}
			return "(empty)"
		}())
	}

	// --new-run must be present.
	foundNewRun := false
	for _, a := range args {
		if a == "--new-run" {
			foundNewRun = true
			break
		}
	}
	if !foundNewRun {
		t.Errorf("args missing --new-run flag; full args: %v", args)
	}
}

// TestBuildRunArgs_GHCPPermissionMode_IncludedWhenSet verifies that when
// GHCPPermissionMode is non-empty, the --ghcp-permission-mode flag and value
// are appended.
func TestBuildRunArgs_GHCPPermissionMode_IncludedWhenSet(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "ghcp-cli",
		FixturePath:        "/fixtures/wf-a",
		GHCPPermissionMode: "blanket",
		Task:               "Test: wf-a / auto / ghcp-cli",
	}
	args := buildRunArgs(inv)

	foundFlag := false
	for i, a := range args {
		if a == "--ghcp-permission-mode" && i+1 < len(args) && args[i+1] == "blanket" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Errorf("args missing --ghcp-permission-mode blanket; full args: %v", args)
	}
}

// TestBuildRunArgs_GHCPPermissionMode_OmittedWhenEmpty verifies that when
// GHCPPermissionMode is empty, --ghcp-permission-mode is not present in args.
func TestBuildRunArgs_GHCPPermissionMode_OmittedWhenEmpty(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		GHCPPermissionMode: "", // empty: must not produce the flag
		Task:               "Test: wf-a / auto / auto",
	}
	args := buildRunArgs(inv)

	for _, a := range args {
		if a == "--ghcp-permission-mode" {
			t.Errorf("args contain --ghcp-permission-mode when GHCPPermissionMode is empty; full args: %v", args)
		}
	}
}

// TestBuildRunArgs_AllFlagsAndValuesPresent verifies that all expected flag-value
// pairs are present when GHCPPermissionMode is set. Order among flags after the
// leading "run" subcommand is not checked -- the design does not mandate positional
// ordering.
func TestBuildRunArgs_AllFlagsAndValuesPresent(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "orchestrated-linear",
		Mode:               "auto-review",
		Harness:            "ghcp-cli",
		FixturePath:        "/catalog/Fixtures/orchestrated-linear",
		GHCPPermissionMode: "allowlist",
		Task:               "Test: orchestrated-linear / auto-review / ghcp-cli",
	}
	args := buildRunArgs(inv)

	// Verify all required flag-value pairs are present (order may vary after
	// the positional "run").
	checks := []struct {
		flag  string
		value string
	}{
		{"--workflow", "orchestrated-linear"},
		{"--mode", "auto-review"},
		{"--harness", "ghcp-cli"},
		{"--input", "/catalog/Fixtures/orchestrated-linear"},
		{"--ghcp-permission-mode", "allowlist"},
		{"--task", "Test: orchestrated-linear / auto-review / ghcp-cli"},
	}
	for _, c := range checks {
		found := false
		for i, a := range args {
			if a == c.flag && i+1 < len(args) && args[i+1] == c.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("buildRunArgs: missing %s %s in args %v", c.flag, c.value, args)
		}
	}
}

// TestBuildRunArgs_Task_SpacesPreservedAsSingleElement verifies that a Task
// value containing spaces is emitted as a single argv element, not split. This
// confirms the argv contract: --task and its value are adjacent elements in the
// returned slice with no shell quoting or word-splitting involved.
func TestBuildRunArgs_Task_SpacesPreservedAsSingleElement(t *testing.T) {
	taskValue := "Test: my workflow / auto / claude-code"
	inv := testrun.RunInvocation{
		WorkflowID:  "my workflow",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/fixtures/my-workflow",
		Task:        taskValue,
	}
	args := buildRunArgs(inv)

	// Find --task and verify the immediately following element is the full value.
	for i, a := range args {
		if a == "--task" {
			if i+1 >= len(args) {
				t.Fatalf("--task flag is the last element; no value follows; full args: %v", args)
			}
			if args[i+1] != taskValue {
				t.Errorf("args[%d] (value after --task) = %q, want %q\n(spaces in Task must be preserved as a single argv element, not split)",
					i+1, args[i+1], taskValue)
			}
			return
		}
	}
	t.Errorf("--task flag not found in args %v", args)
}

// TestBuildRunArgs_DevTestMode_AlwaysPresent verifies that buildRunArgs always
// emits --dev-test-mode in its output regardless of other RunInvocation fields.
// This is the subprocess-side signal that --infrastructure is accepted, and
// must be present in every invocation since buildRunArgs is only called by the
// test framework's SubprocessRunInvoker.
//
// TDD RED: fails until I5.3 adds --dev-test-mode to buildRunArgs.
func TestBuildRunArgs_DevTestMode_AlwaysPresent(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--dev-test-mode") {
		t.Errorf("buildRunArgs: missing --dev-test-mode in args %v\n"+
			"(--dev-test-mode must always be emitted; it is the subprocess-side signal "+
			"that --infrastructure is accepted)", args)
	}
}

// TestBuildRunArgs_DevTestMode_PresentWithNilInfrastructureKeys verifies that
// --dev-test-mode is emitted even when InfrastructureKeys is nil (the workflow
// declares no infrastructure agents). The flag is always emitted regardless of
// whether --infrastructure is also emitted.
//
// TDD RED: fails until I5.3 adds --dev-test-mode to buildRunArgs.
func TestBuildRunArgs_DevTestMode_PresentWithNilInfrastructureKeys(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: nil,
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--dev-test-mode") {
		t.Errorf("buildRunArgs: missing --dev-test-mode when InfrastructureKeys=nil; "+
			"full args: %v", args)
	}
}

// TestBuildRunArgs_DevTestMode_PresentAlongsideInfrastructureKeys verifies that
// --dev-test-mode is emitted together with --infrastructure=k1 when
// InfrastructureKeys is populated. Both flags must appear in the output so the
// subprocess accepts and applies the infrastructure filter.
//
// TDD RED: fails until I5.3 adds --dev-test-mode to buildRunArgs.
func TestBuildRunArgs_DevTestMode_PresentAlongsideInfrastructureKeys(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{"mosaictest-review"},
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--dev-test-mode") {
		t.Errorf("buildRunArgs: missing --dev-test-mode alongside --infrastructure; "+
			"full args: %v", args)
	}
	if !containsArg(args, "--infrastructure=mosaictest-review") {
		t.Errorf("buildRunArgs: missing --infrastructure=mosaictest-review in args %v", args)
	}
}

// TestBuildRunArgs_ReviewLoopLimit_AlwaysPresentWithAcceptedValue verifies that
// buildRunArgs passes --review-loop-limit, which the run subcommand requires
// for a new run, with a value the flag accepts (a positive integer or none).
func TestBuildRunArgs_ReviewLoopLimit_AlwaysPresentWithAcceptedValue(t *testing.T) {
	inv := testrun.RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
	}

	args := buildRunArgs(inv)

	value := ""
	for i, a := range args {
		if a == "--review-loop-limit" && i+1 < len(args) {
			value = args[i+1]
		}
	}
	if value == "" {
		t.Fatalf("--review-loop-limit missing from args; the run subcommand refuses a new run without it; full args: %v", args)
	}
	if _, err := domain.ParseReviewLoopLimit(value); err != nil {
		t.Errorf("--review-loop-limit value %q is not accepted by the flag: %v", value, err)
	}
}
