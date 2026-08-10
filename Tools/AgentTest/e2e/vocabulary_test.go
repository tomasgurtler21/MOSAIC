package e2e

// TestNoAuthoredArtifact_UsesUnNormalizedDispatchVocabulary is T11.4: no
// authored example, fixture or stub registry in the repo may still declare a
// collaborator identity using the harness's own dispatch-tool name (the
// un-normalized vocabulary this stage retires). Every authored "tool" key —
// in a parallel_groups member, an assertions identity, or a stub registry's
// match.tool — must use the normalized, harness-neutral name
// (domain.DispatchToolName, "dispatch") and no other.
//
// Per Stage 11's plan, this is a large mechanical sweep across
// examples/, tests/smoke/ and testdata/e2e/**. This test finds every
// straggler rather than trusting an editor to have caught them all by hand:
// I11.4 is not done until this test is green.
import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// unNormalizedToolPattern matches an authored "tool" identity key (JSON
// `"tool": "..."` or YAML `tool: ...`) whose value is not the normalized
// dispatch-tool name. It deliberately requires the exact key "tool" (with a
// word boundary before it) so it does not false-positive on unrelated keys
// that merely end in "tool", such as subject.allowed_tools.
var unNormalizedToolPattern = regexp.MustCompile(`(?m)\btool"?\s*:\s*"?([A-Za-z][A-Za-z0-9_-]*)"?`)

// sweptVocabularyDirs are the directories Stage 11's plan names explicitly as
// needing the mechanical sweep to the normalized vocabulary.
var sweptVocabularyDirs = []string{"examples", "tests/smoke", "testdata/e2e"}

func TestNoAuthoredArtifact_UsesUnNormalizedDispatchVocabulary(t *testing.T) {
	root := moduleRoot(t)

	var offenders []string
	for _, rel := range sweptVocabularyDirs {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".yaml" && ext != ".yml" && ext != ".json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range unNormalizedToolPattern.FindAllStringSubmatch(string(data), -1) {
				value := m[1]
				if value == "dispatch" {
					continue
				}
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				offenders = append(offenders, rel+": tool="+value)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %q: %v", dir, err)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("found %d authored identity declaration(s) still using the un-normalized dispatch-tool vocabulary; every \"tool\" key must declare %q:\n%s",
			len(offenders), "dispatch", strings.Join(offenders, "\n"))
	}
}
