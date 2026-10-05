package catalog_test

// source_validation_test.go verifies that catalog.Load refuses to proceed when agent
// sources cannot be interpreted: unparseable files, agents without frontmatter, and
// subagents missing id, name, or version. Every test builds its own temp-dir catalog.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

const validSubagentFM = "id: \"7\"\nname: Demo\nversion: \"1.0\"\n"

// writeSource writes content at <root>/<relPath>, creating parent directories.
func writeSource(t *testing.T, root, relPath string, content []byte) string {
	t.Helper()
	p := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", p, err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write %q: %v", p, err)
	}
	return p
}

// writeFrontmatterSource writes an agent-style file with the given raw frontmatter.
func writeFrontmatterSource(t *testing.T, root, relPath, fm string) string {
	t.Helper()
	return writeSource(t, root, relPath, []byte("---\n"+fm+"---\n\n# Body\n"))
}

// requireInvalidSource asserts err matches the sentinel and mentions every given path.
func requireInvalidSource(t *testing.T, err error, paths ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("catalog.Load returned nil error, want invalid-source error")
	}
	if !errors.Is(err, catalog.ErrCatalogSourceInvalid) {
		t.Fatalf("error does not match ErrCatalogSourceInvalid: %v", err)
	}
	for _, p := range paths {
		if !strings.Contains(err.Error(), p) && !strings.Contains(err.Error(), filepath.ToSlash(p)) {
			t.Errorf("error does not name %q: %v", p, err)
		}
	}
}

func TestLoad_AgentWithoutFrontmatter_FailsNamingFile(t *testing.T) {
	cases := []struct{ name, rel string }{
		{"orchestrator", "Catalog/Orchestrator/orchestrator.md"},
		{"orchestrator script", "Catalog/Orchestrator/orchestrator-script.md"},
		{"subagent", "Catalog/Subagents/Cat/worker.md"},
		{"utility", "Catalog/UtilityAgents/util.md"},
		{"standalone flat", "Catalog/StandaloneAgents/solo.md"},
		{"standalone categorised", "Catalog/StandaloneAgents/Cat/solo.md"},
		{"legacy subagent", "Catalog/Agents/Generic/Agents/Cat/worker.md"},
		{"legacy utility", "Catalog/Agents/Generic/UtilityAgents/util.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := makeTempMosaicRoot(t)
			p := writeSource(t, root, tc.rel, []byte("# Just a body, no frontmatter\n"))

			_, err := catalog.Load(root, "")

			requireInvalidSource(t, err, p)
		})
	}
}

func TestLoad_SubagentMissingRequiredField_FailsNamingFile(t *testing.T) {
	cases := []struct{ name, fm string }{
		{"missing id", "name: Demo\nversion: \"1.0\"\n"},
		{"missing name", "id: \"7\"\nversion: \"1.0\"\n"},
		{"missing version", "id: \"7\"\nname: Demo\n"},
		{"blank id", "id: \"\"\nname: Demo\nversion: \"1.0\"\n"},
		{"blank name", "id: \"7\"\nname: \"\"\nversion: \"1.0\"\n"},
		{"blank version", "id: \"7\"\nname: Demo\nversion: \"\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := makeTempMosaicRoot(t)
			p := writeFrontmatterSource(t, root, "Catalog/Subagents/Cat/worker.md", tc.fm)

			_, err := catalog.Load(root, "")

			requireInvalidSource(t, err, p)
		})
	}
}

func TestLoad_FrontmatterRoleSubagentInUtilityDir_RequiresIdNameVersion(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p := writeFrontmatterSource(t, root, "Catalog/UtilityAgents/util.md", "role: subagent\n")

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_FrontmatterRoleUtilityInSubagentsDir_DoesNotRequireIdNameVersion(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeFrontmatterSource(t, root, "Catalog/Subagents/Cat/helper.md", "role: utility\n")

	if _, err := catalog.Load(root, ""); err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
}

func TestLoad_InvalidUTF8Subagent_FailsNamingFileAndLine(t *testing.T) {
	root := makeTempMosaicRoot(t)
	content := append([]byte("---\n"+validSubagentFM+"---\n\nbad byte: "), 0xff, 0xfe, '\n')
	p := writeSource(t, root, "Catalog/Subagents/Cat/worker.md", content)

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
	// Fixture lines: 1 fence, 2-4 fields, 5 fence, 6 blank, 7 the bad byte.
	if !strings.Contains(err.Error(), "line 7") {
		t.Errorf("error does not state line 7 of the format problem: %v", err)
	}
}

func TestLoad_RejectedFenceVariant_FailsNamingFileLineAndExcerpt(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// A zero-width space before the opening fence is a rejected variant.
	content := []byte("\u200b---\n" + validSubagentFM + "---\n\nBody\n")
	p := writeSource(t, root, "Catalog/Subagents/Cat/worker.md", content)

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
	for _, want := range []string{"line 1", "<U+200B>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not contain %q: %v", want, err)
		}
	}
}

func TestLoad_YAMLErrorInAgent_FailsNamingFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p := writeFrontmatterSource(t, root, "Catalog/UtilityAgents/util.md", "name: [unclosed\n")

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_UTF16Agent_FailsNamingFile(t *testing.T) {
	root := makeTempMosaicRoot(t)
	content := []byte{0xff, 0xfe, '-', 0, '-', 0, '-', 0, '\n', 0}
	p := writeSource(t, root, "Catalog/StandaloneAgents/solo.md", content)

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p)
}

func TestLoad_SeveralBrokenSources_AllNamedInOneError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	p1 := writeSource(t, root, "Catalog/Subagents/Cat/a.md", []byte("no frontmatter\n"))
	p2 := writeFrontmatterSource(t, root, "Catalog/Subagents/Cat/b.md", "id: \"2\"\nname: B\n")
	p3 := writeSource(t, root, "Catalog/UtilityAgents/c.md", []byte("\u200b---\nname: c\n---\n"))
	p4 := writeSource(t, root, "Catalog/Skills/broken/SKILL.md", []byte("\u200b---\nname: s\n---\n"))
	p5 := writeSource(t, root, "Catalog/Workflows/Cat/flow.md", []byte("\u200b---\nname: w\n---\n"))
	writeFrontmatterSource(t, root, "Catalog/Subagents/Cat/ok.md", validSubagentFM)

	_, err := catalog.Load(root, "")

	requireInvalidSource(t, err, p1, p2, p3, p4, p5)
}

func TestLoad_NonSubagentAgentsWithoutIdNameVersion_LoadWithoutError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeFrontmatterSource(t, root, "Catalog/Orchestrator/orchestrator.md", "description: orch\n")
	writeFrontmatterSource(t, root, "Catalog/Orchestrator/orchestrator-script.md", "description: script\n")
	writeFrontmatterSource(t, root, "Catalog/UtilityAgents/util.md", "description: util\n")
	writeFrontmatterSource(t, root, "Catalog/StandaloneAgents/solo.md", "description: solo\n")
	writeFrontmatterSource(t, root, "Catalog/StandaloneAgents/Cat/solo2.md", "description: solo2\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if cat.Orchestrator().Key == "" {
		t.Error("orchestrator was not loaded")
	}
	if _, ok := cat.OrchestratorScript(); !ok {
		t.Error("orchestrator script was not loaded")
	}
	if len(cat.UtilityAgents()) != 1 || len(cat.StandaloneAgents()) != 2 {
		t.Errorf("utilities=%d standalones=%d, want 1 and 2", len(cat.UtilityAgents()), len(cat.StandaloneAgents()))
	}
}

func TestLoad_BOMSubagent_LoadsWithIdNameVersion(t *testing.T) {
	root := makeTempMosaicRoot(t)
	content := []byte("\xEF\xBB\xBF---\n" + validSubagentFM + "---\n\n# Body\n")
	writeSource(t, root, "Catalog/Subagents/Cat/worker.md", content)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	a, ok := cat.Agent("worker")
	if !ok {
		t.Fatal("BOM subagent was not loaded")
	}
	if a.NumericID != "7" || a.Name != "Demo" || a.Version != "1.0" {
		t.Errorf("fields = id %q name %q version %q, want 7 / Demo / 1.0", a.NumericID, a.Name, a.Version)
	}
}
