package e2e

// Stage 5 (Harness-Agnostic Suites and Selection): AC5.7's repo-wide sweep —
// "no authored suite file in the repo still declares harness:" — and the
// e2e half of AC5.6 companion to the reflection-based
// TestRunSettings_NoLongerCarriesHarnessIdentity in internal/authoring:
// the exact same unmodified authored suite must run to completion when
// nothing about its content names a harness at all.
//
// Stage-5/Plan.md names this repo's set explicitly: examples.suite.yaml,
// testdata/examples/happy-path.suite.yaml, tests/smoke/smoke.suite.yaml and
// fifteen files under testdata/e2e/*/ — eighteen files in total. Rather than
// hard-code that list (and silently stop covering a nineteenth file added
// later), this test walks the module for every *.suite.yaml and names
// whichever ones still declare the removed key, so I5.5's "remove harness:
// from every authored suite file" has one test it must satisfy rather than
// eighteen individually maintained assertions.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// harnessKeyPattern matches a YAML "harness:" mapping key at any indentation
// level — the shape both suite defaults and a suite entry's own override
// take (see internal/authoring/harness_key_removed_test.go for the parsed
// equivalents of both).
var harnessKeyPattern = regexp.MustCompile(`(?m)^[ \t]*harness:[ \t]*\S`)

// TestNoAuthoredSuiteFileDeclaresHarnessKey is AC5.7.
func TestNoAuthoredSuiteFileDeclaresHarnessKey(t *testing.T) {
	root := moduleRoot(t)
	offenders := findFilesDeclaringHarnessKey(t, root, ".suite.yaml")

	if len(offenders) > 0 {
		t.Errorf("AC5.7: %d authored suite file(s) still declare a removed harness: key:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestNoAuthoredTestDefinitionDeclaresHarnessKey extends the same sweep to
// test-definition files: the schema removal is not suite-only (see
// internal/authoring's TestParseTestDefinition_HarnessKeyInSettings_RejectedAsRemoved).
func TestNoAuthoredTestDefinitionDeclaresHarnessKey(t *testing.T) {
	root := moduleRoot(t)
	offenders := findFilesDeclaringHarnessKey(t, root, ".test.yaml")

	if len(offenders) > 0 {
		t.Errorf("%d authored test-definition file(s) still declare a removed harness: key:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// findFilesDeclaringHarnessKey walks root for every file whose name ends in
// suffix and returns the module-relative paths (sorted, for a diff-stable
// failure message) of every one whose content still matches
// harnessKeyPattern.
func findFilesDeclaringHarnessKey(t *testing.T, root, suffix string) []string {
	t.Helper()

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, suffix) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if harnessKeyPattern.Match(data) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %q for %q files: %v", root, suffix, err)
	}

	sort.Strings(offenders)
	return offenders
}
