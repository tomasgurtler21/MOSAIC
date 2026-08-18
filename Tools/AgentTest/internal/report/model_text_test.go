package report_test

// Tests for the subject model and stub model appearing in the human-readable
// (text) report and for both renderings agreeing on their values (TDD RED
// phase).
//
// The text rendering prints an explicit labelled pair — "subject model: <v>"
// and "stub model: <v>" — per run. When the field is empty the text value is
// "unknown", the same mapping the JSON wire projection applies, so the two
// renderings always agree on one shared value rather than two independent
// mappings.
//
// This mirrors the treatment SubjectVersion already receives and is required
// by the design: "a run with no model recorded must still render a
// well-formed document rather than a blank that reads like a real value."

import (
	"regexp"
	"strings"
	"testing"
)

// TestRenderText_SubjectModelAppearsInOutput asserts that when a RunReport
// carries a non-empty SubjectModel, the human-readable report includes a
// "subject model:" line with that exact value. A developer reading the
// terminal output must be able to attribute a result to the model the subject
// ran on without opening the machine-readable report.
func TestRenderText_SubjectModelAppearsInOutput(t *testing.T) {
	// Arrange
	const wantModel = "claude-opus-4-5"
	result := fixtureResultWithModels(wantModel, "claude-haiku-4-5")

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "subject model: claude-opus-4-5" appears somewhere in the output
	wantSubstr := "subject model: " + wantModel
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report has no %q line;\n"+
			"want the human-readable report to carry the subject model so a "+
			"developer can attribute a terminal result to a specific model.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_StubModelAppearsInOutput asserts that when a RunReport
// carries a non-empty StubModel, the human-readable report includes a
// "stub model:" line with that exact value.
func TestRenderText_StubModelAppearsInOutput(t *testing.T) {
	// Arrange
	const wantStubModel = "claude-haiku-4-5"
	result := fixtureResultWithModels("claude-opus-4-5", wantStubModel)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "stub model: claude-haiku-4-5" appears somewhere in the output
	wantSubstr := "stub model: " + wantStubModel
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report has no %q line;\n"+
			"want the human-readable report to carry the stub model so a "+
			"developer can attribute a terminal result to the stub model used.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_AbsentSubjectModelRendersAsUnknown asserts that when a
// RunReport's SubjectModel is empty — meaning no model was recorded — the
// human-readable report prints "subject model: unknown" rather than an empty
// or missing line. An absent model must be visibly distinct from a real one.
func TestRenderText_AbsentSubjectModelRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("", "") // no model recorded

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "subject model: unknown" appears, never a blank
	const wantSubstr = "subject model: unknown"
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report does not contain %q for an absent subject model;\n"+
			"an absent model must render as 'unknown', never as blank or omitted, "+
			"so a reader does not mistake its absence for a real value.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_AbsentStubModelRendersAsUnknown asserts that when a
// RunReport's StubModel is empty the human-readable report prints
// "stub model: unknown" rather than an empty or missing line.
func TestRenderText_AbsentStubModelRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("", "") // no model recorded

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "stub model: unknown" appears, never a blank
	const wantSubstr = "stub model: unknown"
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report does not contain %q for an absent stub model;\n"+
			"an absent model must render as 'unknown', never as blank or omitted, "+
			"so a reader does not mistake its absence for a real value.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_SubjectModelNotBlankWhenAbsent asserts that neither an empty
// "subject model: " line nor a completely missing subject-model line appears
// when no model was recorded. Only "unknown" is acceptable.
func TestRenderText_SubjectModelNotBlankWhenAbsent(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("", "")

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "subject model: " must not be followed immediately by a
	// newline or only whitespace, which would indicate a blank value.
	blankLine := regexp.MustCompile(`subject model:\s*\n`)
	if blankLine.MatchString(out) {
		t.Errorf("terminal report has a blank 'subject model:' line for an absent model;\n"+
			"want 'subject model: unknown', never a blank value that reads like a real one.\n"+
			"--- got ---\n%s", out)
	}
}

// TestRenderText_StubModelNotBlankWhenAbsent asserts that neither an empty
// "stub model: " line nor a completely missing stub-model line appears when
// no model was recorded.
func TestRenderText_StubModelNotBlankWhenAbsent(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("", "")

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "stub model: " must not be followed immediately by a
	// newline or only whitespace, which would indicate a blank value.
	blankLine := regexp.MustCompile(`stub model:\s*\n`)
	if blankLine.MatchString(out) {
		t.Errorf("terminal report has a blank 'stub model:' line for an absent model;\n"+
			"want 'stub model: unknown', never a blank value that reads like a real one.\n"+
			"--- got ---\n%s", out)
	}
}

// TestReportsAgree_ModelsConsistent asserts that both the terminal and
// machine-readable reports carry the same subject model and stub model for the
// same run. Two reports built from the same RunReport must report the same
// values, so a developer reading the terminal output and a tool consuming the
// machine-readable report both see the identical models.
func TestReportsAgree_ModelsConsistent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		subjectModel string
		stubModel    string
		wantSubject  string // the agreed value both renderings must show
		wantStub     string
	}{
		{"both models recorded", "claude-opus-4-5", "claude-haiku-4-5", "claude-opus-4-5", "claude-haiku-4-5"},
		{"no models recorded", "", "", "unknown", "unknown"},
		{"only subject model", "claude-opus-4-5", "", "claude-opus-4-5", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := fixtureResultWithModels(tc.subjectModel, tc.stubModel)

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

			// Assert: JSON carries the expected agreed values
			jsonSubjectModel := decodeJSONRunSubjectModel(t, jsonOut)
			if jsonSubjectModel != tc.wantSubject {
				t.Errorf("JSON subject_model = %q, want %q for case %q",
					jsonSubjectModel, tc.wantSubject, tc.name)
			}

			jsonStubModel := decodeJSONRunStubModel(t, jsonOut)
			if jsonStubModel != tc.wantStub {
				t.Errorf("JSON stub_model = %q, want %q for case %q",
					jsonStubModel, tc.wantStub, tc.name)
			}

			// Assert: text carries the expected agreed values
			wantSubjectLine := "subject model: " + tc.wantSubject
			if !strings.Contains(textOut, wantSubjectLine) {
				t.Errorf("terminal report does not contain %q for case %q;\n"+
					"both renderings must agree on the subject model value.\n"+
					"--- got ---\n%s", wantSubjectLine, tc.name, textOut)
			}

			wantStubLine := "stub model: " + tc.wantStub
			if !strings.Contains(textOut, wantStubLine) {
				t.Errorf("terminal report does not contain %q for case %q;\n"+
					"both renderings must agree on the stub model value.\n"+
					"--- got ---\n%s", wantStubLine, tc.name, textOut)
			}
		})
	}
}

// TestReportsAgree_ModelsUsesSameMapping asserts that both renderings apply
// the identical "" → "unknown" mapping, rather than each deriving the display
// value independently. If one rendering maps "" to "unknown" and the other
// uses a different sentinel, or skips the field, they disagree.
func TestReportsAgree_ModelsUsesSameMapping(t *testing.T) {
	// Arrange: a run with no models recorded (the absent case, most likely to
	// diverge if the two renderings implement the mapping independently)
	result := fixtureResultWithModels("", "")

	// Act
	textOut, textErr := renderText(t, result)
	jsonOut, jsonErr := renderJSON(t, result)
	if textErr != nil {
		t.Fatalf("RenderText returned an error: %v", textErr)
	}
	if jsonErr != nil {
		t.Fatalf("RenderJSON returned an error: %v", jsonErr)
	}

	// Decode JSON values
	jsonSubjectModel := decodeJSONRunSubjectModel(t, jsonOut)
	jsonStubModel := decodeJSONRunStubModel(t, jsonOut)

	// Assert: text contains the same model strings the JSON carries.
	// If either mapped "" differently, this assertion fires.
	wantSubjectLine := "subject model: " + jsonSubjectModel
	if !strings.Contains(textOut, wantSubjectLine) {
		t.Errorf("terminal report does not contain %q but JSON subject_model = %q; "+
			"both renderings must apply the same '' → 'unknown' mapping — "+
			"one shared value, not two independent mappings.\n"+
			"--- text ---\n%s", wantSubjectLine, jsonSubjectModel, textOut)
	}

	wantStubLine := "stub model: " + jsonStubModel
	if !strings.Contains(textOut, wantStubLine) {
		t.Errorf("terminal report does not contain %q but JSON stub_model = %q; "+
			"both renderings must apply the same '' → 'unknown' mapping — "+
			"one shared value, not two independent mappings.\n"+
			"--- text ---\n%s", wantStubLine, jsonStubModel, textOut)
	}
}
