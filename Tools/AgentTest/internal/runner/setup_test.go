package runner_test

// Setup-content tests: the run identity is authored before the subject is
// spawned, the active stub registry and the parallel-group document are
// written into the control directory in the shape the interceptor expects,
// declared seed files are placed, and run state is initialized with the
// declared early-exit threshold and turn limit.
//
// Every assertion here runs from inside a launcher callback: Launch is
// invoked after setup has completed and before teardown removes anything,
// which is the only point at which setup's output can still be inspected.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/interceptor"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/runstate"
)

func intPtr(n int) *int { return &n }

func TestRun_SetupWritesActiveRegistryAndParallelGroupsDocuments(t *testing.T) {
	h := newHarness(t)
	req := newRequest("setup-content")
	req.Test.Registry = domain.StubRegistry{
		TestID: req.Test.Definition.ID,
		Stubs: []domain.Stub{
			{Match: domain.StubMatch{Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}, Invocation: 1}},
		},
	}
	req.Test.Definition.ParallelGroups = []domain.ParallelGroup{
		{Name: "group-a", Members: []domain.CollaboratorIdentity{{ToolName: "Task", AgentIdentity: "researcher"}}},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox

		reg, err := interceptor.LoadRegistry(sb.ControlDir)
		if err != nil {
			t.Errorf("LoadRegistry returned unexpected error: %v", err)
		} else if reg.TestID != req.Test.Definition.ID {
			t.Errorf("active registry TestID = %q, want %q", reg.TestID, req.Test.Definition.ID)
		}

		groups, err := interceptor.LoadGroups(sb.ControlDir)
		if err != nil {
			t.Errorf("LoadGroups returned unexpected error: %v", err)
		} else if len(groups) != 1 || groups[0].Name != "group-a" {
			t.Errorf("parallel groups = %+v, want one group named %q", groups, "group-a")
		}

		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

func TestRun_SetupSeedsDeclaredFilesBeforeSpawningTheSubject(t *testing.T) {
	h := newHarness(t)
	req := newRequest("setup-seed-files")
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "notes/plan.md", Content: "seeded plan content"},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox

		got, err := os.ReadFile(filepath.Join(sb.SubjectDir, "notes", "plan.md"))
		if err != nil {
			t.Errorf("expected the seeded file to exist before the subject is spawned: %v", err)
		} else if string(got) != "seeded plan content" {
			t.Errorf("seeded file content = %q, want %q", got, "seeded plan content")
		}

		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

func TestRun_SetupInitializesRunStateWithTheDeclaredLimits(t *testing.T) {
	h := newHarness(t)
	req := newRequest("setup-run-state")
	req.Settings.StopAfterInvocations = intPtr(2)
	req.Settings.TurnLimit = intPtr(5)

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox

		store := runstate.NewStore(sb.ControlDir, h.Clock)
		state, err := store.Read()
		if err != nil {
			t.Errorf("runstate.Read returned unexpected error: %v", err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		if state.EarlyExitThreshold != 2 {
			t.Errorf("state.EarlyExitThreshold = %d, want 2", state.EarlyExitThreshold)
		}
		if state.TurnLimit != 5 {
			t.Errorf("state.TurnLimit = %d, want 5", state.TurnLimit)
		}
		if state.TestID != req.Test.Definition.ID {
			t.Errorf("state.TestID = %q, want %q", state.TestID, req.Test.Definition.ID)
		}
		if state.RunNumber != req.Key.RunNumber {
			t.Errorf("state.RunNumber = %d, want %d", state.RunNumber, req.Key.RunNumber)
		}

		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

// TestRun_SetupProvisionRequestCarriesTheInterceptorSubcommand asserts that
// the request runner.setup builds carries the shared interceptor subcommand
// in InterceptorArgs, sourced from domain.InterceptorSubcommand rather than a
// private literal or an unset field. Left unset, the registered hook the
// adapter builds from this request falls through to the CLI frontend instead
// of reaching the interceptor.
func TestRun_SetupProvisionRequestCarriesTheInterceptorSubcommand(t *testing.T) {
	h := newHarness(t)
	req := newRequest("provision-interceptor-subcommand")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	got := h.Adapter.lastProvisionReq.InterceptorArgs
	if len(got) == 0 || got[0] != domain.InterceptorSubcommand {
		t.Errorf("ProvisionRequest.InterceptorArgs = %v, want it to begin with domain.InterceptorSubcommand %q", got, domain.InterceptorSubcommand)
	}
}

// TestRun_SetupProvisionRequestCarriesTheResolvedInterpreter asserts that
// the request runner.setup builds carries Deps.InterpreterCmd verbatim in
// InterpreterCmd — the interpreter the composition root already resolved
// during preflight and carried into Deps — rather than leaving the field
// unset regardless of what Deps holds.
func TestRun_SetupProvisionRequestCarriesTheResolvedInterpreter(t *testing.T) {
	h := newHarness(t)
	h.Deps.InterpreterCmd = "py"
	req := newRequest("provision-interpreter-cmd")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	got := h.Adapter.lastProvisionReq.InterpreterCmd
	if got != h.Deps.InterpreterCmd {
		t.Errorf("ProvisionRequest.InterpreterCmd = %q, want Deps.InterpreterCmd %q carried verbatim", got, h.Deps.InterpreterCmd)
	}
}

// TestRun_SetupProvisionRequestLeavesInterpreterCmdEmptyWhenUnresolved
// asserts that when Deps.InterpreterCmd is left at its zero value — the
// documented legal case for an adapter that runs no interpreter — the built
// ProvisionRequest.InterpreterCmd stays empty rather than being defaulted or
// hardcoded to some literal (e.g. a fallback interpreter name) inside setup.
// This is the "never hardcoded" half of the interpreter-carrying contract;
// TestRun_SetupProvisionRequestCarriesTheResolvedInterpreter above only
// covers the "carried when present" half.
func TestRun_SetupProvisionRequestLeavesInterpreterCmdEmptyWhenUnresolved(t *testing.T) {
	h := newHarness(t)
	// h.Deps.InterpreterCmd left unset (zero value) deliberately.
	req := newRequest("provision-interpreter-cmd-unset")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	got := h.Adapter.lastProvisionReq.InterpreterCmd
	if got != "" {
		t.Errorf("ProvisionRequest.InterpreterCmd = %q, want empty string carried verbatim from an unset Deps.InterpreterCmd (never hardcoded)", got)
	}
}

func TestRun_RunIdentityIsAuthoredBeforeTheSubjectIsSpawned(t *testing.T) {
	h := newHarness(t)
	req := newRequest("setup-run-identity")

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		if sb.Key.RunID == "" {
			t.Error("sandbox.Key.RunID is empty at spawn time, want the run identity authored before the subject is spawned")
		}
		if sb.LogRoot() == "" {
			t.Error("sandbox.LogRoot() is empty, want the log location to be known before spawning")
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}
