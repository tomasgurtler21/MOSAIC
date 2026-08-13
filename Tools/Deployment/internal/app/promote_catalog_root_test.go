package app_test

// promote_catalog_root_test.go covers Stage 6 behaviours: promote's write destination
// and category-enumeration path routing through the active catalogue root (CatalogRoot())
// rather than the MOSAIC root.
//
// Tests for T6.1, T6.3, T6.4, and T6.5 are RED-phase TDD tests. They will FAIL until
// the implementation in promote_service.go is updated to use s.deps.Catalog.CatalogRoot()
// instead of s.deps.MosaicRoot for the promote destination and the category-enumeration
// directory. T6.2 tests are correctness guards that verify byte-identical destination
// paths when the active catalogue root is the default {mosaicRoot}/Catalog.
//
// Verified behaviours:
//
// Custom catalogue root — destination paths (T6.1):
//   - A worker-category promote into a custom catalogue root writes the file under
//     <catalogRoot>/Agents/Generic/Agents/{category}/{key}.md, not under the MOSAIC root.
//   - A utility-category promote into a custom catalogue root writes the file under
//     <catalogRoot>/Agents/Generic/UtilityAgents/{key}.md, not under the MOSAIC root.
//   - The resolved absolute destination contains no double Catalog/ segment (double-prefix
//     guard): the Catalog segment must appear exactly once per resolved path.
//
// Default catalogue root — destinations unchanged (T6.2):
//   - When the active catalogue root is {mosaicRoot}/Catalog (the default), a worker
//     promote lands at {mosaicRoot}/Catalog/Agents/Generic/Agents/{category}/{key}.md —
//     byte-for-byte equal to the pre-feature path (AC6.3).
//   - When the active catalogue root is {mosaicRoot}/Catalog, a utility promote lands at
//     {mosaicRoot}/Catalog/Agents/Generic/UtilityAgents/{key}.md (AC6.3).
//
// Sparse custom catalogue — directory creation (T6.3):
//   - Promoting a worker agent into a sparse custom catalogue (no Agents/ subdirectory)
//     creates the missing intermediate directories and writes the file successfully (AC6.4).
//   - Promoting a utility agent into a sparse custom catalogue creates
//     Agents/Generic/UtilityAgents/ and writes the file (AC6.4).
//
// Category enumeration from the active catalogue root (T6.4):
//   - When a category directory exists under the active catalogue root but not under the
//     MOSAIC root, that category is offered among the QPromoteCategory options (AC6.5).
//   - When the active catalogue root is a custom folder, categories that exist only under
//     {mosaicRoot}/Catalog/Agents/Generic/Agents/ are not offered (AC6.5).
//
// Write-then-load round-trip (T6.5):
//   - An agent promoted into a custom catalogue folder is found by a subsequent
//     catalog.Load(mosaicRoot, customFolder) call (AC6.6).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Stage 6 test helper
// ---------------------------------------------------------------------------

// newPromoteDepsWithCatalogRoot builds an app.Deps suitable for Stage 6 promote tests.
// mosaicRoot is a real MOSAIC root directory (e.g. created by writeMosaicRoot).
// catRoot is the active catalogue root; when empty it defaults to the canonical default
// {mosaicRoot}/Catalog, modelling the no-flag case.
//
// The resulting Deps has MosaicRoot == mosaicRoot and Catalog.CatalogRoot() == catRoot,
// so tests can assert that promote's write destination is routed through CatalogRoot().
func newPromoteDepsWithCatalogRoot(t *testing.T, stub *interactiontest.Stub, mosaicRoot, catRoot string) app.Deps {
	t.Helper()
	if catRoot == "" {
		catRoot = filepath.Join(mosaicRoot, "Catalog")
	}
	deps, _ := newBaseDeps(t, stub)
	deps.MosaicRoot = mosaicRoot
	deps.Catalog = &stubCatalog{
		root:        mosaicRoot,
		catalogRoot: catRoot,
	}
	return deps
}

// ---------------------------------------------------------------------------
// categoryOptionsCapture — spy that records options offered for QPromoteCategory
// ---------------------------------------------------------------------------

// categoryOptionsCapture wraps an interactiontest.Stub and intercepts SelectOne calls
// for QPromoteCategory so the offered options can be inspected by the test. All calls
// are forwarded to the embedded stub; only the options for QPromoteCategory are saved.
type categoryOptionsCapture struct {
	*interactiontest.Stub
	capturedCategoryOptions []domain.Option
}

func (c *categoryOptionsCapture) SelectOne(ctx context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QPromoteCategory {
		c.capturedCategoryOptions = append([]domain.Option(nil), q.Options...)
	}
	return c.Stub.SelectOne(ctx, q)
}

// optionIDSet returns the set of Option.ID values from opts, for concise containment assertions.
func optionIDSet(opts []domain.Option) map[string]bool {
	out := make(map[string]bool, len(opts))
	for _, o := range opts {
		out[o.ID] = true
	}
	return out
}

// optionIDList extracts the ID strings from a slice of domain.Option, for display in failure messages.
func optionIDList(opts []domain.Option) []string {
	ids := make([]string, len(opts))
	for i, o := range opts {
		ids[i] = o.ID
	}
	return ids
}

// ---------------------------------------------------------------------------
// T6.1: Custom catalogue root — destination paths
// ---------------------------------------------------------------------------

// TestPromote_CustomCatalogRoot_WorkerDestinationUnderCatalogRoot verifies that when
// the active catalogue root is a custom folder (not the MOSAIC root's default Catalog/),
// a worker-category promote writes the file at <catalogRoot>/Agents/Generic/Agents/{category}/{key}.md.
// FAILS until promoteDestinationPath is rebased on s.deps.Catalog.CatalogRoot().
func TestPromote_CustomCatalogRoot_WorkerDestinationUnderCatalogRoot(t *testing.T) {
	// Arrange — custom catalogue root is a separate temp dir, unrelated to the MOSAIC root.
	mosaicRoot := writeMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, customCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	wantDest := filepath.Join(customCatalogRoot, "Agents", "Generic", "Agents", "TestCategory", "my-harness-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q\n"+
			"Promote must write under the active catalogue root, not under the MOSAIC root.",
			result.DestinationPath, wantDest)
	}

	// The file must exist at the custom catalogue root path.
	if _, statErr := os.Stat(wantDest); os.IsNotExist(statErr) {
		t.Errorf("file does not exist at %q; promote must write into the active catalogue root", wantDest)
	}
}

// TestPromote_CustomCatalogRoot_UtilityDestinationUnderCatalogRoot verifies that when
// the active catalogue root is a custom folder, a utility-category promote writes the
// file at <catalogRoot>/Agents/Generic/UtilityAgents/{key}.md.
// FAILS until promoteDestinationPath is rebased on s.deps.Catalog.CatalogRoot().
func TestPromote_CustomCatalogRoot_UtilityDestinationUnderCatalogRoot(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-utility-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, customCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  app.PromoteCategoryUtility,
		HarnessID: "stub-harness",
	})

	// Assert
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	wantDest := filepath.Join(customCatalogRoot, "Agents", "Generic", "UtilityAgents", "my-utility-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q\n"+
			"Promote must place utility agents under the active catalogue root, not the MOSAIC root.",
			result.DestinationPath, wantDest)
	}

	if _, statErr := os.Stat(wantDest); os.IsNotExist(statErr) {
		t.Errorf("utility agent file does not exist at %q after promote into custom catalogue root", wantDest)
	}
}

// TestPromote_CustomCatalogRoot_DestinationHasNoCatalogCatalogSegment verifies that the
// resolved destination path contains no double Catalog/ segment. The Catalog segment must
// appear exactly once: in the catalogue root value, not again in the sub-path constants.
// This guards the classic mis-implementation where Catalog/ is applied at two layers.
// FAILS until promoteDestinationPath no longer prepends a Catalog/ segment of its own.
func TestPromote_CustomCatalogRoot_DestinationHasNoCatalogCatalogSegment(t *testing.T) {
	// Arrange — use a catalogue root whose name contains "Catalog" so a double-prefix
	// bug produces a visible "Catalog/Catalog/" segment in the absolute path.
	mosaicRoot := writeMosaicRoot(t)
	// The custom catalog root itself is named "Catalog", the classic default name.
	customCatalogRoot := filepath.Join(t.TempDir(), "Catalog")
	if err := os.MkdirAll(customCatalogRoot, 0o755); err != nil {
		t.Fatalf("mkdir custom catalog root: %v", err)
	}

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, customCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert — no double Catalog/ segment.
	doubleSep := "Catalog" + string(filepath.Separator) + "Catalog"
	if strings.Contains(result.DestinationPath, doubleSep) {
		t.Errorf("DestinationPath %q contains %q; the Catalog segment must appear exactly once — "+
			"in the catalogue root value, not also in the sub-path constant.",
			result.DestinationPath, doubleSep)
	}
}

// ---------------------------------------------------------------------------
// T6.2: Default catalogue root — destinations unchanged (correctness guards)
// ---------------------------------------------------------------------------

// TestPromote_DefaultCatalogRoot_WorkerDestinationMatchesPreFeatureDefault verifies that
// when the active catalogue root is the default {mosaicRoot}/Catalog, a worker promote
// writes to the same path as before the feature: {mosaicRoot}/Catalog/Agents/Generic/Agents/
// {category}/{key}.md. This guards the "double-prefix" risk in both directions (AC6.3).
func TestPromote_DefaultCatalogRoot_WorkerDestinationMatchesPreFeatureDefault(t *testing.T) {
	// Arrange — catalogue root is explicitly the default value.
	mosaicRoot := writeMosaicRoot(t)
	defaultCatalogRoot := filepath.Join(mosaicRoot, "Catalog")

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, defaultCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// The pre-feature path is {mosaicRoot}/Catalog/Agents/Generic/Agents/{category}/{key}.md.
	wantDest := filepath.Join(mosaicRoot, "Catalog", "Agents", "Generic", "Agents", "TestCategory", "my-harness-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q\n"+
			"With the default catalogue root, the destination must be byte-identical to the pre-feature path (AC6.3).",
			result.DestinationPath, wantDest)
	}
}

// TestPromote_DefaultCatalogRoot_UtilityDestinationMatchesPreFeatureDefault verifies that
// when the active catalogue root is the default {mosaicRoot}/Catalog, a utility promote
// writes to {mosaicRoot}/Catalog/Agents/Generic/UtilityAgents/{key}.md — byte-for-byte
// equal to the pre-feature path (AC6.3).
func TestPromote_DefaultCatalogRoot_UtilityDestinationMatchesPreFeatureDefault(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	defaultCatalogRoot := filepath.Join(mosaicRoot, "Catalog")

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-utility-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, defaultCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  app.PromoteCategoryUtility,
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	wantDest := filepath.Join(mosaicRoot, "Catalog", "Agents", "Generic", "UtilityAgents", "my-utility-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q\n"+
			"With the default catalogue root, the utility destination must be byte-identical to the pre-feature path (AC6.3).",
			result.DestinationPath, wantDest)
	}
}

// ---------------------------------------------------------------------------
// T6.3: Sparse custom catalogue — directory creation
// ---------------------------------------------------------------------------

// TestPromote_SparseCustomCatalog_WorkerCategory_CreatesDirectoriesAndWritesFile verifies
// that promoting a worker agent into a completely sparse custom catalogue (no Agents/ tree)
// creates the required intermediate directories and writes the file. Promote must not fail
// on a missing parent directory in the target catalogue folder (AC6.4).
// FAILS until os.MkdirAll is driven from the active catalogue root.
func TestPromote_SparseCustomCatalog_WorkerCategory_CreatesDirectoriesAndWritesFile(t *testing.T) {
	// Arrange — empty custom catalogue root (no Agents/, no Generic/, no Agents/TestCategory/).
	mosaicRoot := writeMosaicRoot(t)
	sparseCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "promoted-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, sparseCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "Audit",
		HarnessID: "stub-harness",
	})

	// Assert — promote must not fail on the missing parent directory.
	if err != nil {
		t.Fatalf("Promote into sparse custom catalogue: unexpected error: %v\n"+
			"Promote must create missing intermediate directories rather than returning an error.",
			err)
	}

	wantDest := filepath.Join(sparseCatalogRoot, "Agents", "Generic", "Agents", "Audit", "promoted-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q", result.DestinationPath, wantDest)
	}

	// The file must have been written under the custom catalogue root.
	if _, statErr := os.Stat(wantDest); os.IsNotExist(statErr) {
		t.Errorf("file does not exist at %q after promote into sparse catalogue; "+
			"intermediate directories must be created and the file must be written", wantDest)
	}
}

// TestPromote_SparseCustomCatalog_UtilityCategory_CreatesDirectoriesAndWritesFile verifies
// that promoting a utility agent into a sparse custom catalogue creates
// Agents/Generic/UtilityAgents/ (if absent) and writes the file (AC6.4).
// FAILS until os.MkdirAll is driven from the active catalogue root.
func TestPromote_SparseCustomCatalog_UtilityCategory_CreatesDirectoriesAndWritesFile(t *testing.T) {
	// Arrange — empty custom catalogue root.
	mosaicRoot := writeMosaicRoot(t)
	sparseCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-utility-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, sparseCatalogRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  app.PromoteCategoryUtility,
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote utility into sparse catalogue: unexpected error: %v\n"+
			"Promote must create Agents/Generic/UtilityAgents/ when absent.",
			err)
	}

	wantDest := filepath.Join(sparseCatalogRoot, "Agents", "Generic", "UtilityAgents", "my-utility-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q", result.DestinationPath, wantDest)
	}

	if _, statErr := os.Stat(wantDest); os.IsNotExist(statErr) {
		t.Errorf("utility agent file does not exist at %q after promote into sparse catalogue", wantDest)
	}
}

// ---------------------------------------------------------------------------
// T6.4: Category enumeration from the active catalogue root
// ---------------------------------------------------------------------------

// TestPromote_CategoryEnumeration_OffersCustomCatalogRootCategories verifies that when
// the active catalogue root contains a category directory that does not exist under the
// MOSAIC root, that category is included in the options offered for QPromoteCategory.
// This confirms that buildPromoteCategoryOptions reads from CatalogRoot(), not MosaicRoot.
// FAILS until buildPromoteCategoryOptions is rebased on s.deps.Catalog.CatalogRoot().
func TestPromote_CategoryEnumeration_OffersCustomCatalogRootCategories(t *testing.T) {
	// Arrange — custom catalogue root has "CustomRootCategory"; MOSAIC root does not.
	mosaicRoot := writeMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	customCategoryDir := filepath.Join(customCatalogRoot, "Agents", "Generic", "Agents", "CustomRootCategory")
	if err := os.MkdirAll(customCategoryDir, 0o755); err != nil {
		t.Fatalf("mkdir custom category under catalog root: %v", err)
	}

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	// Configure the base stub to answer "CustomRootCategory" when QPromoteCategory is asked
	// (subject is the source file path, matching askPromoteCategory's Question.Subject).
	baseStub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, srcPath, "CustomRootCategory").
		Build()
	spy := &categoryOptionsCapture{Stub: baseStub}

	deps := newPromoteDepsWithCatalogRoot(t, baseStub, mosaicRoot, customCatalogRoot)
	deps.Interaction = spy // override to use the capturing spy
	svc := app.New(deps)

	// Act — empty Category triggers the QPromoteCategory prompt.
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "",
		HarnessID: "stub-harness",
	})

	// Assert — the spy must have captured options, and "CustomRootCategory" must be among them.
	if len(spy.capturedCategoryOptions) == 0 {
		t.Fatal("QPromoteCategory was never called with options; " +
			"category enumeration must invoke SelectOne with the catalogue root's category directories")
	}

	idSet := optionIDSet(spy.capturedCategoryOptions)
	if !idSet["CustomRootCategory"] {
		t.Errorf("QPromoteCategory options = %v; want to include %q\n"+
			"Category enumeration must read from the active catalogue root, not from the MOSAIC root.",
			optionIDList(spy.capturedCategoryOptions), "CustomRootCategory")
	}
}

// TestPromote_CategoryEnumeration_DoesNotOfferMosaicRootOnlyCategories verifies that when
// the active catalogue root is a custom folder, category directories that exist only under
// {mosaicRoot}/Catalog/Agents/Generic/Agents/ are NOT offered in QPromoteCategory options.
// The enumeration must read from CatalogRoot(), not from the MOSAIC root.
// FAILS until buildPromoteCategoryOptions is rebased on s.deps.Catalog.CatalogRoot().
func TestPromote_CategoryEnumeration_DoesNotOfferMosaicRootOnlyCategories(t *testing.T) {
	// Arrange — "MosaicOnlyCategory" exists under the MOSAIC root; custom catalogue root is empty.
	mosaicRoot := writeMosaicRoot(t)

	mosaicCategoryDir := filepath.Join(mosaicRoot, "Catalog", "Agents", "Generic", "Agents", "MosaicOnlyCategory")
	if err := os.MkdirAll(mosaicCategoryDir, 0o755); err != nil {
		t.Fatalf("mkdir mosaic-root-only category: %v", err)
	}

	customCatalogRoot := t.TempDir() // completely empty — no categories

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	// No scripted answer for QPromoteCategory — the stub will return SkippedOne, which is
	// intentional: we only want to observe what options were offered, not drive the outcome.
	baseStub := interactiontest.NewBuilder().Build()
	spy := &categoryOptionsCapture{Stub: baseStub}

	deps := newPromoteDepsWithCatalogRoot(t, baseStub, mosaicRoot, customCatalogRoot)
	deps.Interaction = spy
	svc := app.New(deps)

	// Act — trigger the category prompt; the skipped answer causes ErrPromoteCategoryRequired
	// which we discard — we only care about the captured options.
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "",
		HarnessID: "stub-harness",
	})

	// "MosaicOnlyCategory" must NOT appear in the options offered to the user.
	idSet := optionIDSet(spy.capturedCategoryOptions)
	if idSet["MosaicOnlyCategory"] {
		t.Errorf("QPromoteCategory options %v include %q which exists only under the MOSAIC root; "+
			"category enumeration must read from the active catalogue root only.",
			optionIDList(spy.capturedCategoryOptions), "MosaicOnlyCategory")
	}
}

// ---------------------------------------------------------------------------
// Containment: category path-traversal and path-separator rejection
// ---------------------------------------------------------------------------

// TestPromote_CategoryWithDotDot_IsRejected verifies that Promote rejects a category
// value that contains ".." traversal segments, returning an error before any directory
// is created or file written. PromoteRequest.Category is caller-supplied and reaches
// promoteDestinationPath verbatim (e.g. via a CLI flag); a crafted value such as
// "../../escape" must not silently resolve to an unintended location regardless of
// whether the resolved absolute path lands inside or outside the catalogue root.
// The Contracts spec states that category values containing ".." are rejected before
// any directory is created.
// FAILS until promoteDestinationPath (or its caller in the service) rejects category
// values containing "..".
func TestPromote_CategoryWithDotDot_IsRejected(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, customCatalogRoot)
	svc := app.New(deps)

	// Act — "../../escape" contains ".." segments. filepath.Join would silently normalise
	// these against the constructed sub-path; the containment guard must reject the value
	// before any path resolution or I/O takes place.
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "../../escape",
		HarnessID: "stub-harness",
	})

	// Assert — must return an error; any category containing ".." is invalid.
	if err == nil {
		t.Errorf(`Promote with category "../../escape" returned no error; ` +
			`category values containing ".." must be rejected before any directory is created`)
	}
}

// TestPromote_CategoryWithPathSeparator_IsRejected verifies that Promote rejects a
// category value that contains a path separator, returning an error before any directory
// is created or file written. PromoteRequest.Category is caller-supplied; a value such
// as "foo/bar" must not cause promote to silently write into a nested sub-directory tree
// (Agents/Generic/Agents/foo/bar/) rather than treating the entire string as the category
// name, because the category name is expected to be a single directory component.
// FAILS until promoteDestinationPath (or its caller in the service) rejects category
// values containing path separators.
func TestPromote_CategoryWithPathSeparator_IsRejected(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, customCatalogRoot)
	svc := app.New(deps)

	// Act — "foo/bar" contains a forward-slash path separator. Without a guard, filepath.Join
	// treats this as two path components and creates a nested Agents/.../foo/bar/ tree.
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "foo/bar",
		HarnessID: "stub-harness",
	})

	// Assert — must return an error; category values containing path separators are invalid.
	if err == nil {
		t.Errorf(`Promote with category "foo/bar" returned no error; ` +
			`category values containing path separators must be rejected before any directory is created`)
	}

	// After a correctly rejected promote, no nested directory tree must exist inside the
	// catalogue root at the path that an unguarded filepath.Join would have produced.
	unexpectedDir := filepath.Join(customCatalogRoot, "Agents", "Generic", "Agents", "foo", "bar")
	if _, statErr := os.Stat(unexpectedDir); !os.IsNotExist(statErr) {
		t.Errorf("directory %q was created under the catalogue root after Promote with a "+
			"path-separator category; the containment guard must prevent directory creation "+
			"before returning the error",
			unexpectedDir)
	}
}

// ---------------------------------------------------------------------------
// T6.5: Write-then-load round-trip
// ---------------------------------------------------------------------------

// TestPromote_PromotedAgent_IsFoundByCatalogLoadOfCustomFolder verifies that an agent
// promoted into a custom catalogue folder is discoverable by catalog.Load when called with
// that same folder as the catalogRoot parameter. This closes the loop between promote's
// write side and the catalogue's read side and ensures AC6.6.
// FAILS until promote writes to CatalogRoot() so the written file is within the tree that
// catalog.Load(mosaicRoot, customFolder) scans.
func TestPromote_PromotedAgent_IsFoundByCatalogLoadOfCustomFolder(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	customCatalogRoot := t.TempDir()

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "round-trip-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps := newPromoteDepsWithCatalogRoot(t, stub, mosaicRoot, customCatalogRoot)
	svc := app.New(deps)

	// Act — promote into the custom catalogue root.
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Load the catalogue from the same custom folder.
	cat, loadErr := catalog.Load(mosaicRoot, customCatalogRoot)
	if loadErr != nil {
		t.Fatalf("catalog.Load(mosaicRoot, customCatalogRoot): %v", loadErr)
	}

	// Assert — the promoted agent must be discoverable by the catalogue load.
	agent, found := cat.Agent(result.Key)
	if !found {
		t.Errorf("catalog.Agent(%q) returned false after promoting into the custom catalogue folder;\n"+
			"an agent promoted to <catalogRoot>/Agents/Generic/Agents/{category}/ must be found by\n"+
			"catalog.Load(mosaicRoot, catalogRoot).",
			result.Key)
	} else if agent.Key != result.Key {
		t.Errorf("agent.Key = %q, want %q", agent.Key, result.Key)
	}
}
