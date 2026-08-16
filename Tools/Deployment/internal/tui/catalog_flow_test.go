package tui

// catalog_flow_test.go verifies the catalogue folder screen's role in the TUI flow:
//
//   - Flow placement (T7.4): the screen appears between mode selection and workspace entry,
//     ahead of all catalogue-dependent selection screens.
//   - Pre-fill (T7.5): Options.CatalogFolder pre-fills the screen so an ordinary deployment
//     is Enter-through.
//   - Reload on change (T7.2): confirming a folder different from the loaded one calls
//     ReloadCatalog and adopts the returned service for the remainder of the session.
//   - Reload on unchanged (T7.2): confirming the same folder does not call ReloadCatalog.
//   - Reload failure (T7.3): a failed reload is shown inline, the user stays on the screen,
//     and the previously working service is not replaced.
//
// All tests are in the TDD RED phase: the catalogue folder screen is not yet wired into the
// root model's navigation (screenCatalogFolder is defined but updateMode still advances to
// screenWorkspace). Tests that must reach screenCatalogFolder fail at a precondition and
// print a diagnostic explaining which implementation step is missing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Stub service for catalog flow tests
// ---------------------------------------------------------------------------

// catalogStubService is a minimal app.Service implementation for catalogue flow tests.
// The id field lets tests distinguish the original service from a replacement returned
// by a successful ReloadCatalog call.
type catalogStubService struct {
	id        string
	harnesses []domain.HarnessRef
}

func (s *catalogStubService) ListHarnesses() []domain.HarnessRef { return s.harnesses }
func (s *catalogStubService) DeployNew(_ context.Context, _ app.DeployRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *catalogStubService) Update(_ context.Context, _ app.UpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *catalogStubService) UpdateWorkflows(_ context.Context, _ app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *catalogStubService) Promote(_ context.Context, _ app.PromoteRequest) (app.PromoteResult, error) {
	return app.PromoteResult{}, nil
}
func (s *catalogStubService) TransformHarness(_ context.Context, _ app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	return app.TransformHarnessResult{}, nil
}
func (s *catalogStubService) RenderAgent(_ context.Context, _ app.RenderAgentRequest) (app.RenderAgentResult, error) {
	return app.RenderAgentResult{}, nil
}

func (s *catalogStubService) CheckWorkflowIndex(_ context.Context) (app.IndexCheckResult, error) {
	return app.IndexCheckResult{}, nil
}
func (s *catalogStubService) DeployAgents(_ context.Context, _ app.DeployAgentsRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *catalogStubService) DeployHooks(_ context.Context, _ app.DeployHooksRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}

// ---------------------------------------------------------------------------
// Reload stub
// ---------------------------------------------------------------------------

// reloadStub records calls to a CatalogReloadFunc and returns a configured result.
// The zero value returns a nil service and nil error (no-op); configure resultSvc and/or
// resultErr before use.
type reloadStub struct {
	callCount int
	lastArg   string
	resultSvc app.Service
	resultErr error
}

func (r *reloadStub) reload(folder string) (app.Service, error) {
	r.callCount++
	r.lastArg = folder
	return r.resultSvc, r.resultErr
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var catalogTestHarness = domain.HarnessRef{
	ID:          "catalog-test-harness",
	DisplayName: "Catalog Test Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// newCatalogFlowModel creates a rootModel wired for catalogue flow tests. The workspace
// path is pre-filled so the workspace screen can advance without character-by-character input.
func newCatalogFlowModel(t *testing.T, svc app.Service, catalogFolder string, reload CatalogReloadFunc) *rootModel {
	t.Helper()
	workspace := t.TempDir() // pre-fill workspace so Enter advances through the workspace screen
	return newRootModel(context.Background(), svc, Options{
		Theme:         DefaultTheme(),
		CatalogFolder: catalogFolder,
		ReloadCatalog: reload,
		InitialRequest: &app.DeployRequest{
			WorkspacePath: workspace,
		},
	})
}

// newCatalogSvc returns a catalogStubService with one usable harness.
func newCatalogSvc(id string) *catalogStubService {
	return &catalogStubService{
		id:        id,
		harnesses: []domain.HarnessRef{catalogTestHarness},
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Flow placement: catalogue folder screen precedes workspace
// ---------------------------------------------------------------------------

// TestCatalogFlow_ModeEnter_TransitionsToCatalogFolderScreen verifies that after the user
// selects a mode, the root model shows the catalogue folder screen — not the workspace screen.
// This asserts the critical ordering requirement: harness → mode → catalogue folder → workspace.
//
// Fails RED because updateMode currently sets m.screen = screenWorkspace without the
// Stage 7 catalogue folder insertion. The test documents the required post-Stage-7 behaviour.
func TestCatalogFlow_ModeEnter_TransitionsToCatalogFolderScreen(t *testing.T) {
	// Arrange
	catalogFolder := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	// Navigate to mode screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	if m.screen != screenMode {
		t.Fatalf("precondition: screen = %v, want screenMode", m.screen)
	}

	// Act: confirm mode selection.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → should be screenCatalogFolder

	// Assert: catalogue folder screen must appear before workspace.
	if m.screen != screenCatalogFolder {
		t.Errorf("screen = %v after mode Enter; want screenCatalogFolder — "+
			"the catalogue folder screen must be inserted between mode selection and workspace entry; "+
			"implement updateMode to set m.screen = screenCatalogFolder instead of screenWorkspace",
			m.screen)
	}
}

// TestCatalogFlow_CatalogFolderEnter_TransitionsToWorkspaceScreen verifies that after the
// user confirms a valid catalogue folder, the root model advances to the workspace screen.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_CatalogFolderEnter_TransitionsToWorkspaceScreen(t *testing.T) {
	// Arrange
	catalogFolder := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	// Navigate through harness and mode.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder (post-Stage-7)

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder — "+
			"cannot test catalogue→workspace transition until Stage 7 places the screen after mode", m.screen)
	}

	// Act: confirm the pre-filled catalogue folder.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // catalogFolder → workspace

	// Assert
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after catalogue folder Enter; want screenWorkspace — "+
			"confirming a valid catalogue folder must advance to the workspace screen",
			m.screen)
	}
}

// TestCatalogFlow_CatalogFolderEsc_TransitionsBackToMode verifies that pressing Esc on the
// catalogue folder screen returns the user to mode selection, matching every other entry
// screen's back-navigation behaviour.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_CatalogFolderEsc_TransitionsBackToMode(t *testing.T) {
	// Arrange
	catalogFolder := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder (post-Stage-7)

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // catalogFolder → mode

	// Assert
	if m.screen != screenMode {
		t.Errorf("screen = %v after catalogue folder Esc; want screenMode — "+
			"Esc from the catalogue folder screen must return to mode selection",
			m.screen)
	}
}

// TestCatalogFlow_CatalogFolderEsc_PreservesEnteredPath verifies that after Esc from the
// catalogue folder screen, the path entered on that screen is preserved so the user does not
// have to retype it when they return.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_CatalogFolderEsc_PreservesEnteredPath(t *testing.T) {
	// Arrange
	catalogFolder := t.TempDir()
	customFolder := t.TempDir() // a different folder the user would have typed
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// Simulate the user typing a custom folder path.
	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(customFolder)
	}

	// Act: Esc back to mode, then re-enter catalogue folder screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder again

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder after re-entering", m.screen)
	}

	// Assert: the custom folder must still be shown.
	view := m.View()
	if !strings.Contains(view, customFolder) {
		t.Errorf("view does not contain the previously entered path %q after back-navigation; "+
			"the entered catalogue folder must be preserved across Esc and re-entry:\n%s",
			customFolder, view)
	}
}

// TestCatalogFlow_CtrlC_FromCatalogFolderScreen_CancelsContextAndQuits verifies that ctrl+c
// from the catalogue folder screen cancels the model's context and returns a quit command,
// matching the global cancel behaviour of all other entry screens.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_CtrlC_FromCatalogFolderScreen_CancelsContextAndQuits(t *testing.T) {
	// Arrange
	catalogFolder := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// Act
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Assert
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c from catalogue folder screen; context must be cancelled")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c from catalogue folder screen; want tea.Quit")
	}
}

// TestCatalogFlow_WorkspaceEsc_ReturnsToCatalogFolderScreen verifies that pressing Esc on the
// workspace screen returns the user to the catalogue folder screen (not mode selection, as it
// did before Stage 7). The workspace screen's back target shifts from mode to catalogue folder
// when the catalogue folder screen is inserted into the flow.
//
// In pre-Stage-7 code, workspace Esc returns to screenMode, so this test fails RED.
func TestCatalogFlow_WorkspaceEsc_ReturnsToCatalogFolderScreen(t *testing.T) {
	// Arrange — navigate through the full new entry flow to reach workspace.
	catalogFolder := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder (post-Stage-7)

	if m.screen != screenCatalogFolder {
		// Pre-Stage-7: mode goes directly to workspace. Test the workspace Esc target
		// in the old flow to confirm it currently goes to mode (not catalogFolder).
		// After Stage-7, this branch is never taken.
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // workspace → mode (pre-Stage-7)
		if m.screen == screenCatalogFolder {
			return // already correct — implementation must have landed
		}
		t.Fatalf("screen = %v after mode Enter; want screenCatalogFolder — "+
			"cannot test workspace-back-to-catalogFolder until Stage 7 inserts the catalogue screen", m.screen)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // catalogFolder → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v; want screenWorkspace for Esc test", m.screen)
	}

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Assert
	if m.screen != screenCatalogFolder {
		t.Errorf("screen = %v after workspace Esc; want screenCatalogFolder — "+
			"workspace Esc must return to the catalogue folder screen, not mode selection, "+
			"because the catalogue folder screen now sits between mode and workspace",
			m.screen)
	}
}

// ---------------------------------------------------------------------------
// T7.5 — Pre-fill from Options.CatalogFolder
// ---------------------------------------------------------------------------

// TestCatalogFlow_Options_CatalogFolder_PreFillsScreen verifies that the CatalogFolder field
// in Options is used to pre-fill the catalogue folder screen. This ensures that when the tool
// is launched with --catalog-folder the user sees that value on the screen and can confirm it
// with a single Enter, without retyping.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_Options_CatalogFolder_PreFillsScreen(t *testing.T) {
	// Arrange — supply a recognisable catalogue folder via Options.
	customCatalog := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), customCatalog, nil)

	// Navigate to the catalogue folder screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder — "+
			"cannot verify pre-fill until Stage 7 places the catalogue folder screen in the flow", m.screen)
	}

	// Assert: the screen view must contain the pre-filled path.
	view := m.View()
	if !strings.Contains(view, customCatalog) {
		t.Errorf("catalogue folder screen view does not contain the pre-filled path %q; "+
			"Options.CatalogFolder must pre-fill the catalogue folder screen so "+
			"an ordinary deployment is Enter-through:\n%s", customCatalog, view)
	}
}

// TestCatalogFlow_EmptyCatalogFolder_DoesNotPreFill verifies that when Options.CatalogFolder
// is empty, the screen starts with an empty input (or the system default), not a bogus value.
func TestCatalogFlow_EmptyCatalogFolder_DoesNotPreFill(t *testing.T) {
	// Arrange — no CatalogFolder supplied.
	m := newRootModel(context.Background(), newCatalogSvc("orig"), Options{
		Theme: DefaultTheme(),
	})

	// Assert: the selections should start with an empty catalog folder.
	if m.selections.catalogFolder != "" {
		t.Errorf("selections.catalogFolder = %q; want empty string when Options.CatalogFolder is not set",
			m.selections.catalogFolder)
	}
}

// TestCatalogFlow_CatalogFolder_StoredInSelections verifies that Options.CatalogFolder is
// stored in the model's selections.catalogFolder field, making it available to the rest of
// the session even before the user interacts with the screen.
func TestCatalogFlow_CatalogFolder_StoredInSelections(t *testing.T) {
	// Arrange
	customCatalog := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), customCatalog, nil)

	// Assert: the initial catalog folder must be in selections.
	if m.selections.catalogFolder != customCatalog {
		t.Errorf("selections.catalogFolder = %q; want %q — "+
			"Options.CatalogFolder must be stored in entrySelections.catalogFolder at model creation",
			m.selections.catalogFolder, customCatalog)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Reload when a different folder is confirmed
// ---------------------------------------------------------------------------

// TestCatalogFlow_DifferentFolder_CallsReloadCatalog verifies that when the user confirms a
// catalogue folder different from the one currently loaded, ReloadCatalog is called with the
// new path. The call is the mechanism by which the session's catalogue is switched.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_DifferentFolder_CallsReloadCatalog(t *testing.T) {
	// Arrange
	originalCatalog := t.TempDir()
	differentCatalog := t.TempDir()

	newSvc := newCatalogSvc("new-svc")
	stub := &reloadStub{resultSvc: newSvc}
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), originalCatalog, stub.reload)

	// Navigate to catalogue folder screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// Simulate typing a different folder.
	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(differentCatalog)
	}

	// Act: confirm the different folder.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: ReloadCatalog must have been called.
	if stub.callCount == 0 {
		t.Error("ReloadCatalog was not called after confirming a different catalogue folder; "+
			"want exactly one call — the root model must invoke ReloadCatalog when the confirmed "+
			"folder differs from the one currently loaded")
	}
	if stub.callCount > 0 && stub.lastArg != differentCatalog {
		t.Errorf("ReloadCatalog called with %q; want %q — "+
			"the new catalogue folder path must be passed to ReloadCatalog",
			stub.lastArg, differentCatalog)
	}
}

// TestCatalogFlow_DifferentFolder_AdoptsNewServiceOnSuccess verifies that when ReloadCatalog
// succeeds, the returned service replaces the model's active service for the remainder of the
// session. This is the all-or-nothing swap: the new service is adopted only on success.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_DifferentFolder_AdoptsNewServiceOnSuccess(t *testing.T) {
	// Arrange
	originalCatalog := t.TempDir()
	differentCatalog := t.TempDir()

	origSvc := newCatalogSvc("orig-svc")
	newSvc := newCatalogSvc("new-svc")
	stub := &reloadStub{resultSvc: newSvc} // reload succeeds, returning newSvc
	m := newCatalogFlowModel(t, origSvc, originalCatalog, stub.reload)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(differentCatalog)
	}

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: m.svc must now be the service returned by ReloadCatalog.
	if m.svc != newSvc {
		t.Errorf("m.svc is %v after successful reload; want the service returned by ReloadCatalog — "+
			"the root model must adopt the new service for the remainder of the session",
			m.svc)
	}
}

// TestCatalogFlow_DifferentFolder_AdvancesOnSuccess verifies that after a successful reload,
// the root model advances past the catalogue folder screen to the workspace screen.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_DifferentFolder_AdvancesOnSuccess(t *testing.T) {
	// Arrange
	originalCatalog := t.TempDir()
	differentCatalog := t.TempDir()

	newSvc := newCatalogSvc("new-svc")
	stub := &reloadStub{resultSvc: newSvc}
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), originalCatalog, stub.reload)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(differentCatalog)
	}

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: model must advance to workspace.
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after successful reload; want screenWorkspace — "+
			"a successful reload must advance the model past the catalogue folder screen",
			m.screen)
	}
}

// TestCatalogFlow_SameFolder_DoesNotCallReloadCatalog verifies that when the user confirms
// the same catalogue folder that is already loaded, ReloadCatalog is NOT called. There is
// no need to reload when the folder has not changed.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_SameFolder_DoesNotCallReloadCatalog(t *testing.T) {
	// Arrange — same folder for both the pre-loaded catalog and the confirmed input.
	catalogFolder := t.TempDir()
	stub := &reloadStub{} // will record any calls
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, stub.reload)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// The screen is pre-filled with catalogFolder (same as currently loaded); confirm it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: ReloadCatalog must NOT have been called.
	if stub.callCount > 0 {
		t.Errorf("ReloadCatalog called %d time(s) when the same folder was confirmed; want 0 calls — "+
			"confirming the already-loaded folder must not trigger a reload",
			stub.callCount)
	}
}

// TestCatalogFlow_SameFolder_AdvancesWithoutReload verifies that when the same folder is
// confirmed (no reload needed), the model still advances to the workspace screen.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_SameFolder_AdvancesWithoutReload(t *testing.T) {
	// Arrange
	catalogFolder := t.TempDir()
	stub := &reloadStub{}
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, stub.reload)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// Confirm the pre-filled (unchanged) folder.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after confirming same folder; want screenWorkspace — "+
			"confirming the same folder must advance to workspace without calling ReloadCatalog",
			m.screen)
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Reload failure is surfaced and non-destructive
// ---------------------------------------------------------------------------

// TestCatalogFlow_ReloadFailure_KeepsUserOnCatalogFolderScreen verifies that when
// ReloadCatalog returns an error, the root model stays on the catalogue folder screen rather
// than advancing. The user must be able to correct the path or choose a different folder.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_ReloadFailure_KeepsUserOnCatalogFolderScreen(t *testing.T) {
	// Arrange
	originalCatalog := t.TempDir()
	differentCatalog := t.TempDir()

	stub := &reloadStub{resultErr: errors.New("catalog load failed: Agents directory unreadable")}
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), originalCatalog, stub.reload)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(differentCatalog)
	}

	// Act: confirm — reload fails.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: user must stay on catalogue folder screen.
	if m.screen != screenCatalogFolder {
		t.Errorf("screen = %v after reload failure; want screenCatalogFolder — "+
			"a failed reload must not advance the model; the user must stay on the screen "+
			"to correct the path or choose a different folder",
			m.screen)
	}
}

// TestCatalogFlow_ReloadFailure_DoesNotReplaceWorkingService verifies that when
// ReloadCatalog returns an error, the model's service is NOT replaced. The session continues
// with the service that was working before the reload attempt.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_ReloadFailure_DoesNotReplaceWorkingService(t *testing.T) {
	// Arrange
	originalCatalog := t.TempDir()
	differentCatalog := t.TempDir()

	origSvc := newCatalogSvc("orig-svc")
	stub := &reloadStub{
		resultSvc: nil, // broken service — never returned because resultErr is set
		resultErr: errors.New("catalog load failed"),
	}
	m := newCatalogFlowModel(t, origSvc, originalCatalog, stub.reload)
	originalSvcRef := m.svc // capture before any reload attempt

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(differentCatalog)
	}

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: m.svc must still be the original service.
	if m.svc != originalSvcRef {
		t.Errorf("m.svc was replaced after a failed reload; want the original service to be retained — "+
			"a failed reload must leave the session's working catalogue intact; "+
			"a partially-built service must never be adopted")
	}
}

// TestCatalogFlow_ReloadFailure_ShowsErrorOnScreen verifies that after a failed reload the
// error is surfaced inline on the catalogue folder screen via SetExternalError, giving the
// user feedback about what went wrong.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented (and
// SetExternalError is a no-op stub until its implementation is delivered).
func TestCatalogFlow_ReloadFailure_ShowsErrorOnScreen(t *testing.T) {
	// Arrange
	originalCatalog := t.TempDir()
	differentCatalog := t.TempDir()

	reloadErrMsg := "catalog load failed: directory is not a valid MOSAIC catalogue root"
	stub := &reloadStub{resultErr: errors.New(reloadErrMsg)}
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), originalCatalog, stub.reload)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	if m.catalogFolderScreen != nil {
		m.catalogFolderScreen.SetPrefilledPath(differentCatalog)
	}

	// Act: confirm — reload fails.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: the screen view must contain the reload error message.
	view := m.View()
	if !strings.Contains(view, "catalog load failed") {
		t.Errorf("view does not contain the reload error message after failure; "+
			"want the error to be shown inline via SetExternalError so the user knows "+
			"why the reload failed:\n%s", view)
	}
}

// TestCatalogFlow_NilReloadCatalog_AdvancesWithoutError verifies that when Options.ReloadCatalog
// is nil (reload capability not provided), confirming the catalogue folder still advances the
// model without panicking or erroring. The catalogue screen still validates the path, but the
// confirmed value has no further effect on the service.
//
// Fails RED: cannot reach screenCatalogFolder until Stage 7 is implemented.
func TestCatalogFlow_NilReloadCatalog_AdvancesWithoutError(t *testing.T) {
	// Arrange — no reload function supplied (nil).
	catalogFolder := t.TempDir()
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), catalogFolder, nil)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → catalogFolder

	if m.screen != screenCatalogFolder {
		t.Fatalf("screen = %v; want screenCatalogFolder", m.screen)
	}

	// Act: confirm the folder (no reload to call).
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic when confirming catalogue folder with nil ReloadCatalog: %v", r)
		}
	}()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: model must advance to workspace without panic.
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after confirming catalogue folder with nil ReloadCatalog; "+
			"want screenWorkspace — nil reload must be handled gracefully, not panic",
			m.screen)
	}
}

// ---------------------------------------------------------------------------
// T7.4 regression — existing screen navigation contracts unchanged
// ---------------------------------------------------------------------------

// TestCatalogFlow_HarnessToMode_NavigationUnchanged verifies that the harness → mode
// transition is not affected by Stage 7 changes. This is a regression guard confirming
// the catalogue folder screen insertion does not perturb the first two entry screens.
func TestCatalogFlow_HarnessToMode_NavigationUnchanged(t *testing.T) {
	// Arrange
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), t.TempDir(), nil)

	if m.screen != screenHarness {
		t.Fatalf("precondition: screen = %v, want screenHarness", m.screen)
	}

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if m.screen != screenMode {
		t.Errorf("screen = %v after harness Enter; want screenMode — "+
			"Stage 7 must not alter the harness→mode transition",
			m.screen)
	}
}

// TestCatalogFlow_ModeEscToHarness_NavigationUnchanged verifies that Esc on the mode screen
// still returns to the harness screen after Stage 7 changes are applied.
func TestCatalogFlow_ModeEscToHarness_NavigationUnchanged(t *testing.T) {
	// Arrange
	m := newCatalogFlowModel(t, newCatalogSvc("orig"), t.TempDir(), nil)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode

	// Act
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // mode → harness

	// Assert
	if m.screen != screenHarness {
		t.Errorf("screen = %v after mode Esc; want screenHarness — "+
			"Stage 7 must not alter the mode→harness back-navigation",
			m.screen)
	}
}

// ---------------------------------------------------------------------------
// Empty CatalogFolder: screen omission contract
// ---------------------------------------------------------------------------

// TestCatalogFlow_EmptyCatalogFolder_ScreenIsNil verifies that when Options.CatalogFolder is
// empty, newRootModel does not create a catalogFolderScreen. The nil screen signals to the
// navigation logic that mode should advance directly to the workspace screen. This is the
// documented behavior for callers (or tests) that do not supply the catalogue folder option.
func TestCatalogFlow_EmptyCatalogFolder_ScreenIsNil(t *testing.T) {
	// Arrange — no CatalogFolder supplied.
	svc := newCatalogSvc("orig")
	m := newRootModel(context.Background(), svc, Options{
		Theme:         DefaultTheme(),
		CatalogFolder: "",
	})

	// Assert: the catalogue folder screen must not be created.
	if m.catalogFolderScreen != nil {
		t.Error("catalogFolderScreen != nil when Options.CatalogFolder is empty; " +
			"an empty CatalogFolder must omit the screen entirely per the Options doc comment")
	}
}

// TestCatalogFlow_EmptyCatalogFolder_ModeAdvancesDirectlyToWorkspace verifies that when
// Options.CatalogFolder is empty (screen omitted), selecting a mode advances directly to the
// workspace screen without stopping at an intermediate catalogue folder screen.
func TestCatalogFlow_EmptyCatalogFolder_ModeAdvancesDirectlyToWorkspace(t *testing.T) {
	// Arrange — no CatalogFolder, but with a usable harness so Enter on harness advances.
	svc := newCatalogSvc("orig")
	m := newRootModel(context.Background(), svc, Options{
		Theme:         DefaultTheme(),
		CatalogFolder: "",
	})

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode

	if m.screen != screenMode {
		t.Fatalf("precondition: screen = %v; want screenMode", m.screen)
	}

	// Act: confirm mode.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → workspace (no catalogue screen)

	// Assert: catalogue folder screen must be skipped when CatalogFolder is empty.
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after mode Enter with empty CatalogFolder; want screenWorkspace — "+
			"empty CatalogFolder must omit the catalogue folder screen and advance directly to workspace",
			m.screen)
	}
}
