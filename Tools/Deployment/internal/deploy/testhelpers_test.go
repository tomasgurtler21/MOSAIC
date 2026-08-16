package deploy_test

// testhelpers_test.go provides shared test doubles and builder helpers used across all
// deploy test files. Nothing here is a test itself.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// spyLogger — records every call so tests can assert on what was logged.
// ---------------------------------------------------------------------------

type spyLogger struct {
	actions []domain.ActionRecord
	events  []logging.Event
	starts  []logging.RunContext
	ends    []domain.Outcome
}

func newSpyLogger() *spyLogger { return &spyLogger{} }

func (l *spyLogger) StartRun(rc logging.RunContext)  { l.starts = append(l.starts, rc) }
func (l *spyLogger) Event(e logging.Event)           { l.events = append(l.events, e) }
func (l *spyLogger) Action(ar domain.ActionRecord)   { l.actions = append(l.actions, ar) }
func (l *spyLogger) EndRun(outcome domain.Outcome)   { l.ends = append(l.ends, outcome) }
func (l *spyLogger) Paths() domain.LogPaths          { return domain.LogPaths{} }
func (l *spyLogger) Degraded() []error               { return nil }

// actionTakens returns the ActionTaken values from all recorded actions, in order.
func (l *spyLogger) actionTakens() []domain.ActionTaken {
	out := make([]domain.ActionTaken, len(l.actions))
	for i, a := range l.actions {
		out[i] = a.Taken
	}
	return out
}

// ---------------------------------------------------------------------------
// spyCollector — records every Add and AddGap call so tests can assert on it.
// ---------------------------------------------------------------------------

type spyCollector struct {
	items []domain.TodoItem
	gaps  []domain.Gap
}

func newSpyCollector() *spyCollector { return &spyCollector{} }

func (c *spyCollector) Add(item domain.TodoItem)  { c.items = append(c.items, item) }
func (c *spyCollector) AddGap(g domain.Gap)       { c.gaps = append(c.gaps, g) }
func (c *spyCollector) Items() []domain.TodoItem  { return c.items }
func (c *spyCollector) Groups() []todo.Group      { return nil }
func (c *spyCollector) Empty() bool               { return len(c.items) == 0 && len(c.gaps) == 0 }

// hasGapKind reports whether any gap of the given kind was collected.
func (c *spyCollector) hasGapKind(kind domain.GapKind) bool {
	for _, g := range c.gaps {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Plan and item builders
// ---------------------------------------------------------------------------

// newAgentItem builds a PlanItem for an agent, using targetPath as the deployment path
// relative to the deployment root.
func newAgentItem(key, targetPath string, action domain.PlanAction) domain.PlanItem {
	return domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
		SourcePath: filepath.Join("testdata", key+".md"),
		TargetPath: targetPath,
		Action:     action,
	}
}

// newConflictItem builds a PlanItem that has an ActionConflict classification,
// including a LocalModification record to reflect a realistic classification.
func newConflictItem(key, targetPath string) domain.PlanItem {
	item := newAgentItem(key, targetPath, domain.ActionConflict)
	item.Conflict = &domain.LocalModification{
		RecordedHash: "sha256:abc123",
		CurrentHash:  "sha256:def456",
	}
	return item
}

// newPlan builds a minimal Plan with the supplied items and workspace.
func newPlan(workspace string, items ...domain.PlanItem) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
		Harness:       domain.HarnessRef{ID: "test-harness"},
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         items,
	}
}

// fixedContent returns a Content function that always produces the given bytes,
// regardless of which plan item is requested.
func fixedContent(data []byte) func(domain.PlanItem) ([]byte, error) {
	return func(domain.PlanItem) ([]byte, error) { return data, nil }
}

// ---------------------------------------------------------------------------
// Filesystem helpers
// ---------------------------------------------------------------------------

// blockPath creates a regular FILE at path, making it impossible for any process to
// create a directory at that path or write any child file under it. This is used to
// simulate unwritable deployment target directories in a cross-platform way.
func blockPath(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("blockPath: create parent: %v", err)
	}
	if err := os.WriteFile(path, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("blockPath: write blocker file: %v", err)
	}
}

// pathInFile returns a path that is guaranteed to be unwritable because one of its
// ancestor components is a regular file rather than a directory. This simulates a
// completely unwritable root in a cross-platform way.
func pathInFile(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	f := filepath.Join(parent, "file")
	if err := os.WriteFile(f, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("pathInFile: %v", err)
	}
	return filepath.Join(f, "child")
}

// fileExistsAt reports whether a regular file exists at the given absolute path.
func fileExistsAt(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// readFile reads the file at path and returns its contents. It fails the test if the file
// cannot be read.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile(%q): %v", path, err)
	}
	return data
}
