package transform_test

// testhelpers_test.go contains shared test infrastructure for the transform package tests:
//   - fixtureModule: a domain.HarnessModule backed by testdata/transform/fixture.yaml
//   - Standard model selection for tests
//   - Path helpers for locating generic agents and testdata

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// testdataDir is resolved relative to this package's test working directory.
// Go tests run in the package directory (internal/transform/); testdata lives two levels
// up at Tools/Deployment/testdata/transform/.
const testdataDir = "../../testdata/transform"

// newFixtureModule returns a domain.HarnessModule backed by the fixture harness descriptor
// at testdata/transform/fixture.yaml. All harness-specific behaviour goes through the shared
// descriptor algorithms (CD-2), so no test branch varies on a harness name.
func newFixtureModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(testdataDir, "fixture.yaml"))
	if err != nil {
		t.Fatalf("read fixture descriptor: %v", err)
	}
	desc, err := descriptor.Parse(src, testdataDir+"/fixture.yaml")
	if err != nil {
		t.Fatalf("parse fixture descriptor: %v", err)
	}
	return &fixtureModule{desc: desc}
}

// fixtureModel returns the standard model selection used in golden and pipeline tests.
// Using OriginHarnessList reflects the normal flow where the user picks from the harness
// model list.
func fixtureModel() domain.ModelSelection {
	return domain.ModelSelection{
		ModelID: "claude/claude-sonnet",
		Origin:  domain.OriginHarnessList,
	}
}

// genericAgentsDir returns the absolute path to Agents/Generic/Agents/ in the repository
// root — the directory that contains only worker agent .md files organised by category
// (Audit, Creation, Execution, …). Skill files, hook README files, the orchestrator, and
// utility agents live in sibling directories and are intentionally excluded.
//
// The test is skipped rather than failed if the directory cannot be located, so that the
// transform tests remain runnable in an isolated checkout that only includes Tools/.
func genericAgentsDir(t *testing.T) string {
	t.Helper()
	// From Tools/Deployment/internal/transform/ navigate four levels up to the repo root,
	// then descend into the worker agents directory.
	rel := filepath.Join("..", "..", "..", "..", "Agents", "Generic", "Agents")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve generic agents directory: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("generic agents directory not found at %s (skipping body-preservation tests): %v", abs, err)
	}
	return abs
}

// collectGenericAgentPaths returns all worker agent .md file paths under dir, recursively,
// excluding README files. README files have no frontmatter; processing them as ArtifactAgent
// would produce incorrect semantics because SplitFrontmatter would treat the entire file as
// the body, masking bugs in frontmatter handling and polluting the body-preservation corpus.
func collectGenericAgentPaths(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		// Skip README files — they are category documentation, not agent definitions.
		// They have no frontmatter and must not be dispatched as ArtifactAgent.
		if strings.EqualFold(filepath.Base(p), "readme.md") {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	if len(paths) == 0 {
		t.Skipf("no agent .md files found under %s", dir)
	}
	return paths
}

// fixtureProtocol returns a ProtocolContent with distinct orchestrator and subagent blocks
// for use in protocol region tests. The two blocks carry unique, non-overlapping content so
// that tests can assert both the presence of the correct block and the absence of the wrong one.
func fixtureProtocol(version string) domain.ProtocolContent {
	return domain.ProtocolContent{
		Version: version,
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolOrchestrator: []byte(orchestratorBlockContent),
			domain.ProtocolSubagent:     []byte(subagentBlockContent),
		},
	}
}

// truncateBytes returns the first n bytes of b for use in diagnostic messages.
func truncateBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

// ---------------------------------------------------------------------------
// fixtureModule — domain.HarnessModule backed by testdata/transform/fixture.yaml
// ---------------------------------------------------------------------------

// fixtureModule implements domain.HarnessModule entirely through the shared descriptor
// algorithms (CD-2). No harness-specific branch exists in this test helper; all behaviour
// is driven by the parsed descriptor.
type fixtureModule struct {
	desc *domain.HarnessDescriptor
}

func (m *fixtureModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{
		ID:          m.desc.ID,
		DisplayName: m.desc.DisplayName,
		Tier:        domain.TierDescriptor,
		Usable:      true,
	}
}

func (m *fixtureModule) Descriptor() *domain.HarnessDescriptor {
	return m.desc
}

func (m *fixtureModule) Close() error {
	return nil
}

func (m *fixtureModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(m.desc, req)
}

func (m *fixtureModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}

func (m *fixtureModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}

func (m *fixtureModule) Injection(req domain.InjectionRequest) (string, bool) {
	for _, inj := range m.desc.Injections {
		if inj.Name == req.Name {
			return inj.Content, true
		}
	}
	return "", false
}

func (m *fixtureModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{
		Supported: false,
		Reason:    "fixture harness declares no hook support",
	}, nil
}
