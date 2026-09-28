package artifact_test

// Tests for artifact.Parse: frontmatter happy path, refusal cases, and run_id handling.

import (
	"os"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// ---- Parse: frontmatter happy path ----

func TestParse_CanonicalFile_Type(t *testing.T) {
	state := mustReadCanonical(t)

	if state.Type != "orchestration-artifact" {
		t.Errorf("Type: want %q, got %q", "orchestration-artifact", state.Type)
	}
}

func TestParse_CanonicalFile_WorkflowID(t *testing.T) {
	state := mustReadCanonical(t)

	if state.Workflow != domain.WorkflowID("quick-fix") {
		t.Errorf("Workflow: want %q, got %q", "quick-fix", state.Workflow)
	}
}

func TestParse_CanonicalFile_WorkflowVersion(t *testing.T) {
	state := mustReadCanonical(t)

	if state.WorkflowVersion != domain.WorkflowVersion("3.0") {
		t.Errorf("WorkflowVersion: want %q, got %q", "3.0", state.WorkflowVersion)
	}
}

func TestParse_CanonicalFile_Task(t *testing.T) {
	state := mustReadCanonical(t)

	if state.Task != "Fix the authentication timeout bug" {
		t.Errorf("Task: want %q, got %q", "Fix the authentication timeout bug", state.Task)
	}
}

func TestParse_CanonicalFile_Started(t *testing.T) {
	state := mustReadCanonical(t)

	want := time.Date(2026, 1, 29, 9, 0, 0, 0, time.UTC)
	if !state.Started.Equal(want) {
		t.Errorf("Started: want %v, got %v", want, state.Started)
	}
}

func TestParse_CanonicalFile_GlobalSequence(t *testing.T) {
	state := mustReadCanonical(t)

	if state.GlobalSequence != 2 {
		t.Errorf("GlobalSequence: want 2, got %d", state.GlobalSequence)
	}
}

func TestParse_CanonicalFile_CheckpointsTrue(t *testing.T) {
	state := mustReadCanonical(t)

	// canonical.md has checkpoints: enabled
	if !state.Checkpoints {
		t.Error("Checkpoints: want true (enabled), got false")
	}
}

func TestParse_CanonicalFile_LastUpdated(t *testing.T) {
	state := mustReadCanonical(t)

	want := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)
	if !state.LastUpdated.Equal(want) {
		t.Errorf("LastUpdated: want %v, got %v", want, state.LastUpdated)
	}
}

// ---- Parse: refusal cases ----

func TestParse_MissingTypeField_ReturnsError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("no-type.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error when type field is missing")
	}
}

func TestParse_MissingTypeField_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("no-type.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	asRefusalError(t, err)
}

func TestParse_OldTemplateFormat_ReturnsRefusalError(t *testing.T) {
	// Old template format uses plain markdown headings instead of <Name type="..."> region tags
	// and may have the "Subgent" typo.  Both signatures indicate a non-canonical file.
	data, err := os.ReadFile(fixturePath("old-template.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error for the old template format")
	}
	asRefusalError(t, err)
}

func TestParse_MissingExecutionLogSection_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("missing-exec-log.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error when <ExecutionLog type=\"core\"> is absent")
	}
	asRefusalError(t, err)
}

func TestParse_MissingArtifactsSection_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("missing-artifacts-section.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error when <Artifacts type=\"core\"> is absent")
	}
	asRefusalError(t, err)
}

func TestParse_TruncatedFile_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("truncated.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error for a truncated file")
	}
	asRefusalError(t, err)
}

func TestParse_RefusalError_ComponentIsArtifact(t *testing.T) {
	// Every refusal from artifact.Parse must name "artifact" as the component.
	data, err := os.ReadFile(fixturePath("no-type.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	re := asRefusalError(t, err)
	if re.Component != "artifact" {
		t.Errorf("RefusalError.Component: want %q, got %q", "artifact", re.Component)
	}
}

// ---- Parse: run_id handling ----

func TestParse_RunIDPresentInFrontmatter_PopulatesRunID(t *testing.T) {
	// When the frontmatter contains a run_id field, Parse must populate
	// ArtifactState.RunID with the exact string value.
	const wantRunID = "20260727T170000Z-a3f9"
	data := minimalArtifactBytes(wantRunID)

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if state.RunID != wantRunID {
		t.Errorf("RunID: want %q, got %q", wantRunID, state.RunID)
	}
}

func TestParse_CanonicalFile_RunID(t *testing.T) {
	// The canonical fixture includes run_id in the frontmatter.
	// Parse must populate ArtifactState.RunID from that field.
	const wantRunID = "20260727T170000Z-a3f9"
	state := mustReadCanonical(t)

	if state.RunID != wantRunID {
		t.Errorf("RunID from canonical fixture: want %q, got %q", wantRunID, state.RunID)
	}
}

func TestParse_RunIDAbsentFromFrontmatter_ReturnsEmptyString(t *testing.T) {
	// When the frontmatter has no run_id field (pre-v1.8 artifact),
	// ArtifactState.RunID must be "" — not an error.
	data := minimalArtifactBytes("") // no run_id line

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if state.RunID != "" {
		t.Errorf("RunID: want %q (absent = empty), got %q", "", state.RunID)
	}
}

func TestParse_RunIDAbsentFromFrontmatter_ReturnsNoError(t *testing.T) {
	// Absence of run_id in frontmatter must not cause a parse error.
	// Pre-v1.8 artifacts do not have this field and must parse successfully.
	data := minimalArtifactBytes("")

	_, err := artifact.Parse(data)

	if err != nil {
		t.Errorf("Parse: want no error when run_id absent, got %v", err)
	}
}

// ---- Parse: commits frontmatter field ----

func TestParse_FrontmatterWithCommitsLine_DoesNotRefuse(t *testing.T) {
	// Parse must accept an artifact whose frontmatter carries a commits: line
	// without refusing the file. The value is not stored anywhere; only
	// tolerance is required.
	data := []byte("---\n" +
		"type: orchestration-artifact\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T00:00:00Z\n" +
		"global_sequence: 0\n" +
		"checkpoints: disabled\n" +
		"commits: disabled\n" +
		"current_state:\n" +
		"  phase: null\n" +
		"  stage: null\n" +
		"  last_status: null\n" +
		"  last_agent: null\n" +
		"  error_code: null\n" +
		"---\n" +
		"\n" +
		"<ExecutionLog type=\"core\">\n" +
		"</ExecutionLog>\n" +
		"\n" +
		"<Artifacts type=\"core\">\n" +
		"</Artifacts>\n" +
		"\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"</WorkflowNotes>\n")

	_, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: want no error for a frontmatter carrying commits:, got: %v", err)
	}
}
