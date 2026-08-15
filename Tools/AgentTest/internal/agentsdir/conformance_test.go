package agentsdir_test

// Conformance tests for Tools/AgentTest/agents/.
//
// These tests assert the stub agent definition file contract documented in
// Tools/AgentTest/agents/README.md and the Stage 5 design specification:
//
//   - Every *.md file carries the required frontmatter fields with valid values,
//     that role is "subagent" for all files, that name matches the file's base
//     name, and that the required body regions are present.
//   - The directory contains a stub for every agent the brownfield-tdd workflow
//     references, derived from the workflow's own frontmatter.
//   - Local id values are unique across the directory.
//   - Every stub declares recommended_tier equal to domain.TierTestStub.
//
// These tests are written before the stub files are authored in the correct
// format. They fail against the four current comment-only placeholders and pass
// once all 15 stubs are correctly formatted (TDD RED phase).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"

	"mosaic-agent-test/internal/domain"
)

// ---------- path helpers ----------

// sourceFileDir returns the absolute directory of this source file.
// It uses runtime.Caller so the result is correct regardless of the working
// directory at test-run time.
func sourceFileDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — cannot locate test source file")
	}
	return filepath.Dir(filename)
}

// agentsDir returns the absolute path to Tools/AgentTest/agents/.
// Computed relative to this source file: agentsdir/ -> internal/ -> AgentTest/ -> agents/
func agentsDir(t *testing.T) string {
	t.Helper()
	// This file lives at Tools/AgentTest/internal/agentsdir/conformance_test.go.
	// Two levels up reaches Tools/AgentTest/, then into agents/.
	return filepath.Clean(filepath.Join(sourceFileDir(t), "..", "..", "agents"))
}

// repoRoot returns the absolute path to the repository root.
// Computed relative to this source file: agentsdir/ -> internal/ -> AgentTest/ -> Tools/ -> root
func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join(sourceFileDir(t), "..", "..", "..", ".."))
}

// ---------- file-enumeration helper ----------

// agentMDFiles returns the absolute path of every *.md file in the agents
// directory that is a stub definition (i.e. not README.md). Fails the test
// if the directory cannot be read or contains no stub *.md files.
func agentMDFiles(t *testing.T) []string {
	t.Helper()
	dir := agentsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) = %v, want nil", dir, err)
	}
	var paths []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".md") && name != "README.md" {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	if len(paths) == 0 {
		t.Fatalf("no stub *.md files found in %q (README.md is excluded)", dir)
	}
	return paths
}

// ---------- frontmatter-parsing helper ----------

// parseFrontmatter extracts the YAML frontmatter from a markdown file and
// parses it into a map. It returns the map and the body text (everything after
// the closing --- line).
//
// If the file has no frontmatter or the YAML is malformed, parseFrontmatter
// reports a test failure and returns (nil, body). Callers that need the map
// to be non-nil (e.g. for a specific field check) should do:
//
//	fm, _ := parseFrontmatter(t, path)
//	if fm == nil { continue }
func parseFrontmatter(t *testing.T, path string) (map[string]any, string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("%s: ReadFile error: %v", filepath.Base(path), err)
		return nil, ""
	}
	content := string(data)

	const delim = "---"

	// Frontmatter opens with a line that is exactly "---" (leading whitespace
	// permitted on the very first line, but not in practice for agent files).
	firstDelim := strings.Index(content, delim)
	if firstDelim < 0 {
		t.Errorf("%s: no frontmatter delimiter found — file must begin with ---", filepath.Base(path))
		return nil, content
	}

	// Advance past the opening --- and its trailing newline.
	after := content[firstDelim+len(delim):]
	nl := strings.Index(after, "\n")
	if nl < 0 {
		t.Errorf("%s: malformed frontmatter — no newline after opening ---", filepath.Base(path))
		return nil, content
	}
	yamlStart := after[nl+1:]

	// Find the closing --- on its own line.
	closeMarker := "\n" + delim
	closeIdx := strings.Index(yamlStart, closeMarker)
	if closeIdx < 0 {
		t.Errorf("%s: malformed frontmatter — no closing ---", filepath.Base(path))
		return nil, content
	}
	yamlContent := yamlStart[:closeIdx]
	body := yamlStart[closeIdx+len(closeMarker):]

	var fm map[string]any
	if err := goyaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		t.Errorf("%s: YAML parse error in frontmatter: %v", filepath.Base(path), err)
		return nil, body
	}
	return fm, body
}

// ---------- field-checking helpers ----------

// requireStringField extracts field from fm, reports a test failure if the
// field is absent, is not a string, or is empty (after trimming whitespace),
// and returns the value (empty string on failure).
func requireStringField(t *testing.T, fm map[string]any, file, field string) string {
	t.Helper()
	v, ok := fm[field]
	if !ok {
		t.Errorf("%s: frontmatter field %q is absent", file, field)
		return ""
	}
	s, ok := v.(string)
	if !ok {
		t.Errorf("%s: frontmatter field %q has type %T, want string", file, field, v)
		return ""
	}
	if strings.TrimSpace(s) == "" {
		t.Errorf("%s: frontmatter field %q is present but empty", file, field)
		return ""
	}
	return s
}

// toInt attempts to convert a YAML-decoded value to int. go-yaml decodes YAML
// integers into int when decoding to interface{}; this helper guards against
// the remaining integer types for robustness.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		// YAML parsers occasionally return float for numbers; accept only
		// exact integers.
		i := int(n)
		if float64(i) == n {
			return i, true
		}
	}
	return 0, false
}

// ---------- T5.1: frontmatter conformance ----------

// TestAgentStubs_FrontmatterConformance asserts that every *.md file in
// Tools/AgentTest/agents/ carries the required frontmatter fields with valid
// values, that role is "subagent", that name matches the file's base name,
// and that the required body regions are present.
//
// Each file is checked in its own subtest so failures are attributed to the
// specific file. The test must fail against the four current comment-only
// placeholder files before they are rewritten.
func TestAgentStubs_FrontmatterConformance(t *testing.T) {
	for _, path := range agentMDFiles(t) {
		path := path
		base := strings.TrimSuffix(filepath.Base(path), ".md")

		t.Run(base, func(t *testing.T) {
			fm, body := parseFrontmatter(t, path)
			if fm == nil {
				// parseFrontmatter already reported the failure; body checks
				// also cannot pass on a file with no frontmatter.
				t.Errorf("frontmatter is missing or unparseable — body region checks skipped")
				return
			}

			// id: must be present and a positive integer.
			idVal, ok := fm["id"]
			if !ok {
				t.Errorf("frontmatter field \"id\" is absent")
			} else if id, ok := toInt(idVal); !ok {
				t.Errorf("frontmatter field \"id\" has type %T, want an integer", idVal)
			} else if id <= 0 {
				t.Errorf("frontmatter field \"id\" = %d, want a positive integer", id)
			}

			// version: present and non-empty.
			requireStringField(t, fm, base, "version")

			// name: present, non-empty, and must exactly match the file's base
			// name without the .md extension.
			if name := requireStringField(t, fm, base, "name"); name != "" && name != base {
				t.Errorf("frontmatter field \"name\" = %q, want %q (file base name without .md)", name, base)
			}

			// description: present and non-empty.
			requireStringField(t, fm, base, "description")

			// role: present, non-empty, and must be "subagent".
			if role := requireStringField(t, fm, base, "role"); role != "" && role != "subagent" {
				t.Errorf("frontmatter field \"role\" = %q, want \"subagent\"", role)
			}

			// model: present and non-empty.
			requireStringField(t, fm, base, "model")

			// recommended_tier: present, non-empty, and must equal domain.TierTestStub.
			if tier := requireStringField(t, fm, base, "recommended_tier"); tier != "" && tier != domain.TierTestStub {
				t.Errorf("frontmatter field \"recommended_tier\" = %q, want %q", tier, domain.TierTestStub)
			}

			// tier_rationale: present and non-empty.
			requireStringField(t, fm, base, "tier_rationale")

			// tools: key must be present; the value may be an empty list.
			if _, ok := fm["tools"]; !ok {
				t.Errorf("frontmatter field \"tools\" is absent — it must be declared (may be an empty list)")
			}

			// Body must contain an Identity core block.
			if !strings.Contains(body, `<Identity type="core">`) {
				t.Errorf("body does not contain an Identity core block (expected: <Identity type=\"core\">)")
			}

			// Body must contain a CommunicationProtocol managed region.
			if !strings.Contains(body, `<CommunicationProtocol type="managed">`) {
				t.Errorf("body does not contain a CommunicationProtocol managed region (expected: <CommunicationProtocol type=\"managed\">)")
			}
		})
	}
}

// ---------- T5.2: brownfield-tdd coverage ----------

// TestAgentStubs_CoversBrownfieldTDDReferencedAgents asserts that
// Tools/AgentTest/agents/ contains a stub file for every agent listed in the
// referenced_agents frontmatter of Catalog/Workflows/Build/brownfield-tdd.md.
//
// The expected agent set is derived from the workflow file itself rather than
// hardcoded, so this test stays correct as the workflow evolves.
func TestAgentStubs_CoversBrownfieldTDDReferencedAgents(t *testing.T) {
	workflowPath := filepath.Join(
		repoRoot(t),
		"Catalog", "Workflows", "Build", "brownfield-tdd.md",
	)

	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v — cannot load workflow to derive the expected agent set", workflowPath, err)
	}

	referencedAgents := extractWorkflowReferencedAgents(t, workflowPath, string(data))
	if len(referencedAgents) == 0 {
		t.Fatal("brownfield-tdd.md: referenced_agents list is empty — cannot derive expected stub set")
	}

	dir := agentsDir(t)
	for _, agentKey := range referencedAgents {
		stubPath := filepath.Join(dir, agentKey+".md")
		if _, err := os.Stat(stubPath); os.IsNotExist(err) {
			t.Errorf("agents/%s.md: stub file is missing for brownfield-tdd referenced agent %q", agentKey, agentKey)
		}
	}
}

// extractWorkflowReferencedAgents parses the referenced_agents list from a
// workflow file's YAML frontmatter. It fails the test if the frontmatter is
// absent or malformed.
func extractWorkflowReferencedAgents(t *testing.T, path, content string) []string {
	t.Helper()

	const delim = "---"
	firstDelim := strings.Index(content, delim)
	if firstDelim < 0 {
		t.Fatalf("%s: no frontmatter delimiter found", filepath.Base(path))
		return nil
	}
	after := content[firstDelim+len(delim):]
	nl := strings.Index(after, "\n")
	if nl < 0 {
		t.Fatalf("%s: malformed frontmatter — no newline after opening ---", filepath.Base(path))
		return nil
	}
	yamlStart := after[nl+1:]
	closeMarker := "\n" + delim
	closeIdx := strings.Index(yamlStart, closeMarker)
	if closeIdx < 0 {
		t.Fatalf("%s: malformed frontmatter — no closing ---", filepath.Base(path))
		return nil
	}
	yamlContent := yamlStart[:closeIdx]

	type workflowFrontmatter struct {
		ReferencedAgents []string `yaml:"referenced_agents"`
	}
	var wfm workflowFrontmatter
	if err := goyaml.Unmarshal([]byte(yamlContent), &wfm); err != nil {
		t.Fatalf("%s: YAML parse error in frontmatter: %v", filepath.Base(path), err)
		return nil
	}
	return wfm.ReferencedAgents
}

// ---------- T5.3: unique local IDs ----------

// TestAgentStubs_LocalIDsAreUnique asserts that every *.md file in
// Tools/AgentTest/agents/ declares a unique local id value. Duplicate ids
// violate the ID-assignment namespace described in the README and make the
// README's ID table self-contradictory.
//
// A file with no parseable frontmatter also causes this test to fail, because
// such a file has no id at all — an absence that is itself a violation.
func TestAgentStubs_LocalIDsAreUnique(t *testing.T) {
	// seen maps each observed id to the base name of the first file that
	// claimed it.
	seen := make(map[int]string)

	for _, path := range agentMDFiles(t) {
		base := strings.TrimSuffix(filepath.Base(path), ".md")
		fm, _ := parseFrontmatter(t, path)
		if fm == nil {
			// parseFrontmatter already recorded a failure; no id to check.
			continue
		}

		idVal, ok := fm["id"]
		if !ok {
			t.Errorf("%s: frontmatter field \"id\" is absent — cannot verify uniqueness", base)
			continue
		}

		id, ok := toInt(idVal)
		if !ok {
			t.Errorf("%s: frontmatter field \"id\" has type %T, want an integer", base, idVal)
			continue
		}

		if first, dup := seen[id]; dup {
			t.Errorf(
				"id %d is claimed by both %q and %q — ids must be unique within agents/",
				id, first, base,
			)
		} else {
			seen[id] = base
		}
	}
}

// ---------- T5.4: recommended_tier (domain.TierTestStub) ----------

// TestAgentStubs_AllDeclareTestStubTier asserts that every *.md file in
// Tools/AgentTest/agents/ declares recommended_tier equal to domain.TierTestStub.
//
// This is the sole mechanism by which stubs receive the cheap model at deploy
// time. A stub with a different (or absent) recommended_tier falls out of the
// tier-model mapping and deploys with an unresolved model placeholder, on a
// run that still reports success — a silent failure with a real cost.
//
// A file with no parseable frontmatter also causes this test to fail, because
// such a file cannot satisfy the requirement.
func TestAgentStubs_AllDeclareTestStubTier(t *testing.T) {
	const wantTier = domain.TierTestStub

	for _, path := range agentMDFiles(t) {
		base := strings.TrimSuffix(filepath.Base(path), ".md")
		fm, _ := parseFrontmatter(t, path)
		if fm == nil {
			// parseFrontmatter already recorded a failure.
			continue
		}

		tierVal, ok := fm["recommended_tier"]
		if !ok {
			t.Errorf(
				"%s: frontmatter field \"recommended_tier\" is absent — every stub must declare %q",
				base, wantTier,
			)
			continue
		}

		tier, ok := tierVal.(string)
		if !ok {
			t.Errorf(
				"%s: frontmatter field \"recommended_tier\" has type %T, want string",
				base, tierVal,
			)
			continue
		}

		if tier != wantTier {
			t.Errorf(
				"%s: recommended_tier = %q, want %q — stubs must use this exact literal",
				base, tier, wantTier,
			)
		}
	}
}
