package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Seeding: files copied into run folder before first dispatch =====
//
// These integration tests exercise the session's seed-application step against
// the real file-based artifact store. Seeded files must appear at their correct
// relative paths inside the run folder, and they must be present before the
// first agent row is dispatched (Apply runs before the dispatch loop).
//
// Run-folder layout used in all seeding tests:
//   dir/                         ← orchestrator and agent definition files
//   dir/run/                     ← run folder (created by fileStore.Create)
//   dir/run/Orchestration.md     ← artifact (created by fileStore.Create)
//   dir/run/<seeded files>       ← files placed by seed.Apply
//
// RED phase: all tests fail until seed.Apply is inserted into the session's
// run-start sequence, after Store.Create and before the dispatch loop.

// seedDispatchCallbackHarness wraps a MockAdapter and calls onInvoke immediately
// before each agent invocation. Used to inspect filesystem state at dispatch time.
type seedDispatchCallbackHarness struct {
	delegate *harness.MockAdapter
	onInvoke func(agentID string)
}

func (h *seedDispatchCallbackHarness) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	if h.onInvoke != nil {
		h.onInvoke(agent.Identifier)
	}
	return h.delegate.Invoke(ctx, agent, request)
}

// TestIntegration_Seeding_SingleFile_PresentBeforeFirstDispatch verifies that a
// single-file seed source is present in the run folder — with byte-identical
// content to the source — when the first agent row is dispatched.
func TestIntegration_Seeding_SingleFile_PresentBeforeFirstDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	seedContent := "# Requirements\n\nThis is the seeded content.\n"
	srcFile := filepath.Join(dir, "Requirements.md")
	if err := os.WriteFile(srcFile, []byte(seedContent), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}

	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewMockAdapter()

	// Capture the run-folder state at the moment the first agent is dispatched.
	var seededExistsAtDispatch bool
	var seededContentAtDispatch []byte
	cbH := &seedDispatchCallbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				got, err := os.ReadFile(filepath.Join(runDir, "Requirements.md"))
				seededExistsAtDispatch = err == nil
				seededContentAtDispatch = got
			}
		},
	}

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  cbH,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{srcFile},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// The seeded file must have been present when agent-a was dispatched.
	if !seededExistsAtDispatch {
		t.Error("seeded file must exist in the run folder before the first row is dispatched, but it was absent")
	}
	if string(seededContentAtDispatch) != seedContent {
		t.Errorf("seeded file content at first dispatch: got %q, want %q",
			string(seededContentAtDispatch), seedContent)
	}
}

// TestIntegration_Seeding_DirectorySource_FilesAtCorrectRelativePaths verifies
// that a directory seed source contributes one entry per regular file, with
// destination paths preserving the relative structure of the source directory.
func TestIntegration_Seeding_DirectorySource_FilesAtCorrectRelativePaths(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Source directory:
	//   srcDir/
	//     Top.md
	//     Requirements.md   (top-level Requirement* candidate, so seeding is not refused)
	//     Sub/
	//       Nested.md
	srcDir := filepath.Join(dir, "seed-src")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir srcDir: %v", err)
	}
	topContent := "# Top level\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Top.md"), []byte(topContent), 0600); err != nil {
		t.Fatalf("write Top.md: %v", err)
	}
	// Requirement* candidate at the top level of the directory source, so the
	// combined candidate pool has exactly one match and seeding is not refused.
	if err := os.WriteFile(filepath.Join(srcDir, "Requirements.md"), []byte("# Requirements\n"), 0600); err != nil {
		t.Fatalf("write Requirements.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "Sub"), 0700); err != nil {
		t.Fatalf("mkdir Sub: %v", err)
	}
	nestedContent := "# Nested\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Sub", "Nested.md"), []byte(nestedContent), 0600); err != nil {
		t.Fatalf("write Nested.md: %v", err)
	}

	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{srcDir},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Top.md must be at the root of the run folder.
	if gotTop, err := os.ReadFile(filepath.Join(runDir, "Top.md")); err != nil {
		t.Errorf("want seeded Top.md in run folder: %v", err)
	} else if string(gotTop) != topContent {
		t.Errorf("Top.md content: got %q, want %q", string(gotTop), topContent)
	}

	// Nested.md must be at Sub/Nested.md relative to the run folder.
	if gotNested, err := os.ReadFile(filepath.Join(runDir, "Sub", "Nested.md")); err != nil {
		t.Errorf("want seeded Sub/Nested.md in run folder: %v", err)
	} else if string(gotNested) != nestedContent {
		t.Errorf("Sub/Nested.md content: got %q, want %q", string(gotNested), nestedContent)
	}
}

// TestIntegration_Seeding_MultipleSources_AllFilesPresent verifies that when
// several sources are provided, every seeded file from every source is present
// in the run folder with byte-identical content to its source.
func TestIntegration_Seeding_MultipleSources_AllFilesPresent(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Source 1: individual file.
	content1 := "# Input 1\n"
	src1 := filepath.Join(dir, "Input1.md")
	if err := os.WriteFile(src1, []byte(content1), 0600); err != nil {
		t.Fatalf("write src1: %v", err)
	}

	// Source 2: individual file.
	content2 := "# Input 2\n"
	src2 := filepath.Join(dir, "Input2.md")
	if err := os.WriteFile(src2, []byte(content2), 0600); err != nil {
		t.Fatalf("write src2: %v", err)
	}

	// Source 3: directory containing one file.
	srcDir := filepath.Join(dir, "seed-dir")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir srcDir: %v", err)
	}
	content3 := "# Dir Input\n"
	if err := os.WriteFile(filepath.Join(srcDir, "DirInput.md"), []byte(content3), 0600); err != nil {
		t.Fatalf("write DirInput.md: %v", err)
	}

	// Source 4: individual file. This is the sole Requirement* candidate across
	// the combined source set, so seeding is not refused for lacking one.
	content4 := "# Requirements\n"
	src4 := filepath.Join(dir, "Requirements.md")
	if err := os.WriteFile(src4, []byte(content4), 0600); err != nil {
		t.Fatalf("write src4: %v", err)
	}

	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{src1, src2, srcDir, src4},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, tc := range []struct{ name, dest, content string }{
		{"src1", "Input1.md", content1},
		{"src2", "Input2.md", content2},
		{"srcDir/DirInput.md", "DirInput.md", content3},
		{"src4", "Requirements.md", content4},
	} {
		gotContent, err := os.ReadFile(filepath.Join(runDir, tc.dest))
		if err != nil {
			t.Errorf("want seeded %s at run folder/%s: %v", tc.name, tc.dest, err)
			continue
		}
		if string(gotContent) != tc.content {
			t.Errorf("%s content: got %q, want %q", tc.dest, string(gotContent), tc.content)
		}
	}
}

// ===== Seeding: a seeded stage table dispatches on the first attempt =====

// TestIntegration_Seeding_StagedWorkflow_SeededStageTable_DispatchesFirstAttempt
// verifies the Stage 5 defect's absence directly: a staged workflow whose
// stage table arrives via SeedInputs (never pre-populated on disk by the
// test itself) dispatches successfully on a single Start call -- no stop, no
// start-then-resume sequence, and the stage-driven dispatches actually
// occur. This is the coverage gap the defect exploited: nothing previously
// seeded a stage table into a brand-new staged run and then dispatched
// against it in one call.
//
// Before the fix, the session reads the run folder's Plan.md (Step 6) before
// the run folder is created and the seed plan is applied (Step 8), so the
// stage set stays nil and the engine stops with no stage set available,
// before any harness invocation.
func TestIntegration_Seeding_StagedWorkflow_SeededStageTable_DispatchesFirstAttempt(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "staged-orch.md"))
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")

	// Seed sources: a Plan.md with a one-stage stage table, plus the mandatory
	// Requirement* candidate so NewPlan's naming rule is satisfied.
	seedDir := filepath.Join(dir, "seed")
	if err := os.MkdirAll(seedDir, 0700); err != nil {
		t.Fatalf("mkdir seedDir: %v", err)
	}
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	planSrc := filepath.Join(seedDir, "Plan.md")
	if err := os.WriteFile(planSrc, []byte(planContent), 0600); err != nil {
		t.Fatalf("write seed Plan.md: %v", err)
	}
	reqSrc := filepath.Join(seedDir, "Requirements.md")
	if err := os.WriteFile(reqSrc, []byte("# Requirements\n"), 0600); err != nil {
		t.Fatalf("write seed Requirements.md: %v", err)
	}

	// runDir does not exist before Start is called: it is created by
	// Store.Create as part of this one run-start sequence.
	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewMockAdapter()
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implemented",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{planSrc, reqSrc},
	}

	got, err := sess.Start(context.Background(), cfg)

	// The single Start call must complete the run -- no stop, no resume needed.
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least 1 harness invocation on the first Start call, got 0 (stage-driven dispatch never occurred)")
	}
	if invs[0].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want first dispatch to be implementation-tdd (stage 1), got %q", invs[0].Agent.Identifier)
	}

	// The seeded stage table itself must be present in the run folder,
	// byte-identical to its source.
	gotPlan, readErr := os.ReadFile(filepath.Join(runDir, "Plan.md"))
	if readErr != nil {
		t.Fatalf("read seeded Plan.md from run folder: %v", readErr)
	}
	if string(gotPlan) != planContent {
		t.Errorf("seeded Plan.md content: got %q, want %q", string(gotPlan), planContent)
	}
}
