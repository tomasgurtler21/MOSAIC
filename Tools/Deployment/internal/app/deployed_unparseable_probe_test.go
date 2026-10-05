package app

// deployed_unparseable_probe_test.go verifies how probeDeployedStateWithIndex and
// scanWorkspaceAgents treat an existing file at the planned target path of an id-bearing
// agent when that file cannot be used cleanly.
//
// Verified behaviours:
//   - With an active index and no index entry for the agent's id, an existing file at the planned
//     path that is invalid UTF-8, uses a rejected fence variant, has no frontmatter, or has
//     frontmatter without a usable id is reported present and parse-failed, with the parse
//     problem recorded and every version field empty. This holds whether or not a set of known
//     parse-failed paths is supplied.
//   - A genuinely absent file stays absent.
//   - A file that parses cleanly with a different id stays absent (it belongs to another agent).
//   - A BOM'd deployed agent is indexed by its id, wherever it lives.
//   - The Update-flow workspace scan surfaces a frontmatter-less file whose filename matches a
//     catalog agent as parse-failed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

const (
	unparseableAgentsDir = ".mosaic"
	unparseableAgentKey  = "target-agent"
	unparseableAgentID   = "9"
)

// unparseableFixture is one deployed-file content that cannot be used cleanly; wantProblem is
// a substring the recorded parse problem must contain.
type unparseableFixture struct {
	content     []byte
	wantProblem string
}

// unparseableFixtures returns deployed-file contents, built in Go code, that cannot be used
// cleanly for an agent with id unparseableAgentID, keyed by a short case name.
func unparseableFixtures() map[string]unparseableFixture {
	return map[string]unparseableFixture{
		"invalid-utf8": {
			content:     []byte("---\nid: \"9\"\nversion: \"1.0\"\n---\nbody \xff\xfe broken\n"),
			wantProblem: "line ",
		},
		"leading-blank-line-before-fence": {
			content:     []byte("\n---\nid: \"9\"\nversion: \"1.0\"\n---\nbody\n"),
			wantProblem: "line ",
		},
		"cr-only-line-endings": {
			content:     []byte("---\rid: \"9\"\rversion: \"1.0\"\r---\rbody\r"),
			wantProblem: "line ",
		},
		"no-frontmatter": {
			content:     []byte("# Just markdown\nNo frontmatter here.\n"),
			wantProblem: "no frontmatter",
		},
		"frontmatter-without-id": {
			content:     []byte("---\nversion: \"1.0\"\nmosaic_harness_version: \"2.0\"\n---\nbody\n"),
			wantProblem: "no id",
		},
		"frontmatter-with-empty-id": {
			content:     []byte("---\nid: \"\"\nversion: \"1.0\"\n---\nbody\n"),
			wantProblem: "no id",
		},
	}
}

// probeSingleAgent writes content (when non-nil) at the agent's planned path, builds the real
// index from the agents directory, and probes the agent with the given parseFailedPaths.
func probeSingleAgent(t *testing.T, content []byte, parseFailedPaths map[string]bool) (domain.DeployedArtifactState, string) {
	t.Helper()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, unparseableAgentsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	targetPath := filepath.Join(unparseableAgentsDir, unparseableAgentKey+".md")
	if content != nil {
		writeFile(t, filepath.Join(ws, unparseableAgentsDir), unparseableAgentKey+".md", content)
	}

	agent := domain.Agent{Key: unparseableAgentKey, NumericID: unparseableAgentID, Role: domain.RoleSubagent}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key}, TargetPath: targetPath},
	}
	index := buildDeployedAgentIndex(ws, unparseableAgentsDir)

	result, err := probeDeployedStateWithIndex(ws, paths, "", nil, index,
		map[string]domain.Agent{agent.Key: agent}, parseFailedPaths)
	if err != nil {
		t.Fatalf("probeDeployedStateWithIndex: unexpected error: %v", err)
	}
	return result[targetPath], targetPath
}

func TestProbeDeployedStateWithIndex_UnusableFileAtPlannedPath_ReportedParseFailed(t *testing.T) {
	for name, fx := range unparseableFixtures() {
		for _, withKnownPaths := range []bool{false, true} {
			label := name + "/nil-parse-failed-paths"
			if withKnownPaths {
				label = name + "/known-parse-failed-paths"
			}
			t.Run(label, func(t *testing.T) {
				// Arrange
				var known map[string]bool
				if withKnownPaths {
					known = map[string]bool{filepath.Join(unparseableAgentsDir, unparseableAgentKey+".md"): true}
				}

				// Act
				state, _ := probeSingleAgent(t, fx.content, known)

				// Assert
				if !state.Present {
					t.Fatalf("Present = false; an existing unusable file at the planned path must be reported present")
				}
				if !state.ParseFailed {
					t.Errorf("ParseFailed = false; an unusable file must be reported parse-failed, not planned as new")
				}
				if state.ContentHash == "" {
					t.Errorf("ContentHash is empty; it must be computed from the raw bytes")
				}
				if !strings.Contains(state.ParseProblem, fx.wantProblem) {
					t.Errorf("ParseProblem = %q; want it to contain %q", state.ParseProblem, fx.wantProblem)
				}
				if state.Version != "" || state.HarnessVersion != "" || state.InjectionsVersion != "" ||
					state.ToolMappingsVersion != "" || state.BundleVersion != "" {
					t.Errorf("version fields must all be empty for a parse-failed state, got %+v", state)
				}
			})
		}
	}
}

func TestProbeDeployedStateWithIndex_NoFrontmatterProblem_IsFixedLiteral(t *testing.T) {
	// Act
	state, _ := probeSingleAgent(t, unparseableFixtures()["no-frontmatter"].content, nil)

	// Assert
	if state.ParseProblem != "no frontmatter" {
		t.Errorf("ParseProblem = %q, want %q", state.ParseProblem, "no frontmatter")
	}
}

func TestProbeDeployedStateWithIndex_FrontmatterWithoutID_ProblemIsFixedLiteral(t *testing.T) {
	// Act
	state, _ := probeSingleAgent(t, unparseableFixtures()["frontmatter-without-id"].content, nil)

	// Assert
	if state.ParseProblem != "frontmatter has no id" {
		t.Errorf("ParseProblem = %q, want %q", state.ParseProblem, "frontmatter has no id")
	}
}

func TestProbeDeployedStateWithIndex_AbsentFileAtPlannedPath_StaysAbsent(t *testing.T) {
	// Act
	state, _ := probeSingleAgent(t, nil, nil)

	// Assert
	if state.Present || state.ParseFailed || state.ParseProblem != "" {
		t.Errorf("state = %+v; a genuinely absent file must stay the zero (absent) state", state)
	}
}

func TestProbeDeployedStateWithIndex_CleanFileWithDifferentID_StaysAbsent(t *testing.T) {
	// Arrange - parses cleanly, has frontmatter and a non-empty id that is not this agent's.
	content := []byte("---\nid: \"999\"\nversion: \"1.0\"\n---\nAnother agent.\n")

	// Act
	state, _ := probeSingleAgent(t, content, nil)

	// Assert
	if state.Present || state.ParseFailed || state.ParseProblem != "" {
		t.Errorf("state = %+v; a cleanly parsed file with a different id must keep today's absent result", state)
	}
}

func TestBuildDeployedAgentIndex_BOMFile_IndexedByID(t *testing.T) {
	// Arrange - BOM-prefixed deployed agent under a renamed file name.
	ws := t.TempDir()
	content := append([]byte("\xEF\xBB\xBF"), []byte("---\nid: \"9\"\nversion: \"1.0\"\n---\nbody\n")...)
	writeFile(t, mkdirAll(t, filepath.Join(ws, unparseableAgentsDir)), "renamed.md", content)

	// Act
	index := buildDeployedAgentIndex(ws, unparseableAgentsDir)

	// Assert
	entries := index.Lookup("9")
	if len(entries) != 1 {
		t.Fatalf("Lookup(\"9\") returned %d entries, want 1; a BOM'd deployed agent must be indexed by its id", len(entries))
	}
	if entries[0].TargetPath != filepath.Join(unparseableAgentsDir, "renamed.md") {
		t.Errorf("TargetPath = %q, want the BOM'd file's path", entries[0].TargetPath)
	}
}

func TestScanWorkspaceAgents_NoFrontmatterFile_FilenameMatchesCatalogAgent_ParseFailed(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := "agents"
	writeFile(t, mkdirAll(t, filepath.Join(workspace, agentsDir)), "known-agent.md",
		[]byte("# Plain markdown\nNo frontmatter.\n"))
	cat := newScanCatalog()
	cat.byKey["known-agent"] = domain.Agent{Key: "known-agent", NumericID: "42"}

	// Act
	scan := scanWorkspaceAgents(workspace, agentsDir, cat)

	// Assert
	if len(scan.Matched) != 1 {
		t.Fatalf("Matched has %d entries, want 1; a frontmatter-less file whose name matches a catalog agent must be surfaced", len(scan.Matched))
	}
	got := scan.Matched[0]
	if !got.ParseFailed || got.MatchedBy != MatchByFileNameKeyParseFailed {
		t.Errorf("match = %+v; want ParseFailed with MatchedBy %q", got, MatchByFileNameKeyParseFailed)
	}
	if got.AgentKey != "known-agent" || got.NumericID != "" {
		t.Errorf("match = %+v; want catalog key and empty NumericID", got)
	}
}

// mkdirAll creates dir (and parents) and returns it.
func mkdirAll(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	return dir
}
