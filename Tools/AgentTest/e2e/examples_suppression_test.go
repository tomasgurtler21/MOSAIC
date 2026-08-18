package e2e

// Test for T13.3 / AC13.1: no shipped example under examples/ may declare an
// assertion class its own declared layer suppresses. The repo's flagship
// reference example is documentation-grade; a combination its own layer
// silently ignores teaches something false about the format, discoverable
// only by reading raw JSON output rather than by reading the example.
//
// This intentionally does NOT walk testdata/e2e/layer-rule/: those fixtures
// declare suppression deliberately, to exercise it (AC13.2), and must keep
// declaring it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
)

// declaredAssertionClasses reports every assertion class def.Assertions
// declares, using the same nil-means-not-declared convention
// domain.Assertions documents.
func declaredAssertionClasses(a domain.Assertions) []domain.AssertionClass {
	var out []domain.AssertionClass
	if a.InvocationSequence != nil {
		out = append(out, domain.ClassInvocationSequence)
	}
	if a.ExecutionLogAgentIDs != nil {
		out = append(out, domain.ClassExecutionLogAgentIDs)
	}
	if a.ExecutionLogAllStatus != nil {
		out = append(out, domain.ClassExecutionLogAllStatus)
	}
	if a.FinalPhase != nil {
		out = append(out, domain.ClassFinalPhase)
	}
	if a.FinalStatus != nil {
		out = append(out, domain.ClassFinalStatus)
	}
	if a.ProtocolViolations != nil {
		out = append(out, domain.ClassProtocolViolations)
	}
	if a.ArtifactCreated != nil {
		out = append(out, domain.ClassArtifactCreated)
	}
	if a.ArtifactNotCreated != nil {
		out = append(out, domain.ClassArtifactNotCreated)
	}
	if a.MinConcurrency != nil {
		out = append(out, domain.ClassMinConcurrency)
	}
	if len(a.TaskMessages) > 0 {
		out = append(out, domain.ClassTaskMessage)
	}
	return out
}

// TestExamples_NoShippedExampleDeclaresSuppressedAssertion is the direct
// AC13.1 check: every *.test.yaml under examples/ declares only assertion
// classes its own declared layer actually evaluates.
func TestExamples_NoShippedExampleDeclaresSuppressedAssertion(t *testing.T) {
	examplesDir := filepath.Join(moduleRoot(t), "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", examplesDir, err)
	}

	var checked int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		if filepath.Base(entry.Name()) == "examples.suite.yaml" {
			continue
		}
		path := filepath.Join(examplesDir, entry.Name())
		if !isTestDefinitionFile(t, path) {
			continue
		}
		checked++

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		def, report := authoring.ParseTestDefinition(authoring.Source{Path: path, Data: data})
		if report.HasErrors() {
			t.Fatalf("parsing %s: %+v", path, report.Diagnostics)
		}

		for _, class := range declaredAssertionClasses(def.Assertions) {
			if domain.LayerSuppresses(def.Layer, class) {
				t.Errorf("%s declares layer: %s alongside assertion class %q, which that layer suppresses entirely — the assertion is silently ignored, teaching a false property of a %q layer test", entry.Name(), def.Layer, class, def.Layer)
			}
		}
	}

	if checked == 0 {
		t.Fatalf("found no *.test.yaml files under %s to check", examplesDir)
	}
}

// isTestDefinitionFile distinguishes a *.test.yaml definition from any other
// yaml file that might live under examples/ (a suite, for instance), so this
// test degrades gracefully if the directory's contents change shape rather
// than misparsing a non-definition file as one with every assertion class
// absent.
func isTestDefinitionFile(t *testing.T, path string) bool {
	t.Helper()
	return strings.HasSuffix(strings.TrimSuffix(path, ".yaml"), ".test")
}
