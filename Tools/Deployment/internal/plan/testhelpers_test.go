package plan_test

// testhelpers_test.go provides shared infrastructure for plan package tests:
//
//   - loadRealCatalog: loads the real MOSAIC repository catalog from disk
//   - fakeCatalog: a configurable catalog.Catalog implementation for isolated unit tests
//   - fakeModule: a configurable domain.HarnessModule stub for plan tests
//   - fixture helpers for constructing ManifestEntry, Agent, Skill, and HookBundle values
//   - file system state helpers for T16.10 (no-write verification)
//
// Real-catalog tests exercise ResolveArtifacts against the actual repository, verifying
// counts and relationships. Fake-catalog tests cover edge cases (unknown workflows, missing
// agents, specific classification scenarios) without a repository dependency.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Real catalog loader
// ---------------------------------------------------------------------------

// loadRealCatalog resolves the MOSAIC repository root from the current working directory
// and returns a loaded Catalog. The test is fatally failed if loading fails.
func loadRealCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("catalog.ResolveRoot: %v", err)
	}
	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load(%q): %v", root, err)
	}
	return cat
}

// ---------------------------------------------------------------------------
// fakeCatalog — implements catalog.Catalog for isolated unit tests
// ---------------------------------------------------------------------------

// fakeCatalog is a configurable catalog.Catalog implementation. Fields are set directly
// before use; every method returns the zero value of its return type if the corresponding
// field is unset.
type fakeCatalog struct {
	root          string
	workers       []domain.Agent
	orchestrator  domain.Agent
	utilityAgents []domain.Agent
	skills        []domain.Skill
	hooks         []domain.HookBundle
	workflows     []domain.Workflow
}

func (f *fakeCatalog) Root() string { return f.root }

func (f *fakeCatalog) Agents() []domain.Agent { return f.workers }

func (f *fakeCatalog) Agent(key string) (domain.Agent, bool) {
	// Check workers, orchestrator, and utility agents.
	for _, a := range f.workers {
		if a.Key == key {
			return a, true
		}
	}
	if f.orchestrator.Key == key {
		return f.orchestrator, true
	}
	for _, a := range f.utilityAgents {
		if a.Key == key {
			return a, true
		}
	}
	return domain.Agent{}, false
}

func (f *fakeCatalog) Orchestrator() domain.Agent { return f.orchestrator }

func (f *fakeCatalog) UtilityAgents() []domain.Agent { return f.utilityAgents }

func (f *fakeCatalog) Skills() []domain.Skill { return f.skills }

func (f *fakeCatalog) Skill(key string) (domain.Skill, bool) {
	for _, s := range f.skills {
		if s.Key == key {
			return s, true
		}
	}
	return domain.Skill{}, false
}

func (f *fakeCatalog) Hooks() []domain.HookBundle { return f.hooks }

func (f *fakeCatalog) Hook(key string) (domain.HookBundle, bool) {
	for _, h := range f.hooks {
		if h.Key == key {
			return h, true
		}
	}
	return domain.HookBundle{}, false
}

func (f *fakeCatalog) Workflows() []domain.Workflow { return f.workflows }

func (f *fakeCatalog) Workflow(id string) (domain.Workflow, bool) {
	for _, w := range f.workflows {
		if w.ID == id {
			return w, true
		}
	}
	return domain.Workflow{}, false
}

func (f *fakeCatalog) WorkflowCategories() []domain.WorkflowCategory {
	return nil
}

func (f *fakeCatalog) Tiers() []domain.TierInfo { return nil }

func (f *fakeCatalog) WorkflowSection(id string) ([]byte, error) {
	_, ok := f.Workflow(id)
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return []byte(fmt.Sprintf("[[SECTION:Workflow:%s]]\n[[/SECTION:Workflow:%s]]\n", id, id)), nil
}

func (f *fakeCatalog) ReadSource(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ReadSource(%q): %w", path, err)
	}
	return data, nil
}

func (f *fakeCatalog) Issues() []catalog.Issue { return nil }

// ---------------------------------------------------------------------------
// fakeModule — implements domain.HarnessModule for isolated unit tests
// ---------------------------------------------------------------------------

// fakeModule is a configurable domain.HarnessModule stub. TargetPathFn is called to
// compute target paths; if nil, TargetPath returns a default path based on kind and key.
// ToolsFn is called to map tools; if nil, Tools returns an empty result.
type fakeModule struct {
	ref          domain.HarnessRef
	desc         *domain.HarnessDescriptor
	TargetPathFn func(req domain.TargetPathRequest) (string, error)
	ToolsFn      func(req domain.ToolRequest) (domain.ToolResult, error)
	HookPlanFn   func(req domain.HookPlanRequest) (domain.HookPlan, error)
}

func (m *fakeModule) Ref() domain.HarnessRef              { return m.ref }
func (m *fakeModule) Descriptor() *domain.HarnessDescriptor { return m.desc }
func (m *fakeModule) Close() error                         { return nil }

func (m *fakeModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	if m.TargetPathFn != nil {
		return m.TargetPathFn(req)
	}
	// Default: return a deterministic path based on kind and key.
	switch req.Kind {
	case domain.ArtifactAgent:
		return fmt.Sprintf("agents/%s.md", req.Key), nil
	case domain.ArtifactSkill:
		return fmt.Sprintf("skills/%s/SKILL.md", req.Key), nil
	case domain.ArtifactHook:
		return fmt.Sprintf("hooks/%s", req.Key), nil
	}
	return "", domain.ErrArtifactUnsupported
}

func (m *fakeModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	if m.ToolsFn != nil {
		return m.ToolsFn(req)
	}
	// Default: all tools are mapped (one resolution per generic tool, outcome mapped).
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{
			Generic: g,
			Outcome: domain.ToolMapped,
		}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}

func (m *fakeModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

func (m *fakeModule) Injection(_ string) (string, bool) { return "", false }

func (m *fakeModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	if m.HookPlanFn != nil {
		return m.HookPlanFn(req)
	}
	return domain.HookPlan{Supported: false, Reason: "not supported by stub"}, nil
}

// newFakeModule returns a fakeModule with a valid ref and descriptor.
// Tests that need custom TargetPathFn or ToolsFn should set those fields after construction.
func newFakeModule() *fakeModule {
	return &fakeModule{
		ref: domain.HarnessRef{
			ID:          "test-harness",
			DisplayName: "Test Harness",
			Tier:        domain.TierDescriptor,
			Usable:      true,
		},
		desc: &domain.HarnessDescriptor{
			SchemaVersion:     "1",
			ID:                "test-harness",
			DisplayName:       "Test Harness",
			TransformVersion:  "1.0",
			InjectionsVersion: "1.0",
		},
	}
}

// ---------------------------------------------------------------------------
// Agent, skill, hook, and workflow fixtures
// ---------------------------------------------------------------------------

// makeAgent returns a minimal domain.Agent with the given key and version.
// The agent requires no skills by default; callers may set RequiredSkills after construction.
func makeAgent(key, version string) domain.Agent {
	return domain.Agent{
		Key:            key,
		NumericID:      "1",
		Version:        version,
		Name:           key + "-name",
		Description:    key + "-description",
		Role:           domain.RoleWorker,
		RequiredSkills: []string{},
		Tools:          []string{"bash"},
		SourcePath:     "/fake/agents/" + key + ".md",
	}
}

// makeOrchestrator returns a minimal orchestrator agent.
func makeOrchestrator() domain.Agent {
	return domain.Agent{
		Key:            "orchestrator",
		Version:        "1.0",
		Name:           "Orchestrator",
		Description:    "The orchestrator agent.",
		Role:           domain.RoleOrchestrator,
		RequiredSkills: []string{},
		SourcePath:     "/fake/agents/orchestrator.md",
	}
}

// makeUtilityAgent returns a minimal utility agent with the given key.
func makeUtilityAgent(key string) domain.Agent {
	return domain.Agent{
		Key:            key,
		Version:        "1.0",
		Name:           key + "-name",
		Description:    key + "-description",
		Role:           domain.RoleUtility,
		RequiredSkills: []string{},
		SourcePath:     "/fake/agents/" + key + ".md",
	}
}

// makeSkill returns a minimal domain.Skill with the given key and version.
func makeSkill(key, version string) domain.Skill {
	return domain.Skill{
		Key:        key,
		Name:       key + "-name",
		Version:    version,
		SourceDir:  "/fake/skills/" + key,
		EntryFile:  "SKILL.md",
	}
}

// makeHookBundle returns a minimal domain.HookBundle with the given key and version.
func makeHookBundle(key, version string) domain.HookBundle {
	return domain.HookBundle{
		Key:     key,
		Version: version,
		SourceDir: "/fake/hooks/" + key,
	}
}

// makeWorkflow returns a minimal domain.Workflow with the given ID and agent keys.
func makeWorkflow(id string, agentKeys ...string) domain.Workflow {
	return domain.Workflow{
		ID:               id,
		Name:             id + "-name",
		Description:      id + "-description",
		Version:          "1.0",
		ReferencedAgents: agentKeys,
		SourcePath:       "/fake/workflows/" + id + ".md",
	}
}

// ---------------------------------------------------------------------------
// Manifest helpers
// ---------------------------------------------------------------------------

// makeManifestEntry returns a ManifestEntry for the given artifact ref and target path.
// All version fields are set to the given version, and ContentHash is set to the given hash.
func makeManifestEntry(ref domain.ArtifactRef, targetPath, version, contentHash string) domain.ManifestEntry {
	return domain.ManifestEntry{
		Ref:               ref,
		TargetPath:        targetPath,
		Version:           version,
		TransformVersion:  "1.0",
		InjectionsVersion: "1.0",
		ContentHash:       contentHash,
		DeployedAt:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// presentSnapshot returns a Snapshot with StatePresent and the given manifest.
func presentSnapshot(m domain.Manifest) manifest.Snapshot {
	return manifest.Snapshot{
		State:    manifest.StatePresent,
		Manifest: m,
		Path:     "/fake/workspace/.mosaic/manifest.yaml",
	}
}

// absentSnapshot returns a Snapshot with StateAbsent (no manifest on disk).
func absentSnapshot() manifest.Snapshot {
	return manifest.Snapshot{
		State: manifest.StateAbsent,
		Path:  "/fake/workspace/.mosaic/manifest.yaml",
	}
}

// corruptSnapshot returns a Snapshot with StateCorrupt (manifest file exists but is unparseable).
func corruptSnapshot() manifest.Snapshot {
	return manifest.Snapshot{
		State: manifest.StateCorrupt,
		Path:  "/fake/workspace/.mosaic/manifest.yaml",
		Err:   fmt.Errorf("parse error: invalid YAML"),
	}
}

// ---------------------------------------------------------------------------
// File system state capture (for T16.10 no-write tests)
// ---------------------------------------------------------------------------

// fileState records the modification time and size of one file.
type fileState struct {
	modTime time.Time
	size    int64
	isDir   bool
}

// captureFS walks dir and returns a map from relative path to fileState.
// Used to detect file system writes by comparing before/after snapshots.
func captureFS(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	state := make(map[string]fileState)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		state[rel] = fileState{
			modTime: info.ModTime(),
			size:    info.Size(),
			isDir:   d.IsDir(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("captureFS(%q): %v", dir, err)
	}
	return state
}

// fsChanged reports whether the two file system states differ, and describes the first
// difference found. Returns false (no change) if the states are identical.
func fsChanged(before, after map[string]fileState) (changed bool, description string) {
	for path, bSt := range before {
		aSt, ok := after[path]
		if !ok {
			return true, fmt.Sprintf("file removed: %s", path)
		}
		if !bSt.isDir && (bSt.size != aSt.size || !bSt.modTime.Equal(aSt.modTime)) {
			return true, fmt.Sprintf("file modified: %s (size %d→%d)", path, bSt.size, aSt.size)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			return true, fmt.Sprintf("file created: %s", path)
		}
	}
	return false, ""
}

// makeTempWorkspace creates an isolated temp directory with a .mosaic subdirectory.
// It does NOT write a manifest; callers that need one should use writeManifestFile.
func makeTempWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".mosaic"), 0o755); err != nil {
		t.Fatalf("makeTempWorkspace: mkdir .mosaic: %v", err)
	}
	return ws
}

// writeManifestFile writes a manifest for the given workspace using the production Store.
// The test is fatally failed if the write fails.
func writeManifestFile(t *testing.T, ws string, m domain.Manifest) {
	t.Helper()
	if err := manifest.NewStore().Save(ws, m); err != nil {
		t.Fatalf("writeManifestFile: %v", err)
	}
}

// must fatals the test if err is non-nil.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// agentRef returns an ArtifactRef for an agent with the given key.
func agentRef(key string) domain.ArtifactRef {
	return domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key}
}

// skillRef returns an ArtifactRef for a skill with the given key.
func skillRef(key string) domain.ArtifactRef {
	return domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: key}
}

// hookRef returns an ArtifactRef for a hook bundle with the given key.
func hookRef(key string) domain.ArtifactRef {
	return domain.ArtifactRef{Kind: domain.ArtifactHook, Key: key}
}

// containsAgent reports whether agents contains an agent with the given key.
func containsAgent(agents []domain.Agent, key string) bool {
	for _, a := range agents {
		if a.Key == key {
			return true
		}
	}
	return false
}

// containsSkill reports whether skills contains a skill with the given key.
func containsSkill(skills []domain.Skill, key string) bool {
	for _, s := range skills {
		if s.Key == key {
			return true
		}
	}
	return false
}

// findItem returns the first PlanItem whose Ref.Key matches the given key, and a boolean
// indicating whether it was found. Used in plan item classification assertions.
func findItem(items []domain.PlanItem, key string) (domain.PlanItem, bool) {
	for _, item := range items {
		if item.Ref.Key == key {
			return item, true
		}
	}
	return domain.PlanItem{}, false
}

// findGap returns the first Gap with the given kind, or a zero Gap and false.
func findGap(gaps []domain.Gap, kind domain.GapKind) (domain.Gap, bool) {
	for _, g := range gaps {
		if g.Kind == kind {
			return g, true
		}
	}
	return domain.Gap{}, false
}
