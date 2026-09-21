package app

// seam_app_replay_test.go is the app-layer replay that T7.1a requires.
//
// The committed capture/replay driver (seam_baseline_test.go) calls transform.Apply
// directly at the transform layer, so its byte-identity claim does not cover the
// decode funnel, the deployedReader closure, or buildContent -- all of which live in
// internal/app and are the code Stage 7 actually rewires. This file fills that gap.
//
// # What is being proved
//
// The decode-then-encode round trip is an identity at the application layer for
// Markdown-format harnesses:
//
//   decode(deployed_bytes) == deployed_bytes  (identity for Markdown)
//   encode(transform(source, decoded)) == seam_baseline_output  (byte-for-byte)
//
// Two cases are tested per harness:
//
//   - Create path: no prior deployed file; the closure is called with ActionCreate.
//   - Update path: the seam-baseline prior bytes are placed in the workspace; the
//     closure is called with ActionUpdate and the funnel reads + decodes the file.
//
// Both cases compare the closure's output byte-for-byte against the committed seam
// baseline. A mismatch means the seam changed something for Markdown harnesses, which
// is a regression requiring a code fix -- not a re-capture.
//
// # Entry point
//
// The entry point is buildContent (the application layer's content-building closure)
// called directly, since this is a package-internal test. The test constructs a minimal
// service wired with a real harness module and a stub catalog that returns the agent
// fixture source, then invokes the returned closure.
//
// # Blank imports
//
// The four blank imports register each harness's factory with the package-level
// registry. The import guard in tools/importcheck/main.go exempts _test.go files
// from the harness-isolation check, so these imports are permitted here.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/todo"

	_ "mosaic-deploy/internal/harness/builtin/claudecode"
	_ "mosaic-deploy/internal/harness/builtin/ghcpcli"
	_ "mosaic-deploy/internal/harness/builtin/opencode"
	_ "mosaic-deploy/internal/harness/builtin/vscodeghcp"
)

// ---------------------------------------------------------------------------
// Minimal stubs for package-internal use (package app_test stubs not accessible here)
// ---------------------------------------------------------------------------

// seamReplayCatalog is a minimal catalog.Catalog for seam replay tests. It serves
// ReadSource requests from an in-memory map and returns a stub for everything else.
type seamReplayCatalog struct {
	sources map[string][]byte
}

func (c *seamReplayCatalog) Root() string                                   { return "" }
func (c *seamReplayCatalog) CatalogRoot() string                            { return "" }
func (c *seamReplayCatalog) Agents() []domain.Agent                         { return nil }
func (c *seamReplayCatalog) Agent(string) (domain.Agent, bool)              { return domain.Agent{}, false }
func (c *seamReplayCatalog) Orchestrator() domain.Agent                     { return domain.Agent{} }
func (c *seamReplayCatalog) OrchestratorScript() (domain.Agent, bool)       { return domain.Agent{}, false }
func (c *seamReplayCatalog) UtilityAgents() []domain.Agent                  { return nil }
func (c *seamReplayCatalog) StandaloneAgents() []domain.Agent               { return nil }
func (c *seamReplayCatalog) InfrastructureAgents() []domain.Agent           { return nil }
func (c *seamReplayCatalog) Skills() []domain.Skill                         { return nil }
func (c *seamReplayCatalog) Skill(string) (domain.Skill, bool)              { return domain.Skill{}, false }
func (c *seamReplayCatalog) Hooks() []domain.HookBundle                     { return nil }
func (c *seamReplayCatalog) Hook(string) (domain.HookBundle, bool)          { return domain.HookBundle{}, false }
func (c *seamReplayCatalog) Workflows() []domain.Workflow                   { return nil }
func (c *seamReplayCatalog) Workflow(string) (domain.Workflow, bool)        { return domain.Workflow{}, false }
func (c *seamReplayCatalog) WorkflowCategories() []domain.WorkflowCategory  { return nil }
func (c *seamReplayCatalog) Tiers() []domain.TierInfo                       { return nil }
func (c *seamReplayCatalog) WorkflowSection(string) ([]byte, error)         { return nil, nil }
func (c *seamReplayCatalog) Issues() []catalog.Issue                        { return nil }
func (c *seamReplayCatalog) AgentByNumericID(string) (domain.Agent, bool)   { return domain.Agent{}, false }
func (c *seamReplayCatalog) ReadSource(path string) ([]byte, error) {
	if c.sources != nil {
		if b, ok := c.sources[path]; ok {
			return b, nil
		}
	}
	return nil, nil
}

// seamReplayTodo is a no-op todo.Collector for seam replay tests. Gaps emitted by
// the transform (e.g. parking) are accepted and discarded; the test does not assert
// on them.
type seamReplayTodo struct{}

func (seamReplayTodo) Add(domain.TodoItem)     {}
func (seamReplayTodo) AddGap(domain.Gap)       {}
func (seamReplayTodo) Items() []domain.TodoItem { return nil }
func (seamReplayTodo) Groups() []todo.Group    { return nil }
func (seamReplayTodo) Empty() bool             { return true }
func (seamReplayTodo) Reset()                  {}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

func seamReplayFrozenCatalogRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "frozen-catalog")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve frozen-catalog dir: %v", err)
	}
	return abs
}

func seamReplayAgentFixturesDir(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "agent-fixtures")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve agent-fixtures dir: %v", err)
	}
	return abs
}

func seamReplayBaselineDir(t *testing.T, harnessDir, subdir string) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "seam-baseline", harnessDir, subdir)
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve seam-baseline dir: %v", err)
	}
	return abs
}

// seamReplayReadFile reads a file and fails the test if the file cannot be read.
func seamReplayReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// ---------------------------------------------------------------------------
// T7.1a: app-layer replay for claude-code harness
// ---------------------------------------------------------------------------

// TestSeamAppReplay_ClaudeCode_CreateAndUpdate drives two cases from the seam baseline
// through the application layer (the deployedReader closure, the decode funnel, and
// buildContent) and asserts byte-for-byte identity with the committed golden files.
//
// Create case:  no prior deployed file; the source is processed fresh.
// Update case:  the seam-baseline prior bytes are placed in a temp workspace; the
//               funnel reads and decodes them before buildContent calls transform.Apply.
//
// Both cases use the real claude-code harness module resolved from the frozen catalog.
// A byte-identity failure is a regression in the seam wiring; fix the code, do not
// regenerate the baseline.
func TestSeamAppReplay_ClaudeCode_CreateAndUpdate(t *testing.T) {
	frozenRoot := seamReplayFrozenCatalogRoot(t)
	fixturesDir := seamReplayAgentFixturesDir(t)

	// Load protocol from the frozen catalog root (required by transform.Apply for agents).
	protocol, err := catalog.FileProtocolLoader{}.LoadProtocol(frozenRoot)
	if err != nil {
		t.Fatalf("load protocol from frozen catalog: %v", err)
	}

	// Resolve the claude-code module from the frozen catalog. The blank imports at the
	// top of this file register the four built-in harness factories.
	reg, err := registry.Discover(registry.Options{MosaicRoot: frozenRoot})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	mod, err := reg.Resolve("claude-code")
	if err != nil {
		t.Fatalf("resolve claude-code module: %v", err)
	}
	defer mod.Close() //nolint:errcheck

	// Load the test-runner agent source fixture (the single fixture the baseline uses
	// for update-path cases; also used here as the source for both create and update).
	const fixtureName = "test-runner.md"
	const agentKey = "test-runner"
	const agentSourcePath = "test-runner-fixture.md" // catalog key for ReadSource stub
	srcBytes := seamReplayReadFile(t, filepath.Join(fixturesDir, fixtureName))

	// Stub catalog serving the fixture source under a stable path key.
	cat := &seamReplayCatalog{sources: map[string][]byte{agentSourcePath: srcBytes}}

	// agentByKey maps the test-runner key to a minimal domain.Agent carrying the source
	// path and role; these are the only fields buildContent reads from it.
	agentByKey := map[string]domain.Agent{
		agentKey: {
			Key:        agentKey,
			SourcePath: agentSourcePath,
			Role:       domain.RoleWorker,
		},
	}

	// Model selection matching the pinned model used in seam_baseline_test.go.
	models := map[string]domain.ModelSelection{
		agentKey: {
			ModelID: "claude-sonnet-4-6",
			Origin:  domain.OriginHarnessList,
		},
	}

	// Fixed clock for deterministic Timestamp field in the transform request.
	fixedNow := time.Date(2026, 9, 19, 7, 46, 9, 0, time.UTC)

	// Construct a minimal service carrying only the deps buildContent needs.
	svc := &service{deps: Deps{
		Catalog: cat,
		Todo:    seamReplayTodo{},
		Now:     func() time.Time { return fixedNow },
	}}

	// Workspace and descriptor for the deployedReader closure. For Markdown harnesses
	// AgentFormatID is empty, which resolves to the identity Markdown translator.
	desc := mod.Descriptor()

	t.Run("create", func(t *testing.T) {
		// Create path: no prior file exists in the workspace. The funnel returns an absent
		// DeployedRead; buildContent passes Deployed: nil and DeployedRaw: nil to transform.Apply.
		ws := t.TempDir()

		deployedReader := func(item domain.PlanItem) (DeployedRead, agentformat.Operation, error) {
			return readDeployedPlanItem(ws, desc, item)
		}
		contentFn := svc.buildContent(
			mod, agentByKey, models,
			nil, nil, // customTools, skippedTools
			nil, nil, // workflowBlocks, infrastructureBlocks
			domain.ScopeProject,
			deployedReader,
			"",              // toolMappingsVersion
			protocol,
			domain.BundleContent{},
			nil, // harnessOnly
			nil, // ownedKeyDiffSink
		)

		item := domain.PlanItem{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agentKey},
			TargetPath: agentKey + ".md",
			Action:     domain.ActionCreate,
		}
		output, callErr := contentFn(item)
		if callErr != nil {
			t.Fatalf("buildContent closure for create returned error: %v", callErr)
		}

		// Compare against the committed seam-baseline create output.
		goldenPath := filepath.Join(seamReplayBaselineDir(t, "claude-code", "create"), agentKey+".md")
		golden := seamReplayReadFile(t, goldenPath)
		if !bytes.Equal(output, golden) {
			first := seamReplayFirstDiff(output, golden)
			t.Errorf("create output does not match baseline %s\n"+
				"output: %d bytes, golden: %d bytes, first difference at byte %d\n"+
				"(do NOT regenerate the baseline; fix the seam code instead)",
				goldenPath, len(output), len(golden), first)
		}
	})

	t.Run("update", func(t *testing.T) {
		// Update path: place the seam-baseline prior bytes in the workspace so the funnel
		// reads, decodes (identity for Markdown), and returns them as both Raw and Canonical.
		ws := t.TempDir()
		priorPath := filepath.Join(seamReplayBaselineDir(t, "claude-code", "update"), agentKey+".prior.md")
		priorBytes := seamReplayReadFile(t, priorPath)

		targetFile := filepath.Join(ws, agentKey+".md")
		if writeErr := os.WriteFile(targetFile, priorBytes, 0o644); writeErr != nil {
			t.Fatalf("write prior bytes to workspace: %v", writeErr)
		}

		deployedReader := func(item domain.PlanItem) (DeployedRead, agentformat.Operation, error) {
			return readDeployedPlanItem(ws, desc, item)
		}
		contentFn := svc.buildContent(
			mod, agentByKey, models,
			nil, nil,
			nil, nil,
			domain.ScopeProject,
			deployedReader,
			"",
			protocol,
			domain.BundleContent{},
			nil,
			nil, // ownedKeyDiffSink
		)

		item := domain.PlanItem{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agentKey},
			TargetPath: agentKey + ".md",
			Action:     domain.ActionUpdate,
		}
		output, callErr := contentFn(item)
		if callErr != nil {
			t.Fatalf("buildContent closure for update returned error: %v", callErr)
		}

		// Compare against the committed seam-baseline update output.
		goldenPath := filepath.Join(seamReplayBaselineDir(t, "claude-code", "update"), agentKey+".output.md")
		golden := seamReplayReadFile(t, goldenPath)
		if !bytes.Equal(output, golden) {
			first := seamReplayFirstDiff(output, golden)
			t.Errorf("update output does not match baseline %s\n"+
				"output: %d bytes, golden: %d bytes, first difference at byte %d\n"+
				"(do NOT regenerate the baseline; fix the seam code instead)",
				goldenPath, len(output), len(golden), first)
		}
	})
}

// seamReplayFirstDiff returns the index of the first differing byte between a and b.
func seamReplayFirstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
