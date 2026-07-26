package app_test

// workspace_test.go verifies that askWorkspace strips a surrounding matched pair of double
// quotes from the workspace path text returned by the Interaction layer, matching the format
// produced by Windows Explorer "Copy as path". The tests are exercised end-to-end through
// DeployNew: the workspace answer is scripted via the interaction stub, and the WorkspacePath
// in the returned RunSummary is checked.
//
// Coverage:
//   - Matched pair ("C:\path") is stripped to C:\path
//   - Surrounding whitespace is trimmed before quote stripping
//   - "" (only matched pair, empty after stripping) produces the existing error
//   - Unmatched leading or trailing quote is left unchanged
//   - Double-nested pair (""<path>"") strips only the outermost pair

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// runDeployWithWorkspaceAnswer drives DeployNew with HarnessID and WorkflowIDs pre-answered
// but no pre-answered WorkspacePath, forcing askWorkspace to be called. The interaction stub
// returns workspaceAnswer as the text for QWorkspace. deps is returned so callers can replace
// Planner/Executor before constructing the service (needed when the workspace answer is a
// valid path and the run must succeed).
func runDeployWithWorkspaceAnswer(t *testing.T, workspaceAnswer string) (app.DeployRequest, *interactiontest.Stub, app.Deps) {
	t.Helper()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QWorkspace, "", workspaceAnswer).
		Build()
	deps, _ := newBaseDeps(t, stub)
	req := app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	}
	return req, stub, deps
}

// ---------------------------------------------------------------------------
// Matched-pair stripping
// ---------------------------------------------------------------------------

// TestAskWorkspace_QuotedPath_IsStrippedBeforeUse verifies that when the workspace text
// answer is a path wrapped in a matched pair of double quotes, the quotes are removed and the
// unquoted path appears in RunSummary.WorkspacePath.
func TestAskWorkspace_QuotedPath_IsStrippedBeforeUse(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	quotedPath := `"` + tempDir + `"`
	req, _, deps := runDeployWithWorkspaceAnswer(t, quotedPath)
	// Override planner and executor to expect the bare (unquoted) temp dir so the run succeeds.
	deps.Planner = &stubPlanner{plan: newMinimalPlan(tempDir)}
	deps.Executor = &stubExecutor{result: newMinimalExecResult(tempDir)}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("DeployNew returned unexpected error: %v", err)
	}
	if summary.WorkspacePath != tempDir {
		t.Errorf("WorkspacePath = %q; want %q (outer double-quotes must be stripped before the path is used)",
			summary.WorkspacePath, tempDir)
	}
}

// TestAskWorkspace_WhitespacePlusQuotedPath_IsStripped verifies that surrounding whitespace
// is trimmed before quote stripping, so `  "<dir>"  ` resolves to <dir>.
func TestAskWorkspace_WhitespacePlusQuotedPath_IsStripped(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	paddedQuotedPath := `  "` + tempDir + `"  `
	req, _, deps := runDeployWithWorkspaceAnswer(t, paddedQuotedPath)
	deps.Planner = &stubPlanner{plan: newMinimalPlan(tempDir)}
	deps.Executor = &stubExecutor{result: newMinimalExecResult(tempDir)}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("DeployNew returned unexpected error: %v", err)
	}
	if summary.WorkspacePath != tempDir {
		t.Errorf("WorkspacePath = %q; want %q (surrounding whitespace and outer quotes must both be stripped)",
			summary.WorkspacePath, tempDir)
	}
}

// ---------------------------------------------------------------------------
// Empty after stripping
// ---------------------------------------------------------------------------

// TestAskWorkspace_EmptyAfterStripping_ReturnsError verifies that a workspace answer of `""`
// — a matched quote pair wrapping nothing — produces an error after stripping yields an empty
// string. The existing "no workspace path was provided" outcome must fire.
func TestAskWorkspace_EmptyAfterStripping_ReturnsError(t *testing.T) {
	// Arrange: `""` stripped of its outer matched pair yields an empty string.
	req, _, deps := runDeployWithWorkspaceAnswer(t, `""`)
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal(`expected error for workspace answer of "" (empty after stripping); got nil`)
	}
	if !strings.Contains(err.Error(), "workspace") && !strings.Contains(err.Error(), "path") {
		t.Errorf("error %q should mention workspace or path to help the caller diagnose the problem", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Unmatched quotes — path must be preserved unchanged
// ---------------------------------------------------------------------------

// TestAskWorkspace_UnmatchedLeadingQuote_PathIsPreserved verifies that a path with a leading
// double-quote but no matching trailing quote is returned without modification. Stripping must
// not occur when the outer pair is incomplete.
func TestAskWorkspace_UnmatchedLeadingQuote_PathIsPreserved(t *testing.T) {
	// Arrange — leading " present, no trailing "
	tempDir := t.TempDir()
	rawInput := `"` + tempDir // intentionally no closing "
	req, _, deps := runDeployWithWorkspaceAnswer(t, rawInput)
	svc := app.New(deps)

	// Act — the run may fail (path is invalid) but WorkspacePath is still recorded.
	summary, _ := svc.DeployNew(context.Background(), req)

	// Assert — the path in the summary must still have the leading quote.
	if !strings.HasPrefix(summary.WorkspacePath, `"`) {
		t.Errorf("WorkspacePath = %q; want leading quote preserved (unmatched leading quote must not be stripped)",
			summary.WorkspacePath)
	}
}

// TestAskWorkspace_UnmatchedTrailingQuote_PathIsPreserved verifies that a path with a
// trailing double-quote but no matching leading quote is returned without modification.
func TestAskWorkspace_UnmatchedTrailingQuote_PathIsPreserved(t *testing.T) {
	// Arrange — no leading ", trailing " present
	tempDir := t.TempDir()
	rawInput := tempDir + `"` // intentionally no opening "
	req, _, deps := runDeployWithWorkspaceAnswer(t, rawInput)
	svc := app.New(deps)

	// Act
	summary, _ := svc.DeployNew(context.Background(), req)

	// Assert
	if !strings.HasSuffix(summary.WorkspacePath, `"`) {
		t.Errorf("WorkspacePath = %q; want trailing quote preserved (unmatched trailing quote must not be stripped)",
			summary.WorkspacePath)
	}
}

// ---------------------------------------------------------------------------
// Double-nested quotes — only the outermost pair stripped
// ---------------------------------------------------------------------------

// TestAskWorkspace_DoubleNestedQuotes_OnlyOuterPairStripped verifies that when the workspace
// answer is `""<dir>""` (two leading and two trailing quotes), only the outermost matched pair
// is removed. The result must be `"<dir>"` — one remaining quote on each side — not <dir>.
func TestAskWorkspace_DoubleNestedQuotes_OnlyOuterPairStripped(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	doubleQuoted := `""` + tempDir + `""` // two leading and two trailing quotes
	req, _, deps := runDeployWithWorkspaceAnswer(t, doubleQuoted)
	svc := app.New(deps)

	// Act
	summary, _ := svc.DeployNew(context.Background(), req)

	// Assert — after stripping one outer pair the inner quotes must remain.
	want := `"` + tempDir + `"`
	if summary.WorkspacePath != want {
		t.Errorf("WorkspacePath = %q; want %q (only the outermost matched pair must be stripped, inner quotes preserved)",
			summary.WorkspacePath, want)
	}
}
