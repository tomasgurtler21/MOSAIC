package report_test

// Tests for the subject version appearing in the human-readable (text) report
// and for both renderings agreeing on the subject version (TDD RED phase).
//
// The text rendering prints "subject version: <value>" per run. When
// SubjectVersion is empty the text value is "unknown" — the same mapping the
// JSON wire projection applies, so the two renderings always agree on one
// shared value rather than two independent mappings.
//
// The agreement tests extend the existing rendering-consistency guarantee: two
// reports built from two traversals of the same result model must always agree,
// and adding subject version to one renderer without the other is a divergence
// this test makes visible before it reaches a user.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// TestRenderText_SubjectVersionAppearsInOutput asserts that when a RunReport
// carries a non-empty SubjectVersion, the human-readable report includes a
// "subject version:" line with that exact value. A developer reading the
// terminal output must be able to attribute the result to a specific version
// of the agent under test without opening the machine-readable report.
func TestRenderText_SubjectVersionAppearsInOutput(t *testing.T) {
	// Arrange
	const wantVersion = "v2.5.0"
	result := fixtureResultWithSubjectVersion(wantVersion)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "subject version: v2.5.0" appears somewhere in the output
	wantSubstr := "subject version: " + wantVersion
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report has no %q line;\n"+
			"want the human-readable report to carry the subject version so a "+
			"developer can attribute a terminal result to a specific agent version.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_AbsentSubjectVersionRendersAsUnknown asserts that when a
// RunReport's SubjectVersion is empty — meaning the source declared no version
// — the human-readable report prints "subject version: unknown" rather than
// an empty or missing line. An absent version must be visibly distinct from a
// real one.
func TestRenderText_AbsentSubjectVersionRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithSubjectVersion("") // source declared no version

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "subject version: unknown" appears, never a blank
	const wantSubstr = "subject version: unknown"
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report does not contain %q for an absent source version;\n"+
			"an absent version must render as 'unknown', never as blank or omitted, "+
			"so a reader does not mistake its absence for a real value.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_SubjectVersionNotBlankWhenAbsent asserts that neither an
// empty "subject version: " line nor a completely missing subject-version line
// appears when the source declared no version. Only "unknown" is acceptable.
func TestRenderText_SubjectVersionNotBlankWhenAbsent(t *testing.T) {
	// Arrange
	result := fixtureResultWithSubjectVersion("")

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "subject version: " must not be followed immediately by a
	// newline or only whitespace, which would indicate a blank value.
	blankLine := regexp.MustCompile(`subject version:\s*\n`)
	if blankLine.MatchString(out) {
		t.Errorf("terminal report has a blank 'subject version:' line for an absent source version;\n"+
			"want 'subject version: unknown', never a blank value that reads like a real one.\n"+
			"--- got ---\n%s", out)
	}
}

// TestReportsAgree_SubjectVersionConsistent asserts that both the terminal and
// machine-readable reports carry the same subject version for the same run.
// This is the agreement property for subject version: two reports built from
// the same RunReport.SubjectVersion must report the same value, so a developer
// reading the terminal output and a tool consuming the machine-readable report
// both see the identical version.
func TestReportsAgree_SubjectVersionConsistent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		subjectVersion string
		wantValue      string // the agreed value both renderings must show
	}{
		{"declared version", "v1.2.3", "v1.2.3"},
		{"absent version", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := fixtureResultWithSubjectVersion(tc.subjectVersion)

			// Act
			textOut, textErr := renderText(t, result)
			jsonOut, jsonErr := renderJSON(t, result)

			// Assert no render errors
			if textErr != nil {
				t.Fatalf("RenderText returned an error: %v", textErr)
			}
			if jsonErr != nil {
				t.Fatalf("RenderJSON returned an error: %v", jsonErr)
			}

			// Assert: JSON carries the expected agreed value
			jsonVersion := decodeJSONRunSubjectVersion(t, jsonOut)
			if jsonVersion != tc.wantValue {
				t.Errorf("JSON subject_version = %q, want %q for case %q",
					jsonVersion, tc.wantValue, tc.name)
			}

			// Assert: text carries the expected agreed value
			wantTextLine := "subject version: " + tc.wantValue
			if !strings.Contains(textOut, wantTextLine) {
				t.Errorf("terminal report does not contain %q for case %q;\n"+
					"both renderings must agree on the subject version value.\n"+
					"--- got ---\n%s", wantTextLine, tc.name, textOut)
			}

			// Assert: the two renderings agree with each other (they both show
			// the same value, which is the agreed mapping of SubjectVersion)
			if jsonVersion != tc.wantValue {
				t.Errorf("renderings disagree on subject version for case %q: "+
					"JSON = %q, want %q", tc.name, jsonVersion, tc.wantValue)
			}
		})
	}
}

// TestReportsAgree_SubjectVersionUsesSameMapping asserts that both renderings
// apply the identical "" → "unknown" mapping, rather than each deriving the
// display value independently. If one rendering maps "" to "unknown" and the
// other uses a different sentinel, or skips the field, they disagree.
//
// This test is the specific guard the design calls for: "the existing
// agreement check between the two renderings holds on one shared value rather
// than two independent mappings."
func TestReportsAgree_SubjectVersionUsesSameMapping(t *testing.T) {
	// Arrange: a run with no source version (the absent case, most likely to
	// diverge if the two renderings implement the mapping independently)
	result := fixtureResultWithSubjectVersion("")

	// Act
	textOut, textErr := renderText(t, result)
	jsonOut, jsonErr := renderJSON(t, result)
	if textErr != nil {
		t.Fatalf("RenderText returned an error: %v", textErr)
	}
	if jsonErr != nil {
		t.Fatalf("RenderJSON returned an error: %v", jsonErr)
	}

	// Decode the JSON value
	var doc struct {
		Tests []struct {
			Runs []struct {
				SubjectVersion string `json:"subject_version"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("JSON report has no tests or runs")
	}
	jsonVersion := doc.Tests[0].Runs[0].SubjectVersion

	// The text line we expect to find
	wantTextLine := "subject version: " + jsonVersion

	// Assert: text contains the same version string the JSON carries.
	// If either mapped "" differently, this assertion fires.
	if !strings.Contains(textOut, wantTextLine) {
		t.Errorf("terminal report does not contain %q but JSON subject_version = %q; "+
			"both renderings must apply the same '' → 'unknown' mapping — "+
			"one shared value, not two independent mappings.\n"+
			"--- text ---\n%s", wantTextLine, jsonVersion, textOut)
	}
}
