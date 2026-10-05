package integration_test

// End-to-end tests for the Artifacts registry with the real file store, the
// filesystem output detector and the file approval reader. Only outputs an
// invocation actually created or modified are registered, one row per key, and
// the HITL gate looks only at those outputs.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

func approvedDoc(approved, tag string) string {
	return "---\nhuman_approved: " + approved + "\n---\n# Doc " + tag + "\n"
}

type writtenOutputsRun struct {
	sess      session.Session
	adapter   *harness.MockAdapter
	store     domain.ArtifactStore
	cfg       domain.RunConfig
	runFolder string
}

// newWrittenOutputsRun wires an auto-mode run of the two-agent workflow whose
// agent-a declares design.md and agent-b declares result.md, in a fresh
// workspace that is also the working directory.
func newWrittenOutputsRun(t *testing.T) *writtenOutputsRun {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join(sessionTestdataDir, "written-outputs-orch.md"))
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	runFolder := scopedRunFolder(t, dir)
	orchPath := copyFile(t, dir, "orchestrator.md", fixture)
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	f.SetWriteRoot(dir)
	store := artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md"))
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Approvals: artifact.NewApprovalReader(),
		Outputs:   artifact.NewOutputWriteDetector(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return &writtenOutputsRun{
		sess: sess, adapter: f, store: store, runFolder: runFolder,
		cfg: domain.RunConfig{
			RunID:                integrationRunID,
			OrchestratorFilePath: orchPath,
			WorkflowID:           "written-outputs",
			Task:                 "written outputs task",
			IsNewRun:             true,
			RunFolder:            runFolder,
			RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		},
	}
}

func runPath(rel string) string { return domain.RunScopedFolder(integrationRunID) + "/" + rel }

func okResponse(msg string) harness.ScriptedEntry {
	return harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		StatusCode: domain.StatusSUCCESS, StatusMessage: msg}}
}

// registryRows returns the registry rows keyed by artifact, with any run-scoped
// folder prefix removed, and the total row count.
func registryRows(t *testing.T, store domain.ArtifactStore) (map[string]domain.ArtifactRegistryEntry, int) {
	t.Helper()
	state, err := store.Read(context.Background())
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	prefix := domain.RunScopedFolder(integrationRunID) + "/"
	rows := map[string]domain.ArtifactRegistryEntry{}
	for _, e := range state.ArtifactRegistry {
		rows[strings.TrimPrefix(e.Artifact, prefix)] = e
	}
	return rows, len(state.ArtifactRegistry)
}

func TestIntegration_WrittenOutputs_DeclaredButUnwrittenOutputIsNotRegistered(t *testing.T) {
	r := newWrittenOutputsRun(t)
	// agent-a writes design.md; agent-b declares result.md but never writes it.
	first := okResponse("designed")
	first.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("true", "v1")}}
	r.adapter.Queue("agent-a", first)
	r.adapter.Queue("agent-b", okResponse("reviewed"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	rows, total := registryRows(t, r.store)
	if total != 1 {
		t.Errorf("registry rows: want 1 (only the written output), got %d: %v", total, rows)
	}
	if _, ok := rows["design.md"]; !ok {
		t.Errorf("design.md must be registered, got %v", rows)
	}
	if _, ok := rows["result.md"]; ok {
		t.Errorf("result.md was never written and must not be registered")
	}
}

func TestIntegration_WrittenOutputs_UnchangedPreExistingOutputIsNeitherRegisteredNorGated(t *testing.T) {
	r := newWrittenOutputsRun(t)
	// result.md exists from earlier work with a stale false stamp; agent-b
	// leaves it alone. design.md pre-exists and agent-a rewrites it.
	writeRunFileAt(t, r.runFolder, "result.md", approvedDoc("false", "stale"))
	writeRunFileAt(t, r.runFolder, "design.md", approvedDoc("true", "old"))
	first := okResponse("redesigned")
	first.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("true", "new")}}
	r.adapter.Queue("agent-a", first)
	r.adapter.Queue("agent-b", okResponse("reviewed"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	rows, total := registryRows(t, r.store)
	if total != 1 || rows["design.md"].Artifact == "" {
		t.Errorf("want only the modified design.md registered, got %v", rows)
	}
	if n := len(r.adapter.Invocations()); n != 2 {
		t.Errorf("want 2 invocations (the stale stamp on the unchanged result.md must not trigger a re-dispatch), got %d", n)
	}
}

func TestIntegration_WrittenOutputs_ReworkedOutputKeepsOneRowAttributedToLatestWriter(t *testing.T) {
	r := newWrittenOutputsRun(t)
	// The first attempt leaves a false stamp, so the gate re-dispatches; the
	// second attempt writes the approved version of the same file.
	first := okResponse("draft")
	first.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("false", "draft")}}
	second := okResponse("final")
	second.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("true", "final")}}
	r.adapter.Queue("agent-a", first, second)
	r.adapter.Queue("agent-b", okResponse("reviewed"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	rows, total := registryRows(t, r.store)
	if total != 1 {
		t.Fatalf("registry rows: want 1 (rework upserts the row), got %d: %v", total, rows)
	}
	if by := rows["design.md"].CreatedBy; by != "agent-a#2" {
		t.Errorf("design.md CreatedBy: want the accepted re-dispatch agent-a#2, got %q", by)
	}
}

// writeRunFileAt writes content to a file in the run folder, creating directories.
func writeRunFileAt(t *testing.T, runFolder, rel, content string) {
	t.Helper()
	p := filepath.Join(runFolder, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
