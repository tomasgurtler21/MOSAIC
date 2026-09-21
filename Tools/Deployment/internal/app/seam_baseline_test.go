package app_test

// seam_baseline_test.go is the committed capture/replay driver for the pre-seam
// regression baseline of all four existing harnesses.
//
// # Entry point
//
// This driver calls transform.Apply (the transform layer), NOT the app layer's
// buildContent or any service method.
//
// Why the transform layer and not the app layer: The app layer's per-agent content
// building happens inside buildContent, which is a private closure reached only through
// the full deploy flow (DeployNew, updateAgents, and their siblings). Pinning that flow
// deterministically requires stubbing the catalog, planner, interaction port, filesystem
// I/O, and clock -- a full service harness that produces no signal beyond what
// transform.Apply already produces for these four Markdown-format harnesses. The seam
// stage's change to buildContent (inserting decode before transform.Apply) is observable
// through transform.Apply for Markdown content because Markdown decode is the identity:
// Decode(bytes) == bytes. If the seam stage changes something in buildContent that is
// NOT visible at the transform layer (for example, a bug in how Op is threaded), the
// seam stage adds its own app-layer replay on top of this driver; this driver's contract
// is to cover the transform.Apply call and nothing above it.
//
// The seam stage must replay this driver at the same entry point (transform.Apply) and
// nowhere else. It may add its own app-layer test on top; that additional test is the
// seam stage's responsibility and is not part of this baseline.
//
// # Adaptation rule for later stages
//
// The call shape of transform.Apply may be adapted when the seam stage changes the
// function's signature (it adds a required ArtifactContext parameter and a second
// deployed-bytes field in the Result). Specifically:
//
//   - The parameters passed to transform.Apply and the Request built for each call MAY
//     be updated to match the new signature.
//   - The pinned fixtures (testdata/frozen-catalog/, testdata/agent-fixtures/) are OFF
//     LIMITS and must not be modified.
//   - The committed golden files under testdata/seam-baseline/ are OFF LIMITS and must
//     not be regenerated to make a comparison pass. The baseline is the pre-seam record
//     and must remain unchanged for the comparison to mean anything.
//
// # Inputs
//
// All inputs come from committed fixtures inside the Deployment module:
//
//   - testdata/frozen-catalog/ -- a frozen snapshot of Catalog/ and Development/ used
//     by all harness golden tests; this driver reuses it rather than duplicating it.
//   - testdata/agent-fixtures/ -- four pinned agent source files (contracts-review,
//     test-runner, planner-tdd-soft, orchestrator) used by the existing harness golden
//     tests; this driver reuses the same set.
//
// No input is read from the live Catalog/ tree. Two consecutive capture runs on an
// unchanged tree produce byte-identical baseline files (verified before committing).
//
// # Blank imports
//
// The four blank imports below register the built-in harness factories with the package-
// level registry so that registry.Discover resolves them. The import guard in
// Tools/Deployment/tools/importcheck/main.go exempts _test.go files from the harness-
// isolation check, so these imports are permitted here.
//
// # Usage
//
//	# Capture (initial run or to extend the baseline with new fixtures):
//	go test ./internal/app/... -run TestSeamBaseline -update
//
//	# Compare (default, run after every pipeline change):
//	go test ./internal/app/... -run TestSeamBaseline

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"

	// Blank imports register each harness's factory with the package-level registry.
	// The import guard in tools/importcheck/main.go exempts _test.go files from the
	// harness-isolation check; these imports are permitted in this test file.
	_ "mosaic-deploy/internal/harness/builtin/claudecode"
	_ "mosaic-deploy/internal/harness/builtin/ghcpcli"
	_ "mosaic-deploy/internal/harness/builtin/opencode"
	_ "mosaic-deploy/internal/harness/builtin/vscodeghcp"
)

var seamUpdate = flag.Bool("update", false, "capture seam baseline (overwrite testdata/seam-baseline/)")

// seamBaselineModel is the pinned model selection used for all harnesses and fixtures.
// A per-harness model can be used; a single pinned model is sufficient for the byte-
// identity property the baseline is meant to guard.
var seamBaselineModel = domain.ModelSelection{
	ModelID: "claude-sonnet-4-6",
	Origin:  domain.OriginHarnessList,
}

// seamHarnesses enumerates the four existing harnesses and their harness IDs.
var seamHarnesses = []struct {
	id  string
	dir string // subdirectory under testdata/seam-baseline/
}{
	{id: "claude-code", dir: "claude-code"},
	{id: "ghcp-cli", dir: "ghcp-cli"},
	{id: "opencode", dir: "opencode"},
	{id: "vscode-ghcp", dir: "vscode-ghcp"},
}

// seamAgentFixtures enumerates the four pinned agent fixtures with their roles.
var seamAgentFixtures = []struct {
	file string
	key  string
	role domain.AgentRole
}{
	{file: "contracts-review.md", key: "contracts-review", role: domain.RoleWorker},
	{file: "test-runner.md", key: "test-runner", role: domain.RoleWorker},
	{file: "planner-tdd-soft.md", key: "planner-tdd-soft", role: domain.RoleWorker},
	{file: "orchestrator.md", key: "orchestrator", role: domain.RoleOrchestrator},
}

// seamUpdateFixture is the single fixture used for the update-path cases.
// Using test-runner because it exercises the full tool set (tool-heavy) and has
// a non-trivial body, making it a good regression anchor for the decode+encode cycle.
const seamUpdateFixture = "test-runner.md"

// seamBaselineDir returns the absolute path to testdata/seam-baseline/ relative to
// the internal/app package.
func seamBaselineDir(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "seam-baseline")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve seam-baseline dir: %v", err)
	}
	return abs
}

// seamFrozenCatalogRoot returns the absolute path to testdata/frozen-catalog/.
func seamFrozenCatalogRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "frozen-catalog")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve frozen-catalog root: %v", err)
	}
	return abs
}

// seamAgentFixturesDir returns the absolute path to testdata/agent-fixtures/.
func seamAgentFixturesDir(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "testdata", "agent-fixtures")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve agent-fixtures dir: %v", err)
	}
	return abs
}

// TestSeamBaseline is the single capture/replay entry point for the seam baseline.
// It runs in capture mode when -update is passed, and in compare mode otherwise.
//
// Capture: for each harness x fixture combination, transform.Apply is called with
// nil Deployed (create path) and the output is written to seam-baseline/<harness>/create/.
// For the update-path fixture (test-runner), the create output is re-used as the prior
// deployed bytes and transform.Apply is called again; both the prior bytes and the update
// output are written to seam-baseline/<harness>/update/.
//
// Compare: the same transform.Apply calls are made and their outputs are compared byte-
// for-byte with the committed golden files. A mismatch indicates that a pipeline change
// altered the output of one or more harness+fixture combinations.
func TestSeamBaseline(t *testing.T) {
	frozenRoot := seamFrozenCatalogRoot(t)
	fixturesDir := seamAgentFixturesDir(t)
	baselineDir := seamBaselineDir(t)

	// Resolve the four built-in harness modules from the frozen catalog root.
	// The blank imports at the top of this file ensure each factory is registered.
	reg, err := registry.Discover(registry.Options{MosaicRoot: frozenRoot})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}

	// Load protocol from the frozen catalog root once per run.
	protocol, err := catalog.FileProtocolLoader{}.LoadProtocol(frozenRoot)
	if err != nil {
		t.Fatalf("load protocol from frozen catalog: %v", err)
	}

	for _, h := range seamHarnesses {
		h := h
		t.Run(h.id, func(t *testing.T) {
			mod, err := reg.Resolve(h.id)
			if err != nil {
				t.Fatalf("resolve harness %q: %v", h.id, err)
			}
			defer mod.Close() //nolint:errcheck

			createDir := filepath.Join(baselineDir, h.dir, "create")
			updateDir := filepath.Join(baselineDir, h.dir, "update")

			if *seamUpdate {
				if err := os.MkdirAll(createDir, 0o755); err != nil {
					t.Fatalf("create dir %s: %v", createDir, err)
				}
				if err := os.MkdirAll(updateDir, 0o755); err != nil {
					t.Fatalf("create dir %s: %v", updateDir, err)
				}
			}

			// Map from fixture file name to create-path output (used to set Deployed for update path).
			createOutputs := make(map[string][]byte, len(seamAgentFixtures))

			// Create-path cases.
			for _, fix := range seamAgentFixtures {
				fix := fix
				t.Run("create/"+fix.key, func(t *testing.T) {
					src := readFixture(t, fixturesDir, fix.file)
					req := transform.Request{
						Source:   src,
						Kind:     domain.ArtifactAgent,
						Key:      fix.key,
						Module:   mod,
						Model:    seamBaselineModel,
						Scope:    domain.ScopeProject,
						Role:     fix.role,
						Protocol: protocol,
					}
					result, err := transform.Apply(req)
					if err != nil {
						t.Fatalf("transform.Apply create/%s: %v", fix.key, err)
					}
					createOutputs[fix.file] = result.Output

					goldenPath := filepath.Join(createDir, fix.key+".md")
					seamCheckOrWrite(t, goldenPath, result.Output)
				})
			}

			// Update-path case: use the create output as prior deployed bytes.
			t.Run("update/"+seamUpdateFixture[:len(seamUpdateFixture)-3], func(t *testing.T) {
				// Find the fixture entry for the update case.
				var updateFix *struct {
					file string
					key  string
					role domain.AgentRole
				}
				for i := range seamAgentFixtures {
					if seamAgentFixtures[i].file == seamUpdateFixture {
						f := seamAgentFixtures[i]
						updateFix = &f
						break
					}
				}
				if updateFix == nil {
					t.Fatalf("update fixture %q not in seamAgentFixtures", seamUpdateFixture)
				}

				prior, ok := createOutputs[updateFix.file]
				if !ok || len(prior) == 0 {
					t.Fatalf("create output for %q not available (create subtest may have failed)", updateFix.file)
				}

				src := readFixture(t, fixturesDir, updateFix.file)
				req := transform.Request{
					Source:   src,
					Kind:     domain.ArtifactAgent,
					Key:      updateFix.key,
					Module:   mod,
					Model:    seamBaselineModel,
					Scope:    domain.ScopeProject,
					Role:     updateFix.role,
					Deployed: prior,
					Protocol: protocol,
				}
				result, err := transform.Apply(req)
				if err != nil {
					t.Fatalf("transform.Apply update/%s: %v", updateFix.key, err)
				}

				priorPath := filepath.Join(updateDir, updateFix.key+".prior.md")
				outputPath := filepath.Join(updateDir, updateFix.key+".output.md")

				seamCheckOrWrite(t, priorPath, prior)
				seamCheckOrWrite(t, outputPath, result.Output)
			})
		})
	}
}

// readFixture reads an agent source fixture from fixturesDir.
func readFixture(t *testing.T, fixturesDir, filename string) []byte {
	t.Helper()
	p := filepath.Join(fixturesDir, filename)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return b
}

// seamCheckOrWrite writes the data to path when in capture mode (-update), or reads
// the committed golden file and fails the test when the output does not match.
func seamCheckOrWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if *seamUpdate {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write baseline %s: %v", path, err)
		}
		t.Logf("wrote baseline: %s (%d bytes)", path, len(data))
		return
	}

	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline %s: %v\n(run: go test ./internal/app/... -run TestSeamBaseline -update)", path, err)
	}
	if !bytes.Equal(data, golden) {
		first := seamFirstDiff(data, golden)
		t.Errorf("output does not match baseline %s\n"+
			"output: %d bytes, golden: %d bytes, first difference at byte %d\n\n"+
			"--- output (first 600 bytes) ---\n%s\n\n--- golden (first 600 bytes) ---\n%s",
			path,
			len(data), len(golden), first,
			seamTruncate(data, 600),
			seamTruncate(golden, 600),
		)
	}
}

func seamFirstDiff(a, b []byte) int {
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

func seamTruncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
