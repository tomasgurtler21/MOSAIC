package app_test

// promote_service_test.go covers the service-level Promote flow: eligible and ineligible
// source files, the category prompt, catalog registration after the write, the destination-
// collision policy, source-path validation, and related edge cases.
//
// Verified behaviours:
//
// Service-level Promote flow (T9.1):
//   - An eligible harness-only file with a supplied category produces a PromoteResult with
//     the correct SourcePath, DestinationPath (under Subagents/{category}/),
//     Key, NumericID, and Category values.
//   - After a successful promote, the source file is byte-identical to what it was before.
//   - After a successful promote, the destination file exists on disk.
//   - A promote with DryRun=true validates everything but writes no file.
//
// Ineligible source rejection (T9.2, service level):
//   - A source file that does not carry transform_version in frontmatter is rejected with
//     an error wrapping ErrPromoteNotTransformed, and no destination file is written.
//   - A source file with transform_version but no canonical boundary tags is rejected
//     with an error wrapping ErrPromoteNotTransformed, and no destination file is written.
//   - An empty FilePath is rejected with an error wrapping ErrPromoteFileRequired.
//
// Category prompt (T9.3):
//   - When req.Category is empty, QPromoteCategory is asked through the Interaction port.
//   - When req.Category is non-empty, QPromoteCategory is not asked.
//   - A non-empty Category equal to PromoteCategoryUtility places the file under
//     UtilityAgents/ rather than Subagents/{category}/.
//   - A non-empty Category naming a subdirectory places the file under
//     Subagents/{category}/.
//   - A cancelled or skipped QPromoteCategory response returns ErrPromoteCategoryRequired
//     and writes no file.
//
// Catalog registration (T9.4):
//   - After a successful promote, loading the catalog from the MOSAIC root discovers
//     the newly written agent.
//   - The discovered agent's key matches the key the promote computed from the source filename.
//   - No duplicate-agent-id issue is reported by the catalog after the write.
//
// Destination-collision policy (T9.7):
//   - When a file already exists at the destination and req.Overwrite is false,
//     Promote returns ErrPromoteDestinationExists and writes nothing.
//   - When a file already exists at the destination and req.Overwrite is true,
//     Promote replaces it and returns a successful PromoteResult.
//   - A refused collision (Overwrite=false) leaves the destination file byte-identical
//     to what it was before the call.
//
// Source-path validation — TDD RED phase (T2.1, T2.2):
//   - A FilePath that points to a directory is rejected with an error wrapping
//     ErrPromoteSourceNotFile whose message contains the supplied path; result is zero.
//   - A FilePath that does not exist is rejected with an error wrapping
//     ErrPromoteSourceNotFound whose message contains the supplied path; result is zero.
//   - Neither rejection writes any file at the destination.
//   - Neither rejection invokes QPromoteCategory through the Interaction port.
//   - ErrPromoteSourceNotFound and ErrPromoteSourceNotFile are distinct sentinels that do
//     not match each other through errors.Is.
//   - Both sentinel errors are matched by errors.Is on the wrapped return value.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Promote test fixtures
// ---------------------------------------------------------------------------

// eligibleHarnessOnlyAgentBytes returns the bytes of a minimal harness-only agent that
// satisfies both eligibility signals: frontmatter with transform_version and a body with
// one properly paired canonical <Identity type="core"> tag. The file also carries the catalog
// fields (id, version, name, description, role, recommended_tier, tier_rationale, tools,
// required_skills) so the generated generic file is catalog-discoverable after promotion.
func eligibleHarnessOnlyAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"id: \"77\"\n" +
		"version: \"1.5.0\"\n" +
		"name: my-harness-agent\n" +
		"description: A harness-only agent for promote tests\n" +
		"role: subagent\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: moderate capability needed for this task\n" +
		"tools: [file_read, bash]\n" +
		"required_skills: [lean-tdd]\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are My Harness Agent, responsible for performing promote-test tasks.\n" +
		"<IdentityExtension type=\"project\">\n" +
		"Some harness-specific injection content.\n" +
		"</IdentityExtension>\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\" version=\"1.9\">\n" +
		"Protocol content here.\n" +
		"</CommunicationProtocol>\n" +
		"<Capabilities type=\"core\">\n" +
		"Some capability prose.\n" +
		"</Capabilities>\n")
}

// ineligibleSourceWithNoTransformVersion returns source bytes that do not carry
// transform_version — signal one of the eligibility rule is missing.
func ineligibleSourceWithNoTransformVersion() []byte {
	return []byte("---\n" +
		"id: \"1\"\n" +
		"version: \"1.0.0\"\n" +
		"name: not-harness-only\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Plain prose — no transform_version means ineligible.\n" +
		"</Identity>\n")
}

// ineligibleSourceWithNoBoundaryTags returns source bytes that carry transform_version
// but no canonical boundary tags — signal two of the eligibility rule is missing.
func ineligibleSourceWithNoBoundaryTags() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"Just plain text. No section region tags of any kind.\n")
}

// writeMosaicRoot creates a minimal MOSAIC root in t.TempDir() that catalog.Load can parse.
// It creates the sentinel files ResolveRoot requires and the category subdirectory.
func writeMosaicRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Files that satisfy catalog.ResolveRoot.
	if err := os.MkdirAll(filepath.Join(root, "Catalog"), 0o755); err != nil {
		t.Fatalf("writeMosaicRoot: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "Catalog", "SourceFilesFormat.md"),
		[]byte("# Source Files Format\n"),
		0o644,
	); err != nil {
		t.Fatalf("writeMosaicRoot: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Catalog", "Workflows"), 0o755); err != nil {
		t.Fatalf("writeMosaicRoot: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "Catalog", "Workflows", "Index.md"),
		[]byte("# Workflow Index\n"),
		0o644,
	); err != nil {
		t.Fatalf("writeMosaicRoot: %v", err)
	}

	return root
}

// writeEligibleSourceFile writes an eligible harness-only agent to path and returns the path.
func writeEligibleSourceFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeEligibleSourceFile mkdir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, eligibleHarnessOnlyAgentBytes(), 0o644); err != nil {
		t.Fatalf("writeEligibleSourceFile: %v", err)
	}
	return path
}

// newPromoteDeps builds an app.Deps suitable for promote tests. It uses the minimal catalog
// stub and a real MosaicRoot so file writes land in the temp directory. The interaction
// stub is wired from the caller.
//
// The stub catalog's CatalogRoot() is set to filepath.Join(mosaicRoot, "Catalog") so that
// promote's write destination is the default catalogue root — byte-identical to the
// pre-feature behaviour — and catalog.Load(mosaicRoot, "") discovers the written file.
func newPromoteDeps(t *testing.T, stub *interactiontest.Stub, mosaicRoot string) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	deps.MosaicRoot = mosaicRoot
	deps.Catalog = &stubCatalog{
		root:        mosaicRoot,
		catalogRoot: filepath.Join(mosaicRoot, "Catalog"),
	}
	return deps, workspace
}

// ---------------------------------------------------------------------------
// T9.1: Service-level Promote flow — eligible source
// ---------------------------------------------------------------------------

// TestPromote_EligibleFile_WithCategory_ReturnsPopulatedResult verifies that a Promote call
// with an eligible source file and a pre-supplied category returns a PromoteResult with
// correct SourcePath, DestinationPath, Key, Category, and a non-empty NumericID.
func TestPromote_EligibleFile_WithCategory_ReturnsPopulatedResult(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	agentDir := filepath.Join(mosaicRoot, "Catalog", "Subagents", "TestCategory")
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}
	if result.SourcePath != srcPath {
		t.Errorf("SourcePath = %q, want %q", result.SourcePath, srcPath)
	}

	wantKey := "my-harness-agent"
	if result.Key != wantKey {
		t.Errorf("Key = %q, want %q", result.Key, wantKey)
	}
	if result.Category != "TestCategory" {
		t.Errorf("Category = %q, want %q", result.Category, "TestCategory")
	}
	if result.NumericID == "" {
		t.Error("NumericID is empty; must be a non-empty numeric string")
	}

	wantDest := filepath.Join(agentDir, "my-harness-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q", result.DestinationPath, wantDest)
	}
}

// TestPromote_EligibleFile_SourceFileByteIdenticalAfterPromote verifies that the source
// file is untouched after a successful promote: its bytes remain identical to what they
// were before the call. Promote must never modify or delete the source file.
func TestPromote_EligibleFile_SourceFileByteIdenticalAfterPromote(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcDir := t.TempDir()
	srcPath := writeEligibleSourceFile(t, srcDir, "my-harness-agent.md")
	srcBefore, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source before: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert — source unchanged regardless of whether Promote succeeded or failed.
	srcAfter, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source after: %v", err)
	}
	if !bytes.Equal(srcBefore, srcAfter) {
		t.Error("source file bytes changed after Promote; Promote must not modify the source file")
	}
}

// TestPromote_EligibleFile_GenericFileExistsAtDestination verifies that after a successful
// non-dry-run Promote, the generic file exists at the computed destination path.
func TestPromote_EligibleFile_GenericFileExistsAtDestination(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — file must exist on disk.
	if _, statErr := os.Stat(result.DestinationPath); os.IsNotExist(statErr) {
		t.Errorf("destination file %q does not exist after Promote; Promote must write the file", result.DestinationPath)
	}
}

// TestPromote_EligibleFile_DryRun_WritesNoFile verifies that when DryRun is true, Promote
// validates and computes everything but writes no file at the destination path.
func TestPromote_EligibleFile_DryRun_WritesNoFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		DryRun:    true,
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote (dry run): %v", err)
	}
	if !result.DryRun {
		t.Error("PromoteResult.DryRun is false; must be true when DryRun was requested")
	}

	// Assert — no file was written.
	if _, statErr := os.Stat(result.DestinationPath); statErr == nil {
		t.Errorf("destination file %q exists after dry-run Promote; no file must be written", result.DestinationPath)
	}
}

// ---------------------------------------------------------------------------
// T9.2: Ineligible source rejection (service level)
// ---------------------------------------------------------------------------

// TestPromote_IneligibleSource_NoTransformVersion_WrapsErrPromoteNotTransformed verifies
// that a source file without transform_version is rejected at the service level with an
// error wrapping ErrPromoteNotTransformed.
func TestPromote_IneligibleSource_NoTransformVersion_WrapsErrPromoteNotTransformed(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "not-eligible.md")
	if err := os.WriteFile(srcPath, ineligibleSourceWithNoTransformVersion(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error for ineligible source (no transform_version), got nil")
	}
	if !errors.Is(err, app.ErrPromoteNotTransformed) {
		t.Errorf("error = %v; want wrapping ErrPromoteNotTransformed", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on rejection: %+v; on failure the result must be the zero value", result)
	}
}

// TestPromote_IneligibleSource_NoBoundaryTags_WrapsErrPromoteNotTransformed verifies
// that a source with transform_version but no canonical boundary tags is rejected at
// the service level with an error wrapping ErrPromoteNotTransformed.
func TestPromote_IneligibleSource_NoBoundaryTags_WrapsErrPromoteNotTransformed(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "no-tags.md")
	if err := os.WriteFile(srcPath, ineligibleSourceWithNoBoundaryTags(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error for ineligible source (no boundary tags), got nil")
	}
	if !errors.Is(err, app.ErrPromoteNotTransformed) {
		t.Errorf("error = %v; want wrapping ErrPromoteNotTransformed", err)
	}
}

// TestPromote_IneligibleSource_WritesNoFile verifies that when an ineligible source is
// rejected, no file is written at the destination path.
func TestPromote_IneligibleSource_WritesNoFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	destPath := filepath.Join(mosaicRoot, "Catalog", "Subagents", "TestCategory", "not-eligible.md")
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "not-eligible.md")
	if err := os.WriteFile(srcPath, ineligibleSourceWithNoTransformVersion(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert
	if _, statErr := os.Stat(destPath); statErr == nil {
		t.Error("destination file exists after rejection; ineligible source must write nothing")
	}
}

// TestPromote_EmptyFilePath_WrapsErrPromoteFileRequired verifies that an empty FilePath
// field is rejected with an error wrapping ErrPromoteFileRequired.
func TestPromote_EmptyFilePath_WrapsErrPromoteFileRequired(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: "", // deliberately empty
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error for empty FilePath, got nil")
	}
	if !errors.Is(err, app.ErrPromoteFileRequired) {
		t.Errorf("error = %v; want wrapping ErrPromoteFileRequired", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on rejection: %+v", result)
	}
}

// ---------------------------------------------------------------------------
// T9.3: Category prompt
// ---------------------------------------------------------------------------

// TestPromote_CategoryAbsent_AsksQPromoteCategory verifies that when req.Category is empty,
// the Interaction port is consulted with QPromoteCategory, and the user's answer is used
// as the category. The interaction stub is pre-configured to answer with "TestCategory".
func TestPromote_CategoryAbsent_AsksQPromoteCategory(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	// Create the TestCategory directory so it appears as an option.
	if err := os.MkdirAll(filepath.Join(mosaicRoot, "Catalog", "Subagents", "TestCategory"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	// The category question uses the source file path as the Question.Subject.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, srcPath, "TestCategory").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "", // empty — prompt must fire
		HarnessID: "stub-harness",
	})

	// Assert
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if result.Category != "TestCategory" {
		t.Errorf("Category = %q, want %q; the answer from QPromoteCategory must be used", result.Category, "TestCategory")
	}

	// Verify QPromoteCategory was indeed asked.
	order := stub.QuestionOrder()
	found := false
	for _, id := range order {
		if id == domain.QPromoteCategory {
			found = true
			break
		}
	}
	if !found {
		t.Error("QPromoteCategory was not asked; when req.Category is empty the prompt must fire")
	}
}

// TestPromote_CategorySupplied_DoesNotAskQPromoteCategory verifies that when req.Category
// is non-empty, QPromoteCategory is never asked through the Interaction port.
func TestPromote_CategorySupplied_DoesNotAskQPromoteCategory(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build() // no QPromoteCategory answer configured
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory", // supplied — prompt must NOT fire
		HarnessID: "stub-harness",
	})

	// Assert
	for _, id := range stub.QuestionOrder() {
		if id == domain.QPromoteCategory {
			t.Error("QPromoteCategory was asked even though req.Category was non-empty; the pre-answer convention must suppress the prompt")
			break
		}
	}
}

// TestPromote_CategoryUtility_PlacesFileUnderUtilityAgentsDir verifies that a Category
// equal to PromoteCategoryUtility places the generic file under UtilityAgents/
// rather than Subagents/{category}/.
func TestPromote_CategoryUtility_PlacesFileUnderUtilityAgentsDir(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  app.PromoteCategoryUtility,
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert
	wantDest := filepath.Join(mosaicRoot, "Catalog", "UtilityAgents", "my-harness-agent.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q (PromoteCategoryUtility must place under UtilityAgents/)",
			result.DestinationPath, wantDest)
	}
}

// TestPromote_CategoryNamed_PlacesFileUnderSubagentsCategoryDir verifies that a non-utility
// category places the file under Subagents/{category}/.
func TestPromote_CategoryNamed_PlacesFileUnderSubagentsCategoryDir(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	category := "Audit"
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "audit-specialist.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  category,
		HarnessID: "stub-harness",
	})

	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert
	wantDest := filepath.Join(mosaicRoot, "Catalog", "Subagents", category, "audit-specialist.md")
	if result.DestinationPath != wantDest {
		t.Errorf("DestinationPath = %q, want %q (named category must place under Subagents/{category}/)",
			result.DestinationPath, wantDest)
	}
}

// TestPromote_CategoryPromptCancelled_WrapsErrPromoteCategoryRequired verifies that when
// the QPromoteCategory prompt is cancelled or skipped (no answer provided), Promote returns
// an error wrapping ErrPromoteCategoryRequired and writes no file.
func TestPromote_CategoryPromptCancelled_WrapsErrPromoteCategoryRequired(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	// No QPromoteCategory answer configured — the stub will return SkippedOne.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "", // empty — prompt fires, then returns SkippedOne
		HarnessID: "stub-harness",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when category prompt is skipped, got nil")
	}
	if !errors.Is(err, app.ErrPromoteCategoryRequired) {
		t.Errorf("error = %v; want wrapping ErrPromoteCategoryRequired", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero when category prompt is skipped: %+v", result)
	}
}

// ---------------------------------------------------------------------------
// T9.4: Catalog registration
// ---------------------------------------------------------------------------

// TestPromote_WrittenFile_IsDiscoveredByCatalog verifies that after a successful promote,
// loading the catalog from the MOSAIC root discovers the newly written agent. Registration
// is automatic: writing a well-formed file into the chosen directory IS the registration.
func TestPromote_WrittenFile_IsDiscoveredByCatalog(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Load the catalog from the MOSAIC root to verify registration.
	cat, loadErr := catalog.Load(mosaicRoot, "")
	if loadErr != nil {
		t.Fatalf("catalog.Load after promote: %v", loadErr)
	}

	// Assert — the agent is present, correctly keyed.
	agent, found := cat.Agent(result.Key)
	if !found {
		t.Errorf("catalog.Agent(%q) returned false after promote; the written file must be discoverable by the catalog", result.Key)
	} else if agent.Key != result.Key {
		t.Errorf("agent.Key = %q, want %q", agent.Key, result.Key)
	}
}

// TestPromote_WrittenFile_HasCorrectKey verifies that the agent discovered by the catalog
// has the key the promote computed from the source filename.
func TestPromote_WrittenFile_HasCorrectKey(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	cat, loadErr := catalog.Load(mosaicRoot, "")
	if loadErr != nil {
		t.Fatalf("catalog.Load: %v", loadErr)
	}

	// Assert — key derived from filename without extension.
	wantKey := "my-harness-agent"
	if result.Key != wantKey {
		t.Errorf("PromoteResult.Key = %q, want %q", result.Key, wantKey)
	}
	if _, found := cat.Agent(wantKey); !found {
		t.Errorf("catalog.Agent(%q) not found after promote; expected catalog to discover the written file", wantKey)
	}
}

// TestPromote_AssignedNumericID_NoCollisionInCatalog verifies that after a successful promote,
// no "duplicate-agent-id" issue is reported by the catalog — the assigned id does not
// collide with any existing agent.
func TestPromote_AssignedNumericID_NoCollisionInCatalog(t *testing.T) {
	// Arrange: pre-populate the catalog stub with agents that have ids "1" and "2".
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	// Pre-populate the stub catalog with agents that have numeric ids "1" and "2" so that
	// nextNumericID must assign "3" or higher.
	deps.Catalog = &stubCatalog{
		root: mosaicRoot,
		agents: []domain.Agent{
			{Key: "agent-one", NumericID: "1"},
			{Key: "agent-two", NumericID: "2"},
		},
	}
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert: the assigned id must not be "1" or "2".
	existingIDs := map[string]bool{"1": true, "2": true}
	if existingIDs[result.NumericID] {
		t.Errorf("NumericID = %q collides with an existing agent id; nextNumericID must assign a unique id", result.NumericID)
	}

	// Load the catalog and check for duplicate-agent-id issues.
	cat, loadErr := catalog.Load(mosaicRoot, "")
	if loadErr != nil {
		t.Fatalf("catalog.Load: %v", loadErr)
	}
	for _, issue := range cat.Issues() {
		if issue.Code == "duplicate-agent-id" {
			t.Errorf("catalog.Issues contains duplicate-agent-id for %q after promote; assigned id must be unique", issue.Subject)
		}
	}
}

// ---------------------------------------------------------------------------
// T9.7: Destination-collision policy
// ---------------------------------------------------------------------------

// TestPromote_ExistingDestination_WithoutOverwrite_WrapsErrPromoteDestinationExists verifies
// that when a file already exists at the destination and req.Overwrite is false, Promote
// returns an error wrapping ErrPromoteDestinationExists and writes nothing.
func TestPromote_ExistingDestination_WithoutOverwrite_WrapsErrPromoteDestinationExists(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	category := "TestCategory"
	destDir := filepath.Join(mosaicRoot, "Catalog", "Subagents", category)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-create the destination file.
	existingContent := []byte("existing generic agent content")
	destPath := filepath.Join(destDir, "my-harness-agent.md")
	if err := os.WriteFile(destPath, existingContent, 0o644); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — without Overwrite flag.
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  category,
		Overwrite: false,
		HarnessID: "stub-harness",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error for collision without Overwrite, got nil")
	}
	if !errors.Is(err, app.ErrPromoteDestinationExists) {
		t.Errorf("error = %v; want wrapping ErrPromoteDestinationExists", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on collision: %+v", result)
	}
}

// TestPromote_ExistingDestination_RefusedCollision_LeavesDestinationUnchanged verifies that
// a refused collision (Overwrite=false) leaves the existing destination file byte-identical
// to what it was before the call.
func TestPromote_ExistingDestination_RefusedCollision_LeavesDestinationUnchanged(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	category := "TestCategory"
	destDir := filepath.Join(mosaicRoot, "Catalog", "Subagents", category)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existingContent := []byte("original generic agent content — must survive the refused collision")
	destPath := filepath.Join(destDir, "my-harness-agent.md")
	if err := os.WriteFile(destPath, existingContent, 0o644); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  category,
		Overwrite: false,
		HarnessID: "stub-harness",
	})

	// Assert — destination file unchanged.
	after, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read destination after refused collision: %v", err)
	}
	if !bytes.Equal(existingContent, after) {
		t.Error("destination file was modified by a refused collision; Promote must not touch the existing file without Overwrite consent")
	}
}

// TestPromote_ExistingDestination_WithOverwrite_ReplacesFile verifies that when a file
// already exists at the destination and req.Overwrite is true, Promote replaces it and
// returns a successful PromoteResult.
func TestPromote_ExistingDestination_WithOverwrite_ReplacesFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	category := "TestCategory"
	destDir := filepath.Join(mosaicRoot, "Catalog", "Subagents", category)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldContent := []byte("old generic agent content to be replaced")
	destPath := filepath.Join(destDir, "my-harness-agent.md")
	if err := os.WriteFile(destPath, oldContent, 0o644); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}

	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — with Overwrite flag.
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  category,
		Overwrite: true,
		HarnessID: "stub-harness",
	})

	// Assert
	if err != nil {
		t.Fatalf("Promote with Overwrite: %v", err)
	}
	if result.DestinationPath == "" {
		t.Error("DestinationPath is empty; Promote with Overwrite must return a populated result")
	}

	// The destination file must exist and differ from the old content.
	newContent, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("read destination after overwrite: %v", readErr)
	}
	if bytes.Equal(oldContent, newContent) {
		t.Error("destination file is identical to old content after Overwrite; the file must be replaced by the promoted generic agent")
	}
}

// ---------------------------------------------------------------------------
// T2.1 & T2.2: Source path validation (TDD RED — requires ErrPromoteSourceNotFound and
// ErrPromoteSourceNotFile to be declared in service.go)
// ---------------------------------------------------------------------------

// TestPromote_DirectoryPath_WrapsErrPromoteSourceNotFile verifies that when FilePath points
// to an existing directory, Promote returns an error wrapping ErrPromoteSourceNotFile.
// Directories are never valid promote input; a single harness-only agent file is required.
func TestPromote_DirectoryPath_WrapsErrPromoteSourceNotFile(t *testing.T) {
	// Arrange — use a real directory as the source path.
	mosaicRoot := writeMosaicRoot(t)
	dirPath := t.TempDir() // an existing directory

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: dirPath,
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when FilePath is a directory, got nil")
	}
	if !errors.Is(err, app.ErrPromoteSourceNotFile) {
		t.Errorf("error = %v; want wrapping ErrPromoteSourceNotFile", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on directory rejection: %+v; on failure the result must be the zero value", result)
	}
}

// TestPromote_DirectoryPath_ErrorMessageContainsPath verifies that the error returned when
// FilePath is a directory contains the supplied path in its message, so the user can identify
// which path was rejected without parsing a localized OS message. It also checks that the
// message carries domain wording stating a file is required (AC2.2) and that the old
// raw-OS-text wrapping ("reading source file ...") is not the shape of the message (AC2.6).
func TestPromote_DirectoryPath_ErrorMessageContainsPath(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	dirPath := t.TempDir()

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: dirPath,
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when FilePath is a directory, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, dirPath) {
		t.Errorf("error message %q does not contain the rejected path %q; the message must name the offending path", msg, dirPath)
	}
	// AC2.2: the message must state that a single agent file is required, not just name the path.
	if !strings.Contains(msg, "required") {
		t.Errorf("error message %q does not contain the domain keyword %q; the message must state a single agent file is required", msg, "required")
	}
	// AC2.6: the message must not be the old raw-OS-text wrapping shape.
	if strings.HasPrefix(msg, "reading source file") {
		t.Errorf("error message %q starts with %q; the directory-rejection message must not use the old read-failure wrapping", msg, "reading source file")
	}
}

// TestPromote_DirectoryPath_WritesNothing verifies that when FilePath is a directory,
// Promote writes no file at the destination path.
func TestPromote_DirectoryPath_WritesNothing(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	dirPath := t.TempDir()
	category := "TestCategory"
	// The destination path that would be written if validation were skipped.
	destPath := filepath.Join(mosaicRoot, "Catalog", "Subagents", category, filepath.Base(dirPath)+".md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: dirPath,
		Category: category,
	})

	// Assert — no file must exist at the computed destination.
	if _, statErr := os.Stat(destPath); statErr == nil {
		t.Errorf("file exists at %q after directory-path rejection; Promote must write nothing on validation failure", destPath)
	}
}

// TestPromote_DirectoryPath_DoesNotAskCategoryQuestion verifies that when FilePath is a
// directory, Promote returns without asking QPromoteCategory through the Interaction port.
// Source-path validation runs before the category prompt; a directory is rejected first.
func TestPromote_DirectoryPath_DoesNotAskCategoryQuestion(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	dirPath := t.TempDir()

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — Category is intentionally empty so QPromoteCategory would fire if reached.
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: dirPath,
		Category: "",
	})

	// Assert
	for _, id := range stub.QuestionOrder() {
		if id == domain.QPromoteCategory {
			t.Error("QPromoteCategory was asked when FilePath is a directory; source-path validation must run before the category prompt")
			break
		}
	}
}

// TestPromote_NonExistentPath_WrapsErrPromoteSourceNotFound verifies that when FilePath
// names a path that does not exist on the filesystem, Promote returns an error wrapping
// ErrPromoteSourceNotFound.
func TestPromote_NonExistentPath_WrapsErrPromoteSourceNotFound(t *testing.T) {
	// Arrange — construct a path that is guaranteed not to exist.
	mosaicRoot := writeMosaicRoot(t)
	missingPath := filepath.Join(t.TempDir(), "no-such-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: missingPath,
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when FilePath does not exist, got nil")
	}
	if !errors.Is(err, app.ErrPromoteSourceNotFound) {
		t.Errorf("error = %v; want wrapping ErrPromoteSourceNotFound", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on not-found rejection: %+v; on failure the result must be the zero value", result)
	}
}

// TestPromote_NonExistentPath_ErrorMessageContainsPath verifies that the error returned when
// FilePath does not exist contains the supplied path in its message. It also checks that the
// message carries domain wording indicating the path is missing (AC2.3) and that the old
// raw-OS-text wrapping ("reading source file ...") is not the shape of the message (AC2.6).
func TestPromote_NonExistentPath_ErrorMessageContainsPath(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	missingPath := filepath.Join(t.TempDir(), "no-such-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: missingPath,
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when FilePath does not exist, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, missingPath) {
		t.Errorf("error message %q does not contain the rejected path %q; the message must name the offending path", msg, missingPath)
	}
	// AC2.3: the message must state that the source path does not exist, not just name it.
	if !strings.Contains(msg, "not exist") {
		t.Errorf("error message %q does not contain the domain keyword %q; the message must indicate the path is missing", msg, "not exist")
	}
	// AC2.6: the message must not be the old raw-OS-text wrapping shape.
	if strings.HasPrefix(msg, "reading source file") {
		t.Errorf("error message %q starts with %q; the not-found-rejection message must not use the old read-failure wrapping", msg, "reading source file")
	}
}

// TestPromote_NonExistentPath_WritesNothing verifies that when FilePath does not exist,
// Promote writes no file at the destination path.
func TestPromote_NonExistentPath_WritesNothing(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	missingPath := filepath.Join(t.TempDir(), "no-such-agent.md")
	category := "TestCategory"
	destPath := filepath.Join(mosaicRoot, "Catalog", "Subagents", category, "no-such-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: missingPath,
		Category: category,
	})

	// Assert
	if _, statErr := os.Stat(destPath); statErr == nil {
		t.Errorf("file exists at %q after not-found rejection; Promote must write nothing on validation failure", destPath)
	}
}

// TestPromote_NonExistentPath_DoesNotAskCategoryQuestion verifies that when FilePath does
// not exist, Promote returns without asking QPromoteCategory. Source-path validation must
// run before the category prompt, so a missing path is rejected first.
func TestPromote_NonExistentPath_DoesNotAskCategoryQuestion(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	missingPath := filepath.Join(t.TempDir(), "no-such-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — Category is intentionally empty so QPromoteCategory would fire if reached.
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: missingPath,
		Category: "",
	})

	// Assert
	for _, id := range stub.QuestionOrder() {
		if id == domain.QPromoteCategory {
			t.Error("QPromoteCategory was asked when FilePath does not exist; source-path validation must run before the category prompt")
			break
		}
	}
}

// TestPromote_SourcePathSentinels_AreDistinct verifies that ErrPromoteSourceNotFound and
// ErrPromoteSourceNotFile are two independent sentinel values: errors.Is(ErrPromoteSourceNotFound,
// ErrPromoteSourceNotFile) is false and vice versa. Callers that branch on source-path failure
// depend on this distinction.
func TestPromote_SourcePathSentinels_AreDistinct(t *testing.T) {
	if errors.Is(app.ErrPromoteSourceNotFound, app.ErrPromoteSourceNotFile) {
		t.Error("errors.Is(ErrPromoteSourceNotFound, ErrPromoteSourceNotFile) is true; the two sentinels must be distinct")
	}
	if errors.Is(app.ErrPromoteSourceNotFile, app.ErrPromoteSourceNotFound) {
		t.Error("errors.Is(ErrPromoteSourceNotFile, ErrPromoteSourceNotFound) is true; the two sentinels must be distinct")
	}
}

// TestPromote_DirectoryPath_SentinelDoesNotMatchNotFound verifies that when FilePath is a
// directory, the returned error wraps ErrPromoteSourceNotFile and does NOT wrap
// ErrPromoteSourceNotFound, so the caller can distinguish the two cases.
func TestPromote_DirectoryPath_SentinelDoesNotMatchNotFound(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	dirPath := t.TempDir()

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: dirPath,
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when FilePath is a directory, got nil")
	}
	if errors.Is(err, app.ErrPromoteSourceNotFound) {
		t.Errorf("error wraps ErrPromoteSourceNotFound for a directory path; directory paths must wrap ErrPromoteSourceNotFile only")
	}
}

// TestPromote_NonExistentPath_SentinelDoesNotMatchNotFile verifies that when FilePath does
// not exist, the returned error wraps ErrPromoteSourceNotFound and does NOT wrap
// ErrPromoteSourceNotFile, so the caller can distinguish the two cases.
func TestPromote_NonExistentPath_SentinelDoesNotMatchNotFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	missingPath := filepath.Join(t.TempDir(), "no-such-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath: missingPath,
		Category: "TestCategory",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error when FilePath does not exist, got nil")
	}
	if errors.Is(err, app.ErrPromoteSourceNotFile) {
		t.Errorf("error wraps ErrPromoteSourceNotFile for a non-existent path; missing paths must wrap ErrPromoteSourceNotFound only")
	}
}

// ---------------------------------------------------------------------------
// Harness identity threading (T1.1)
// ---------------------------------------------------------------------------

// closeTrackingModule wraps a stubHarnessModule and records whether Close was called. It is
// used to verify the harness module release contract (AC1.7) without relying on side effects
// that are hard to observe from the outside.
type closeTrackingModule struct {
	*stubHarnessModule
	closeCalled bool
}

func (m *closeTrackingModule) Close() error {
	m.closeCalled = true
	return nil
}

// TestPromote_EmptyHarnessID_WrapsErrPromoteHarnessRequired verifies that when HarnessID is
// empty, Promote returns an error wrapping ErrPromoteHarnessRequired and the zero PromoteResult.
// The TUI and CLI both know the harness before calling Promote; no interactive fallback exists.
func TestPromote_EmptyHarnessID_WrapsErrPromoteHarnessRequired(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "", // deliberately empty
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error for empty HarnessID, got nil")
	}
	if !errors.Is(err, app.ErrPromoteHarnessRequired) {
		t.Errorf("error = %v; want wrapping ErrPromoteHarnessRequired", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on rejection: %+v; on failure the result must be the zero value", result)
	}
}

// TestPromote_EmptyHarnessID_WritesNothing verifies that when HarnessID is empty, Promote
// writes no file at the destination path. The all-or-nothing contract means an error
// leaves the filesystem untouched.
func TestPromote_EmptyHarnessID_WritesNothing(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")
	category := "TestCategory"
	expectedDest := filepath.Join(mosaicRoot, "Catalog", "Subagents", category, "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  category,
		HarnessID: "",
	})

	// Assert — no file must have been written.
	if _, statErr := os.Stat(expectedDest); statErr == nil {
		t.Errorf("file exists at %q after empty-HarnessID rejection; Promote must write nothing on harness validation failure", expectedDest)
	}
}

// TestPromote_EmptyHarnessID_DoesNotAskCategoryQuestion verifies that when HarnessID is
// empty, Promote returns without asking QPromoteCategory through the Interaction port.
// Harness validation runs before the category question; the flow aborts before prompting.
func TestPromote_EmptyHarnessID_DoesNotAskCategoryQuestion(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — Category is empty so QPromoteCategory would fire if reached.
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "",
		HarnessID: "",
	})

	// Assert
	for _, id := range stub.QuestionOrder() {
		if id == domain.QPromoteCategory {
			t.Error("QPromoteCategory was asked when HarnessID is empty; harness validation must abort the flow before the category prompt")
			break
		}
	}
}

// TestPromote_UnknownHarnessID_WrapsErrPromoteHarnessUnresolvable verifies that when
// HarnessID names a harness not in the registry, Promote returns an error wrapping
// ErrPromoteHarnessUnresolvable and the zero PromoteResult.
func TestPromote_UnknownHarnessID_WrapsErrPromoteHarnessUnresolvable(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — "no-such-harness" is not registered in the minimal registry.
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "no-such-harness",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote: expected error for unknown HarnessID, got nil")
	}
	if !errors.Is(err, app.ErrPromoteHarnessUnresolvable) {
		t.Errorf("error = %v; want wrapping ErrPromoteHarnessUnresolvable", err)
	}
	if !reflect.DeepEqual(result, app.PromoteResult{}) {
		t.Errorf("result is non-zero on rejection: %+v; on failure the result must be the zero value", result)
	}
}

// TestPromote_UnknownHarnessID_WritesNothing verifies that when HarnessID names no registered
// harness, Promote writes no file. The all-or-nothing contract applies to harness failures.
func TestPromote_UnknownHarnessID_WritesNothing(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")
	category := "TestCategory"
	expectedDest := filepath.Join(mosaicRoot, "Catalog", "Subagents", category, "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, _ = svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  category,
		HarnessID: "no-such-harness",
	})

	// Assert
	if _, statErr := os.Stat(expectedDest); statErr == nil {
		t.Errorf("file exists at %q after unknown-HarnessID rejection; Promote must write nothing on harness resolution failure", expectedDest)
	}
}

// TestPromote_HarnessSentinels_AreDistinct verifies that ErrPromoteHarnessRequired and
// ErrPromoteHarnessUnresolvable are two independent sentinel values. Callers that branch on
// harness failure depend on this distinction to deliver accurate feedback.
func TestPromote_HarnessSentinels_AreDistinct(t *testing.T) {
	if errors.Is(app.ErrPromoteHarnessRequired, app.ErrPromoteHarnessUnresolvable) {
		t.Error("errors.Is(ErrPromoteHarnessRequired, ErrPromoteHarnessUnresolvable) is true; the two sentinels must be distinct")
	}
	if errors.Is(app.ErrPromoteHarnessUnresolvable, app.ErrPromoteHarnessRequired) {
		t.Error("errors.Is(ErrPromoteHarnessUnresolvable, ErrPromoteHarnessRequired) is true; the two sentinels must be distinct")
	}
}

// TestPromote_MissingFilePath_TakesPrecedenceOverEmptyHarnessID verifies that when both
// FilePath and HarnessID are missing, the error names the missing FilePath, not the missing
// harness. Source-path validation must run before harness validation per the documented ordering.
func TestPromote_MissingFilePath_TakesPrecedenceOverEmptyHarnessID(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — both FilePath and HarnessID are empty.
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  "",
		HarnessID: "",
		Category:  "TestCategory",
	})

	// Assert — source-path failure must be reported, not harness failure.
	if err == nil {
		t.Fatal("Promote: expected error, got nil")
	}
	if !errors.Is(err, app.ErrPromoteFileRequired) {
		t.Errorf("error = %v; want ErrPromoteFileRequired; when FilePath is missing, source-path validation must report first, before harness validation", err)
	}
	if errors.Is(err, app.ErrPromoteHarnessRequired) {
		t.Error("error wraps ErrPromoteHarnessRequired when FilePath is missing; source-path validation must take precedence")
	}
}

// TestPromote_NonExistentFilePath_TakesPrecedenceOverUnknownHarnessID verifies that when
// FilePath does not exist and HarnessID is also invalid, the source-path error is returned.
// This pins the ordering documented in the Service.Promote contract: source-path validation
// runs before harness resolution.
func TestPromote_NonExistentFilePath_TakesPrecedenceOverUnknownHarnessID(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	missingPath := filepath.Join(t.TempDir(), "no-such-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — FilePath does not exist AND HarnessID names no registered harness.
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  missingPath,
		HarnessID: "no-such-harness",
		Category:  "TestCategory",
	})

	// Assert — source-path failure must be reported first.
	if err == nil {
		t.Fatal("Promote: expected error, got nil")
	}
	if !errors.Is(err, app.ErrPromoteSourceNotFound) {
		t.Errorf("error = %v; want ErrPromoteSourceNotFound; source-path validation must precede harness validation", err)
	}
	if errors.Is(err, app.ErrPromoteHarnessUnresolvable) {
		t.Error("error wraps ErrPromoteHarnessUnresolvable; harness resolution must not run before source-path validation confirms the file exists")
	}
}

// TestPromote_ValidHarnessID_ModuleIsReleasedAfterSuccessfulRun verifies that when a valid
// HarnessID is supplied and Promote succeeds, the resolved harness module's Close method is
// called. This mirrors the defer module.Close() pattern in DeployNew (AC1.7).
func TestPromote_ValidHarnessID_ModuleIsReleasedAfterSuccessfulRun(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)

	// Replace the registry module with one that tracks Close calls.
	tracker := &closeTrackingModule{stubHarnessModule: newMinimalModule()}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": tracker},
	}
	svc := app.New(deps)

	// Act — successful promote with a valid harness id.
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert — Close must have been called after the run completed.
	if !tracker.closeCalled {
		t.Error("module.Close was not called after a successful Promote; the resolved harness module must be released when the run finishes")
	}
}

// TestPromote_ValidHarnessID_ModuleIsReleasedAfterErrorRun verifies that when a valid
// HarnessID is resolved but the promote fails (ineligible source), the module's Close
// method is still called. The release must happen on every exit path (AC1.7).
func TestPromote_ValidHarnessID_ModuleIsReleasedAfterErrorRun(t *testing.T) {
	// Arrange — ineligible source to trigger failure after harness resolution.
	mosaicRoot := writeMosaicRoot(t)
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "not-eligible.md")
	if err := os.WriteFile(srcPath, ineligibleSourceWithNoTransformVersion(), 0o644); err != nil {
		t.Fatalf("write ineligible source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)

	tracker := &closeTrackingModule{stubHarnessModule: newMinimalModule()}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": tracker},
	}
	svc := app.New(deps)

	// Act — promote fails at the eligibility check, but harness was resolved.
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err == nil {
		t.Fatal("Promote: expected error for ineligible source, got nil")
	}

	// Assert — Close must have been called even though the run failed.
	if !tracker.closeCalled {
		t.Error("module.Close was not called after a failed Promote; the resolved harness module must be released on error paths too")
	}
}

// ---------------------------------------------------------------------------
// AC2.2 / AC2.3 service-level integration: harness drop-set wired through Promote
// ---------------------------------------------------------------------------

// openCodeLikeHarnessModule is a test double for service-level integration tests that need
// a harness whose drop-set covers mode, model, and permission — the three keys an OpenCode-
// shaped harness would write to every deployed agent. It satisfies domain.HarnessModule.
type openCodeLikeHarnessModule struct {
	ref domain.HarnessRef
}

func (m *openCodeLikeHarnessModule) Ref() domain.HarnessRef { return m.ref }
func (m *openCodeLikeHarnessModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID: m.ref.ID,
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
			ToolsKey: "permission",
		},
	}
}
func (m *openCodeLikeHarnessModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *openCodeLikeHarnessModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
		},
	}, nil
}
func (m *openCodeLikeHarnessModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *openCodeLikeHarnessModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}
func (m *openCodeLikeHarnessModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *openCodeLikeHarnessModule) Close() error { return nil }

// eligibleHarnessOnlyAgentWithOpenCodeKeys returns the bytes of a harness-only agent that
// carries the three OpenCode-specific frontmatter keys an OpenCode-shaped harness would
// have stamped: model (the model key), permission (the tools key), and mode (a plan-added
// key). These keys must be absent from the promoted generic file when promoteHarnessDropKeys
// is correctly wired through service.Promote.
func eligibleHarnessOnlyAgentWithOpenCodeKeys() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"mode: subagent\n" +
		"model: claude-opus-4-5\n" +
		"id: \"42\"\n" +
		"version: \"1.5.0\"\n" +
		"name: my-opencode-agent\n" +
		"description: An agent deployed by an OpenCode-like harness\n" +
		"role: subagent\n" +
		"permission:\n" +
		"  list: allow\n" +
		"  bash: allow\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are My OpenCode Agent.\n" +
		"</Identity>\n")
}

// TestPromote_HarnessDropKeysWiredIntoService verifies the end-to-end drop-set wiring
// through service.Promote: a module whose Descriptor declares ModelKey="model" and
// ToolsKey="permission", and whose Frontmatter plan Set adds "mode", must result in a
// promoted file that carries none of those three keys. This is the service-level
// integration assertion for AC2.2 and AC2.3 — the critical gap identified by the
// implementation review.
func TestPromote_HarnessDropKeysWiredIntoService(t *testing.T) {
	// Arrange — source file that carries the three OpenCode-specific keys.
	mosaicRoot := writeMosaicRoot(t)
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "my-opencode-agent.md")
	if err := os.WriteFile(srcPath, eligibleHarnessOnlyAgentWithOpenCodeKeys(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	// Wire the OpenCode-like module under a dedicated harness id so the registry returns it.
	const openCodeHarnessID = "opencode-like"
	openCodeModule := &openCodeLikeHarnessModule{
		ref: domain.HarnessRef{
			ID:          openCodeHarnessID,
			DisplayName: "OpenCode-Like",
			Tier:        domain.TierBuiltin,
			Usable:      true,
		},
	}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{openCodeModule.ref},
		modules: map[string]domain.HarnessModule{openCodeHarnessID: openCodeModule},
	}
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: openCodeHarnessID,
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Read and parse the promoted file to inspect its frontmatter keys.
	promoted, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	promotedDoc, parseErr := docformat.Parse(promoted)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	fm := promotedDoc.Frontmatter()

	// Assert — all three OpenCode-specific keys must be absent from the promoted file.
	for _, key := range []string{"mode", "model", "permission"} {
		if _, present := fm.Get(key); present {
			t.Errorf("promoted file carries harness-specific frontmatter key %q; "+
				"promoteHarnessDropKeys must derive and apply the drop-set through service.Promote", key)
		}
	}
}

// TestPromote_NonDryRunDiffersFromDryRun verifies that a non-dry-run promote writes a file
// but a dry-run promote does not, and that both return DryRun=false and DryRun=true
// respectively in the PromoteResult. This guards against the dry-run flag being silently
// ignored by the implementation.
func TestPromote_NonDryRunDiffersFromDryRun(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcDir := t.TempDir()
	srcPath := writeEligibleSourceFile(t, srcDir, "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{FilePath: srcPath, Category: "TestCategory", HarnessID: "stub-harness"}

	// Act — dry run first.
	dryResult, dryErr := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		DryRun:    true,
		HarnessID: "stub-harness",
	})

	// Assert — dry run leaves no file. This check must happen before the real run so that
	// a file written by the real run cannot mask a dry-run that silently wrote the file.
	if dryErr != nil {
		t.Fatalf("Promote (dry): %v", dryErr)
	}
	if !dryResult.DryRun {
		t.Error("PromoteResult.DryRun is false for dry-run call; must be true")
	}
	if dryResult.DestinationPath == "" {
		t.Fatal("PromoteResult.DestinationPath is empty for dry-run call; must be populated even when no file is written")
	}
	if _, statErr := os.Stat(dryResult.DestinationPath); statErr == nil {
		t.Errorf("file exists after dry-run at %q; dry-run must not write", dryResult.DestinationPath)
	}

	// Act — real run second (same source, different DryRun flag).
	realResult, realErr := svc.Promote(context.Background(), req)

	// Assert — real run writes a file.
	if realErr != nil {
		t.Fatalf("Promote (real): %v", realErr)
	}
	if realResult.DryRun {
		t.Error("PromoteResult.DryRun is true for non-dry-run call; must be false")
	}
	if _, statErr := os.Stat(realResult.DestinationPath); os.IsNotExist(statErr) {
		t.Errorf("file missing after non-dry-run at %q; non-dry-run must write", realResult.DestinationPath)
	}
}

// ---------------------------------------------------------------------------
// Tool-resolution test infrastructure (T4.1, T4.2)
// ---------------------------------------------------------------------------

// toolResolutionHarnessModule is a test double for interactive and non-interactive
// reverse tool resolution tests. Its descriptor declares a list-shaped "tools" key
// with one explicit mapping (bash → bash). "bash" therefore reverse-maps to the
// generic "bash" automatically; any other harness tool name present in the source is
// unmapped and triggers QPromoteCustomTool in the interactive path.
type toolResolutionHarnessModule struct {
	ref domain.HarnessRef
}

func (m *toolResolutionHarnessModule) Ref() domain.HarnessRef { return m.ref }
func (m *toolResolutionHarnessModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID: m.ref.ID,
		Frontmatter: domain.FrontmatterSpec{
			ToolsKey: "tools",
		},
		Tools: domain.ToolSpec{
			Universe: []domain.HarnessTool{
				{Name: "bash"},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "bash",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"bash"}},
					},
				},
			},
		},
	}
}
func (m *toolResolutionHarnessModule) Tools(_ domain.ToolRequest) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}
func (m *toolResolutionHarnessModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *toolResolutionHarnessModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *toolResolutionHarnessModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}
func (m *toolResolutionHarnessModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *toolResolutionHarnessModule) Close() error { return nil }

// toolResolutionHarness is the HarnessRef paired with toolResolutionHarnessModule.
var toolResolutionHarness = domain.HarnessRef{
	ID:          "tool-resolution-harness",
	DisplayName: "Tool Resolution Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// eligibleAgentWithHarnessTools returns bytes of an eligible harness-only agent whose
// frontmatter carries the supplied names in a list-shaped "tools" key (matching the
// ToolsKey declared by toolResolutionHarnessModule). It also includes all generic-only
// fields (recommended_tier, tier_rationale, required_skills) so that
// resolvePromoteGenericFields asks no questions, keeping tool-resolution tests focused
// on QPromoteCustomTool behaviour only.
func eligibleAgentWithHarnessTools(toolNames ...string) []byte {
	toolsLine := "tools: ["
	for i, name := range toolNames {
		if i > 0 {
			toolsLine += ", "
		}
		toolsLine += name
	}
	toolsLine += "]"
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"id: \"55\"\n" +
		"version: \"1.0.0\"\n" +
		"name: tool-test-agent\n" +
		"description: An agent for tool-resolution tests\n" +
		"role: subagent\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: moderate capability for tool tests\n" +
		"required_skills: [lean-tdd]\n" +
		toolsLine + "\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the ToolTestAgent.\n" +
		"</Identity>\n")
}

// newToolResolutionPromoteDeps returns app.Deps wired with toolResolutionHarnessModule
// and the given interaction stub. The mosaicRoot is the MOSAIC root directory the
// promote output is written into.
func newToolResolutionPromoteDeps(t *testing.T, stub *interactiontest.Stub, mosaicRoot string) app.Deps {
	t.Helper()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	mod := &toolResolutionHarnessModule{ref: toolResolutionHarness}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{toolResolutionHarness},
		modules: map[string]domain.HarnessModule{toolResolutionHarness.ID: mod},
	}
	return deps
}

// ---------------------------------------------------------------------------
// T4.1: Reverse custom-tool question — interactive path
// ---------------------------------------------------------------------------

// TestPromote_UnmappedHarnessTool_AsksExactlyOneQuestion verifies that an unmapped harness
// tool entry triggers exactly one QPromoteCustomTool text question in the interactive path.
// The question subject is the harness-side tool name, so the user knows which harness entry
// they are being asked to name in generic terms.
func TestPromote_UnmappedHarnessTool_AsksExactlyOneQuestion(t *testing.T) {
	// Arrange — source with one unmapped harness tool "my-custom-tool"
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("my-custom-tool"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	// Script a text answer so the promote flow completes and reaches the assertion.
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteCustomTool, "my-custom-tool", "file-reader").
		Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — exactly one QPromoteCustomTool with subject "my-custom-tool"
	count := stub.CountAsked(domain.QPromoteCustomTool, "my-custom-tool")
	if count != 1 {
		t.Errorf("QPromoteCustomTool asked %d time(s) for 'my-custom-tool'; want exactly 1", count)
	}
}

// TestPromote_UnmappedHarnessTool_SuppliedNameLandsInToolsList verifies that when the user
// answers QPromoteCustomTool with a generic name, that name appears in the reconstructed
// generic tools list reported in PromoteResult.Tools.
func TestPromote_UnmappedHarnessTool_SuppliedNameLandsInToolsList(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("my-custom-tool"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteCustomTool, "my-custom-tool", "file-reader").
		Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — user-supplied name "file-reader" must appear in the generic tools list
	found := false
	for _, tool := range result.Tools {
		if tool == "file-reader" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.Tools = %v; want 'file-reader' (user-supplied generic name for 'my-custom-tool')", result.Tools)
	}
}

// TestPromote_AutoMappedHarnessTool_TriggersNoQuestion verifies that a harness tool entry
// that reverse-maps automatically via the descriptor's mapping triggers no QPromoteCustomTool
// question. Mapped entries are resolved silently and must not create interactive prompts.
func TestPromote_AutoMappedHarnessTool_TriggersNoQuestion(t *testing.T) {
	// Arrange — source with only "bash", which reverse-maps to generic "bash" automatically
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("bash"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — no QPromoteCustomTool for "bash"; it resolved automatically
	if stub.WasAsked(domain.QPromoteCustomTool, "bash") {
		t.Error("QPromoteCustomTool was asked for 'bash'; entries that reverse-map automatically must not trigger a question")
	}
}

// TestPromote_MixedHarnessTools_OnlyUnmappedTriggersQuestion verifies that when the source
// carries both a mapped and an unmapped harness tool, only the unmapped one triggers a
// QPromoteCustomTool question. This is the combination assertion: auto-mapped stays silent
// while unmapped produces exactly one question.
func TestPromote_MixedHarnessTools_OnlyUnmappedTriggersQuestion(t *testing.T) {
	// Arrange — source with "bash" (auto-maps to "bash") and "my-custom-tool" (unmapped)
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("bash", "my-custom-tool"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteCustomTool, "my-custom-tool", "file-reader").
		Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — no question for "bash" (auto-mapped), one question for "my-custom-tool"
	if stub.WasAsked(domain.QPromoteCustomTool, "bash") {
		t.Error("QPromoteCustomTool was asked for 'bash'; auto-reverse-mapped entries must not trigger a question")
	}
	if !stub.WasAsked(domain.QPromoteCustomTool, "my-custom-tool") {
		t.Error("QPromoteCustomTool was not asked for 'my-custom-tool'; unmapped entries must trigger a question in the interactive path")
	}
}

// TestPromote_AllToolsAutoMapped_NoCustomToolQuestion verifies that when every harness
// tool in the source reverse-maps automatically, QPromoteCustomTool is never asked.
// This confirms the "ask only for entries that have no mapping" contract.
func TestPromote_AllToolsAutoMapped_NoCustomToolQuestion(t *testing.T) {
	// Arrange — source with only "bash", which has a descriptor reverse mapping
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("bash"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — no QPromoteCustomTool of any kind
	for _, call := range stub.Calls() {
		if call.ID == domain.QPromoteCustomTool {
			t.Errorf("QPromoteCustomTool was asked (subject=%q) when all entries reverse-map automatically; no question must be asked", call.Subject)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.2: Non-interactive verbatim carry-through
// ---------------------------------------------------------------------------

// TestPromote_NonInteractive_UnmappedTool_CarriedVerbatimInToolsList verifies that in the
// non-interactive path an unmapped harness tool entry is carried into the generic tools
// list unchanged. The interaction stub returns SkippedOne (the default for unscripted
// text questions), simulating the non-interactive CLI implementation.
func TestPromote_NonInteractive_UnmappedTool_CarriedVerbatimInToolsList(t *testing.T) {
	// Arrange — source with "my-custom-tool"; no answer scripted so the stub returns SkippedOne
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("my-custom-tool"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build() // SkippedOne for every unscripted question
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — "my-custom-tool" appears verbatim in PromoteResult.Tools
	found := false
	for _, tool := range result.Tools {
		if tool == "my-custom-tool" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.Tools = %v; want 'my-custom-tool' carried verbatim when the question is skipped", result.Tools)
	}
}

// TestPromote_NonInteractive_UnmappedTools_ListedInVerbatimTools verifies that harness
// tool entries carried verbatim (because no generic name was supplied) appear in
// PromoteResult.VerbatimTools, making the limitation visible to the caller.
func TestPromote_NonInteractive_UnmappedTools_ListedInVerbatimTools(t *testing.T) {
	// Arrange — source with two unmapped tools; neither question is scripted
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("tool-alpha", "tool-beta"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — both unmapped tools appear in VerbatimTools
	verbatimSet := make(map[string]bool, len(result.VerbatimTools))
	for _, v := range result.VerbatimTools {
		verbatimSet[v] = true
	}
	for _, want := range []string{"tool-alpha", "tool-beta"} {
		if !verbatimSet[want] {
			t.Errorf("PromoteResult.VerbatimTools = %v; want %q listed (carried verbatim, not renamed)", result.VerbatimTools, want)
		}
	}
}

// TestPromote_NonInteractive_CompletesWithoutError verifies that a promote containing
// unmapped harness tools completes successfully in the non-interactive path. Verbatim
// carry-through must not return an error; it is a documented limitation, not a failure.
func TestPromote_NonInteractive_CompletesWithoutError(t *testing.T) {
	// Arrange — source with one unmapped tool; nothing scripted
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("my-custom-tool"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — must complete without error even though unmapped tools exist
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})

	// Assert — no error and a valid result
	if err != nil {
		t.Fatalf("Promote with unmapped tools returned an error: %v; "+
			"non-interactive verbatim carry-through must not produce an error", err)
	}
	if result.DestinationPath == "" {
		t.Error("PromoteResult.DestinationPath is empty; Promote must return a populated result on success")
	}
}

// TestPromote_ZeroHarnessToolEntries_NoCustomToolQuestion verifies that when the harness
// declares no tool entries at all (an empty tools list in the frontmatter), the service
// asks no QPromoteCustomTool question. There are no entries — mapped or unmapped — to
// resolve, so the interactive dialog must remain completely silent.
//
// This is distinct from the "all entries auto-map" case: here the source carries tools: []
// rather than a list whose every entry has a descriptor mapping.
func TestPromote_ZeroHarnessToolEntries_NoCustomToolQuestion(t *testing.T) {
	// Arrange — source with an empty tools list (no entries at all)
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — no QPromoteCustomTool asked when the harness declares zero tool entries
	for _, call := range stub.Calls() {
		if call.ID == domain.QPromoteCustomTool {
			t.Errorf("QPromoteCustomTool was asked (subject=%q) when the harness declares zero tool entries; no question must be asked", call.Subject)
		}
	}
}

// TestPromote_CancelledCustomToolQuestion_ToolCarriedVerbatim verifies that a Cancelled
// response to QPromoteCustomTool results in the harness tool name being carried verbatim
// into the generic tools list and reported in VerbatimTools. Every non-answered outcome —
// skip, skip-all, cancellation — must behave identically: carry the harness name through
// unmodified rather than blocking or returning an error.
func TestPromote_CancelledCustomToolQuestion_ToolCarriedVerbatim(t *testing.T) {
	// Arrange — source with one unmapped tool; script a Cancelled response for it
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("my-custom-tool"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().
		AnswerCancelledText(domain.QPromoteCustomTool, "my-custom-tool").
		Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — must complete without error even though the question was cancelled
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote with cancelled custom-tool question: %v; "+
			"a cancelled question must not propagate as an error — carry verbatim instead", err)
	}

	// Assert — harness name carried verbatim in the generic tools list
	foundInTools := false
	for _, tool := range result.Tools {
		if tool == "my-custom-tool" {
			foundInTools = true
			break
		}
	}
	if !foundInTools {
		t.Errorf("PromoteResult.Tools = %v; want 'my-custom-tool' carried verbatim when the question is cancelled", result.Tools)
	}

	// Assert — also surfaces in VerbatimTools so the caller can detect the limitation
	foundInVerbatim := false
	for _, v := range result.VerbatimTools {
		if v == "my-custom-tool" {
			foundInVerbatim = true
			break
		}
	}
	if !foundInVerbatim {
		t.Errorf("PromoteResult.VerbatimTools = %v; want 'my-custom-tool' listed when the question is cancelled (Cancelled is a non-answered outcome and must surface in VerbatimTools)", result.VerbatimTools)
	}
}

// TestPromote_SkippedAllLatches_RemainingToolsCarriedVerbatimWithoutQuestion verifies that
// when the user responds SkippedAll to the first QPromoteCustomTool, subsequent unmapped
// tools are carried verbatim without asking any further questions. The SkippedAll latch
// must prevent additional prompts for the rest of the run.
func TestPromote_SkippedAllLatches_RemainingToolsCarriedVerbatimWithoutQuestion(t *testing.T) {
	// Arrange — source with two unmapped tools; first question returns SkippedAll
	mosaicRoot := writeMosaicRoot(t)
	srcPath := filepath.Join(t.TempDir(), "tool-test-agent.md")
	if err := os.WriteFile(srcPath, eligibleAgentWithHarnessTools("tool-alpha", "tool-beta"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().
		AnswerSkipAll(domain.QPromoteCustomTool, "tool-alpha").
		Build()
	deps := newToolResolutionPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: toolResolutionHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — both tools appear verbatim (SkippedAll applied to tool-alpha carries both)
	verbatimSet := make(map[string]bool, len(result.VerbatimTools))
	for _, v := range result.VerbatimTools {
		verbatimSet[v] = true
	}
	for _, want := range []string{"tool-alpha", "tool-beta"} {
		if !verbatimSet[want] {
			t.Errorf("PromoteResult.VerbatimTools = %v; want %q (SkippedAll must carry all remaining tools verbatim)", result.VerbatimTools, want)
		}
	}

	// "tool-beta" must not have been asked — the SkippedAll latch suppressed it
	if stub.WasAsked(domain.QPromoteCustomTool, "tool-beta") {
		t.Error("QPromoteCustomTool was asked for 'tool-beta' after SkippedAll on 'tool-alpha'; the latch must suppress subsequent questions")
	}
}

// ---------------------------------------------------------------------------
// Stage 5: Missing generic-field recovery — service-layer tests
//
// A harness drops recommended_tier, tier_rationale, and required_skills at deploy time.
// The promote flow asks the user to supply these values when the source does not carry them.
// The tests in this section verify:
//
//   T5.1 — Interactive prompts (when source lacks the field):
//     - Each of the three recoverable fields triggers its question when the source lacks it.
//     - When the source already carries the field, the question is not asked (AC5.3).
//     - A supplied (answered) value is written to the promoted file (AC5.4).
//     - A supplied value appears in PromoteResult.RecoveredFields (AC5.4).
//
//   T5.2 — Unsupplied values (skipped/unanswered answers):
//     - A skipped answer results in the field being absent from the promoted file,
//       never written as an empty scalar or list (AD-12, AC5.5).
//     - The non-interactive path (all answers SkippedOne) completes without error (AC5.6).
//
//   T5.3 — List-valued required_skills (comma-separated entry):
//     - A comma-separated answer is split and trimmed into a list (AD-11).
//     - The split entries appear in the promoted file as a proper YAML list.
//
// Source fixture:
//   eligibleAgentWithoutRecoverableFields() — an eligible harness-only source that carries
//   none of the three recoverable fields. Using this fixture ensures the question fires.
//   Use eligibleHarnessOnlyAgentBytes() (which carries all three) to test the "not asked"
//   branch.
// ---------------------------------------------------------------------------

// eligibleAgentWithoutRecoverableFields returns bytes of an eligible harness-only agent that
// carries none of the three generic-only recoverable fields. Promote tests that want to
// trigger the recovery questions must use this fixture as the source.
func eligibleAgentWithoutRecoverableFields() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"id: \"33\"\n" +
		"version: \"1.0.0\"\n" +
		"name: field-recovery-agent\n" +
		"description: An agent for recoverable-field service tests\n" +
		"role: subagent\n" +
		// Intentionally absent: recommended_tier, tier_rationale, required_skills
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content for field recovery tests.\n" +
		"</Identity>\n")
}

// writeSourceFile writes src to t.TempDir() as "field-recovery-agent.md" and returns the path.
func writeSourceFile(t *testing.T, src []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "field-recovery-agent.md")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatalf("writeSourceFile: %v", err)
	}
	return path
}

// parseFrontmatterFieldSvc returns the scalar value and presence flag for a given key in
// the YAML frontmatter of a file at the given path. It is the service-test equivalent of
// the core-test parseFrontmatterField helper.
func parseFrontmatterFieldSvc(t *testing.T, filePath, key string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read promoted file: %v", err)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get(key)
	if !present {
		return "", false
	}
	return v.Scalar, true
}

// parseFrontmatterListSvc returns the list entries for a given key in the YAML frontmatter
// of a file at the given path. Returns nil if the key is absent.
func parseFrontmatterListSvc(t *testing.T, filePath, key string) []string {
	t.Helper()
	b, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read promoted file: %v", err)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	v, present := doc.Frontmatter().Get(key)
	if !present {
		return nil
	}
	if v.Kind != domain.KindList {
		t.Fatalf("key %q has kind %v, want KindList", key, v.Kind)
	}
	result := make([]string, len(v.Items))
	for i, item := range v.Items {
		result[i] = item.Scalar
	}
	return result
}

// ---------------------------------------------------------------------------
// T5.1: Interactive prompts — each field asked when source lacks it
// ---------------------------------------------------------------------------

// TestPromote_RecommendedTier_AskedWhenSourceLacksIt verifies that when the source file
// does not carry recommended_tier, the service asks QPromoteRecommendedTier through the
// Interaction port during the promote flow (AC5.2, AC5.8).
func TestPromote_RecommendedTier_AskedWhenSourceLacksIt(t *testing.T) {
	// Arrange — source has no recommended_tier; stub returns SkippedOne for everything.
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — QPromoteRecommendedTier must have been asked with the source path as subject.
	if !stub.WasAsked(domain.QPromoteRecommendedTier, srcPath) {
		t.Error("QPromoteRecommendedTier was not asked when source lacks recommended_tier; the service must prompt the user for this recoverable field")
	}
}

// TestPromote_TierRationale_AskedWhenSourceLacksIt verifies that when the source file
// does not carry tier_rationale, the service asks QPromoteTierRationale through the
// Interaction port during the promote flow (AC5.2, AC5.8).
func TestPromote_TierRationale_AskedWhenSourceLacksIt(t *testing.T) {
	// Arrange — source has no tier_rationale; stub returns SkippedOne for everything.
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert
	if !stub.WasAsked(domain.QPromoteTierRationale, srcPath) {
		t.Error("QPromoteTierRationale was not asked when source lacks tier_rationale; the service must prompt the user for this recoverable field")
	}
}

// TestPromote_RequiredSkills_AskedWhenSourceLacksIt verifies that when the source file
// does not carry required_skills, the service asks QPromoteRequiredSkills through the
// Interaction port during the promote flow (AC5.2, AC5.8).
func TestPromote_RequiredSkills_AskedWhenSourceLacksIt(t *testing.T) {
	// Arrange — source has no required_skills; stub returns SkippedOne for everything.
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert
	if !stub.WasAsked(domain.QPromoteRequiredSkills, srcPath) {
		t.Error("QPromoteRequiredSkills was not asked when source lacks required_skills; the service must prompt the user for this recoverable field")
	}
}

// ---------------------------------------------------------------------------
// T5.1: Each field NOT asked when the source already carries it (AC5.3)
// ---------------------------------------------------------------------------

// TestPromote_RecommendedTier_NotAskedWhenSourceCarriesIt verifies that when the source
// already carries recommended_tier, QPromoteRecommendedTier is not asked — asking would
// overwrite a value the user already set in the generic source (AC5.3).
func TestPromote_RecommendedTier_NotAskedWhenSourceCarriesIt(t *testing.T) {
	// Arrange — eligibleHarnessOnlyAgentBytes carries recommended_tier.
	src := eligibleHarnessOnlyAgentBytes()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert
	if stub.WasAsked(domain.QPromoteRecommendedTier, srcPath) {
		t.Error("QPromoteRecommendedTier was asked even though the source already carries recommended_tier; must not ask for a field the source already has (AC5.3)")
	}
}

// TestPromote_TierRationale_NotAskedWhenSourceCarriesIt verifies that when the source
// already carries tier_rationale, QPromoteTierRationale is not asked (AC5.3).
func TestPromote_TierRationale_NotAskedWhenSourceCarriesIt(t *testing.T) {
	src := eligibleHarnessOnlyAgentBytes()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	if stub.WasAsked(domain.QPromoteTierRationale, srcPath) {
		t.Error("QPromoteTierRationale was asked even though the source already carries tier_rationale; must not ask for a field the source already has (AC5.3)")
	}
}

// TestPromote_RequiredSkills_NotAskedWhenSourceCarriesIt verifies that when the source
// already carries required_skills, QPromoteRequiredSkills is not asked (AC5.3).
func TestPromote_RequiredSkills_NotAskedWhenSourceCarriesIt(t *testing.T) {
	src := eligibleHarnessOnlyAgentBytes()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	if stub.WasAsked(domain.QPromoteRequiredSkills, srcPath) {
		t.Error("QPromoteRequiredSkills was asked even though the source already carries required_skills; must not ask for a field the source already has (AC5.3)")
	}
}

// ---------------------------------------------------------------------------
// T5.1: Supplied (answered) values written to promoted file (AC5.4)
// ---------------------------------------------------------------------------

// TestPromote_RecommendedTierSupplied_WrittenToPromotedFile verifies that when the user
// supplies an answer for QPromoteRecommendedTier, the promoted file carries the supplied
// value as the recommended_tier scalar (AC5.4).
func TestPromote_RecommendedTierSupplied_WrittenToPromotedFile(t *testing.T) {
	// Arrange — stub answers QPromoteRecommendedTier with option "HIGH" (catalog entry).
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteRecommendedTier, srcPath, "HIGH").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — promoted file must carry recommended_tier: HIGH.
	gotTier, present := parseFrontmatterFieldSvc(t, result.DestinationPath, "recommended_tier")
	if !present {
		t.Fatal("recommended_tier is absent from promoted file; a supplied answer must be written to the output")
	}
	if gotTier != "HIGH" {
		t.Errorf("recommended_tier = %q, want %q", gotTier, "HIGH")
	}
}

// TestPromote_TierRationaleSupplied_WrittenToPromotedFile verifies that when the user
// supplies an answer for QPromoteTierRationale, the promoted file carries the supplied
// text as the tier_rationale scalar (AC5.4).
func TestPromote_TierRationaleSupplied_WrittenToPromotedFile(t *testing.T) {
	wantRationale := "needs high reasoning capability for complex tasks"
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteTierRationale, srcPath, wantRationale).
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotRationale, present := parseFrontmatterFieldSvc(t, result.DestinationPath, "tier_rationale")
	if !present {
		t.Fatal("tier_rationale is absent from promoted file; a supplied answer must be written to the output")
	}
	if gotRationale != wantRationale {
		t.Errorf("tier_rationale = %q, want %q", gotRationale, wantRationale)
	}
}

// TestPromote_RequiredSkillsSupplied_WrittenToPromotedFile verifies that when the user
// supplies a comma-separated answer for QPromoteRequiredSkills, the promoted file carries
// the split and trimmed entries as a proper YAML list (AD-11, T5.3, AC5.7).
func TestPromote_RequiredSkillsSupplied_WrittenToPromotedFile(t *testing.T) {
	// "lean-tdd, efficient-file-reading" — note the space after comma (must be trimmed).
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "lean-tdd, efficient-file-reading").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotSkills := parseFrontmatterListSvc(t, result.DestinationPath, "required_skills")
	if gotSkills == nil {
		t.Fatal("required_skills is absent from promoted file; a supplied comma-separated answer must be split and written as a list")
	}
	wantSkills := []string{"lean-tdd", "efficient-file-reading"}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Errorf("required_skills = %v, want %v (comma-separated input must be split and trimmed per AD-11)", gotSkills, wantSkills)
	}
}

// TestPromote_RecommendedTierSupplied_AppearsInRecoveredFields verifies that when the user
// supplies an answer for QPromoteRecommendedTier, PromoteResult.RecoveredFields includes
// "recommended_tier", indicating the field was recovered during this promote (AC5.4).
func TestPromote_RecommendedTierSupplied_AppearsInRecoveredFields(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteRecommendedTier, srcPath, "HIGH").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	found := false
	for _, f := range result.RecoveredFields {
		if f == "recommended_tier" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.RecoveredFields = %v; want it to contain %q when the user supplied a tier answer", result.RecoveredFields, "recommended_tier")
	}
}

// TestPromote_TierRationaleSupplied_AppearsInRecoveredFields verifies that when the user
// supplies an answer for QPromoteTierRationale, PromoteResult.RecoveredFields includes
// "tier_rationale" (AC5.4).
func TestPromote_TierRationaleSupplied_AppearsInRecoveredFields(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteTierRationale, srcPath, "needed for complex analysis").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	found := false
	for _, f := range result.RecoveredFields {
		if f == "tier_rationale" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.RecoveredFields = %v; want it to contain %q", result.RecoveredFields, "tier_rationale")
	}
}

// TestPromote_RequiredSkillsSupplied_AppearsInRecoveredFields verifies that when the user
// supplies an answer for QPromoteRequiredSkills, PromoteResult.RecoveredFields includes
// "required_skills" (AC5.4).
func TestPromote_RequiredSkillsSupplied_AppearsInRecoveredFields(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "lean-tdd").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	found := false
	for _, f := range result.RecoveredFields {
		if f == "required_skills" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.RecoveredFields = %v; want it to contain %q", result.RecoveredFields, "required_skills")
	}
}


// ---------------------------------------------------------------------------
// T5.2: Non-interactive path completes without error (AC5.6)
// ---------------------------------------------------------------------------

// TestPromote_RecoverableFieldsAllSkipped_CompletesWithoutError verifies that the promote
// flow completes successfully even when all three recoverable-field questions are unanswered
// (all SkippedOne). No error must be returned; a promoted file must be written; the three
// fields must simply be absent from the output (AC5.6).
func TestPromote_RecoverableFieldsAllSkipped_CompletesWithoutError(t *testing.T) {
	// Arrange — source lacks all three recoverable fields; all questions → SkippedOne.
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	// Act — non-interactive: no scripted answers, all questions silently skipped.
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert — no error and a non-empty destination path.
	if err != nil {
		t.Fatalf("Promote returned error %v; the non-interactive path must complete without blocking (AC5.6)", err)
	}
	if result.DestinationPath == "" {
		t.Error("PromoteResult.DestinationPath is empty; Promote must write a file even when all recoverable-field questions are skipped")
	}
	if _, statErr := os.Stat(result.DestinationPath); statErr != nil {
		t.Errorf("promoted file does not exist at %q: %v", result.DestinationPath, statErr)
	}

	// RecoveredFields must be empty (nothing was supplied).
	if len(result.RecoveredFields) != 0 {
		t.Errorf("PromoteResult.RecoveredFields = %v; want empty — no fields were answered", result.RecoveredFields)
	}
}

// ---------------------------------------------------------------------------
// T5.3: List-valued required_skills — comma-separated entry (AD-11, AC5.7)
// ---------------------------------------------------------------------------

// TestPromote_RequiredSkills_MultipleEntries_CommaSeparatedAnswer verifies that when the
// user enters multiple skills separated by commas, the service splits and trims the input
// so the promoted file receives a proper list of individual entries (AD-11, AC5.7).
func TestPromote_RequiredSkills_MultipleEntries_CommaSeparatedAnswer(t *testing.T) {
	// Two skills with whitespace around the separator — both must be trimmed.
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "lean-tdd ,  efficient-file-reading").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotSkills := parseFrontmatterListSvc(t, result.DestinationPath, "required_skills")
	if gotSkills == nil {
		t.Fatal("required_skills is absent from promoted file; comma-separated input must produce a list")
	}
	wantSkills := []string{"lean-tdd", "efficient-file-reading"}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Errorf("required_skills = %v, want %v; comma separation and whitespace trimming must be applied (AD-11)", gotSkills, wantSkills)
	}
}

// TestPromote_RequiredSkills_EmptyCommaEntries_Discarded verifies that empty entries
// produced by splitting a comma-separated answer (e.g. "lean-tdd,,") are discarded
// so the resulting list contains only non-empty entries (AD-11).
func TestPromote_RequiredSkills_EmptyCommaEntries_Discarded(t *testing.T) {
	// "lean-tdd,," — two trailing commas produce two empty entries, which must be discarded.
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "lean-tdd,,").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotSkills := parseFrontmatterListSvc(t, result.DestinationPath, "required_skills")
	wantSkills := []string{"lean-tdd"}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Errorf("required_skills = %v, want %v; empty comma entries must be discarded (AD-11)", gotSkills, wantSkills)
	}
}


// TestPromote_EachRecoverableField_AskedAtMostOnce verifies that each recoverable-field
// question is asked at most once per promote invocation, even if the source lacks all three
// fields (AC5.2 — ask-at-most-once rule).
func TestPromote_EachRecoverableField_AskedAtMostOnce(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	for _, qid := range []domain.QuestionID{
		domain.QPromoteRecommendedTier,
		domain.QPromoteTierRationale,
		domain.QPromoteRequiredSkills,
	} {
		if n := stub.CountAsked(qid, srcPath); n > 1 {
			t.Errorf("question %q was asked %d times; each recoverable-field question must be asked at most once per promote (AC5.2)", qid, n)
		}
	}
}

// TestPromote_SkippedRecoverableFields_AbsentFromRecoveredFields verifies that when all
// three recoverable-field questions are skipped, PromoteResult.RecoveredFields is empty —
// only answered (supplied) fields should appear in RecoveredFields.
func TestPromote_SkippedRecoverableFields_AbsentFromRecoveredFields(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build() // all SkippedOne
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// RecoveredFields tracks only fields where the user supplied a value.
	for _, f := range result.RecoveredFields {
		switch f {
		case "recommended_tier", "tier_rationale", "required_skills":
			t.Errorf("PromoteResult.RecoveredFields contains %q even though the question was skipped; only answered fields must appear in RecoveredFields", f)
		}
	}
}

// TestPromote_AllRecoverableFieldsSupplied_AllAppearInRecoveredFields verifies that when
// all three recoverable-field questions are answered, all three field names appear in
// PromoteResult.RecoveredFields (AC5.4).
func TestPromote_AllRecoverableFieldsSupplied_AllAppearInRecoveredFields(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteRecommendedTier, srcPath, "HIGH").
		AnswerText(domain.QPromoteTierRationale, srcPath, "high reasoning needed").
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "lean-tdd").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	recoveredSet := make(map[string]bool, len(result.RecoveredFields))
	for _, f := range result.RecoveredFields {
		recoveredSet[f] = true
	}
	for _, want := range []string{"recommended_tier", "tier_rationale", "required_skills"} {
		if !recoveredSet[want] {
			t.Errorf("PromoteResult.RecoveredFields = %v; want it to contain %q (all supplied fields must be reported)", result.RecoveredFields, want)
		}
	}
}

// TestPromote_CustomTierValue_WrittenToPromotedFile verifies that when the user enters a
// custom tier value (not in the catalog list) via SelectOne with AllowCustom: true, the
// custom value is written to the promoted file (AD-10).
func TestPromote_CustomTierValue_WrittenToPromotedFile(t *testing.T) {
	wantTier := "CUSTOM-TIER-7"
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QPromoteRecommendedTier, srcPath, wantTier).
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotTier, present := parseFrontmatterFieldSvc(t, result.DestinationPath, "recommended_tier")
	if !present {
		t.Fatal("recommended_tier is absent from promoted file; a custom-value answer must be written")
	}
	if gotTier != wantTier {
		t.Errorf("recommended_tier = %q, want %q (custom tier value must be used verbatim)", gotTier, wantTier)
	}
}

// TestPromote_CustomTierValue_AppearsInRecoveredFields verifies that a custom-value tier
// answer is reported in PromoteResult.RecoveredFields just like a catalog option (AD-10).
func TestPromote_CustomTierValue_AppearsInRecoveredFields(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QPromoteRecommendedTier, srcPath, "CUSTOM-TIER-X").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	found := false
	for _, f := range result.RecoveredFields {
		if f == "recommended_tier" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.RecoveredFields = %v; custom tier answer must still include %q in RecoveredFields", result.RecoveredFields, "recommended_tier")
	}
}

// TestPromote_StringsContainsAllRecoverableFieldQuestions_InAnsweredFields verifies that
// PromoteResult.RecoveredFields does not contain field names for questions the source
// already carried — only truly recovered (newly supplied) fields are reported. This test
// uses a source that already has recommended_tier and supplies the other two.
func TestPromote_SourceHasTier_OnlyRationaleAndSkillsInRecoveredFields(t *testing.T) {
	// Source carries recommended_tier but not tier_rationale or required_skills.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"bundle_version: \"1.0.0\"\n" +
		"protocol_version: \"1.9\"\n" +
		"id: \"99\"\n" +
		"version: \"1.0.0\"\n" +
		"name: partial-recovery-agent\n" +
		"description: Has recommended_tier but not the other two\n" +
		"role: subagent\n" +
		"recommended_tier: HIGH\n" +
		// No tier_rationale, no required_skills
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteTierRationale, srcPath, "complex reasoning required").
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "lean-tdd").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	recoveredSet := make(map[string]bool, len(result.RecoveredFields))
	for _, f := range result.RecoveredFields {
		recoveredSet[f] = true
	}

	// recommended_tier was already in the source — it must not appear in RecoveredFields.
	if recoveredSet["recommended_tier"] {
		t.Error("recommended_tier appears in RecoveredFields even though the source already carried it; only fields newly recovered during this promote must be reported")
	}

	// tier_rationale and required_skills were newly supplied — both must be reported.
	for _, want := range []string{"tier_rationale", "required_skills"} {
		if !recoveredSet[want] {
			t.Errorf("PromoteResult.RecoveredFields = %v; want it to contain %q (was newly supplied)", result.RecoveredFields, want)
		}
	}

	_ = strings.Contains // prevent unused-import error if strings is only used elsewhere
}

// ---------------------------------------------------------------------------
// Stage 6: Source/Target Parity — real OpenCode fixture
// ---------------------------------------------------------------------------

// openCodeRealFixtureModule faithfully replicates the OpenCode harness descriptor for the
// Stage 6 end-to-end fixture tests. It models:
//
//   - FrontmatterSpec: ModelKey="model", ToolsKey="permission"
//   - Tools.Shape: ShapePermission
//   - Frontmatter plan: adds "mode" key so promoteHarnessDropKeys picks it up
//   - Tool universe: 15 tools matching opencode.yaml; "list" is ByConvention=true
//   - 9 tool mappings from opencode.yaml
//
// It implements domain.HarnessModule but is not a real OpenCode module — it never touches
// the filesystem and avoids any dependency on the MosaicRoot injections directory.
type openCodeRealFixtureModule struct {
	ref domain.HarnessRef
}

// openCodeFixtureHarness is the HarnessRef paired with openCodeRealFixtureModule.
var openCodeFixtureHarness = domain.HarnessRef{
	ID:          "opencode-fixture",
	DisplayName: "OpenCode (fixture)",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

func (m *openCodeRealFixtureModule) Ref() domain.HarnessRef { return m.ref }

func (m *openCodeRealFixtureModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID: m.ref.ID,
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
			ToolsKey: "permission",
		},
		Tools: domain.ToolSpec{
			Shape: domain.ShapePermission,
			Universe: []domain.HarnessTool{
				{Name: "read", Unused: domain.Deny},
				{Name: "write", Unused: domain.Deny},
				{Name: "edit", Unused: domain.Deny},
				{Name: "glob", Unused: domain.Deny},
				{Name: "grep", Unused: domain.Deny},
				{Name: "list", Unused: domain.Allow, ByConvention: true},
				{Name: "bash", Unused: domain.Deny},
				{Name: "patch", Unused: domain.Deny},
				{Name: "webfetch", Unused: domain.Deny},
				{Name: "question", Unused: domain.Deny},
				{Name: "lsp", Unused: domain.Deny},
				{Name: "task", Unused: domain.Deny},
				{Name: "todowrite", Unused: domain.Deny},
				{Name: "todoread", Unused: domain.Deny},
				{Name: "skill", Unused: domain.Deny},
			},
			Mappings: []domain.ToolMapping{
				{Generic: "file_read", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"read"}}}},
				{Generic: "file_write", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"write"}}}},
				{Generic: "file_edit", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"edit"}}}},
				{Generic: "file_search", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"glob"}}}},
				{Generic: "content_search", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"grep"}}}},
				{Generic: "terminal", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"bash"}}}},
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"question"}}}},
				{Generic: "subagent", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"task"}}}},
				{Generic: "skill", Destinations: []domain.ToolDestination{{Kind: domain.DestMain, Names: []string{"skill"}}}},
			},
		},
	}
}

func (m *openCodeRealFixtureModule) Tools(_ domain.ToolRequest) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}

// Frontmatter returns the plan that OpenCode would apply at deploy time. The "mode" key is
// in FrontmatterPlan.Set so promoteHarnessDropKeys picks it up as a harness-added key to
// drop during promotion. Only the key name matters; the value is never read by promote.
func (m *openCodeRealFixtureModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{
		Set: []domain.FrontmatterField{
			{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
		},
	}, nil
}

func (m *openCodeRealFixtureModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}

func (m *openCodeRealFixtureModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}

func (m *openCodeRealFixtureModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

func (m *openCodeRealFixtureModule) Close() error { return nil }

// newOpenCodeFixtureDeps returns app.Deps wired for Stage 6 fixture tests:
//   - an empty catalog so nextNumericID returns "1" (matching the expected fixture's id field)
//   - openCodeRealFixtureModule as the sole registry entry under openCodeFixtureHarness.ID
//
// The returned mosaicRoot is the temp MOSAIC root the promote output will be written into.
func newOpenCodeFixtureDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	mosaicRoot := writeMosaicRoot(t)
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	// Empty catalog → nextNumericID returns "1", matching the expected fixture.
	deps.Catalog = &stubCatalog{root: mosaicRoot}
	mod := &openCodeRealFixtureModule{ref: openCodeFixtureHarness}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{openCodeFixtureHarness},
		modules: map[string]domain.HarnessModule{openCodeFixtureHarness.ID: mod},
	}
	return deps, mosaicRoot
}

// TestPromote_RealOpenCodeFixture_Interactive_WholeFileMatchesExpected promotes the real
// OpenCode-deployed codebase-research-new fixture against a stub that faithfully replicates
// the OpenCode harness descriptor and asserts that the produced generic file is byte-identical
// to the expected corrected output at testdata/promote/codebase-research-new-expected.md.
//
// All optional recovery questions (recommended_tier, tier_rationale, required_skills) receive
// SkippedOne so those fields are absent from the output — the expected fixture also omits them,
// keeping this test focused on the harness-specific field and tool-mapping corrections.
//
// Any deviation indicates a regression in the promote field-policy implementation or an error
// in the expected fixture. Treat both as real defects; never weaken this assertion.
func TestPromote_RealOpenCodeFixture_Interactive_WholeFileMatchesExpected(t *testing.T) {
	// Arrange — load source and expected fixtures from testdata.
	srcFixture := filepath.Join("testdata", "promote", "codebase-research-new-source.md")
	expectedFixture := filepath.Join("testdata", "promote", "codebase-research-new-expected.md")

	srcBytes, err := os.ReadFile(srcFixture)
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	wantBytes, err := os.ReadFile(expectedFixture)
	if err != nil {
		t.Fatalf("read expected fixture: %v", err)
	}

	// Copy the source to a temp dir with the canonical filename so the key derivation
	// produces "codebase-research-new" and the destination path matches the expected fixture.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "codebase-research-new.md")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newOpenCodeFixtureDeps(t, stub)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "Research",
		HarnessID: openCodeFixtureHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Assert — whole-file byte comparison against the expected fixture.
	gotBytes, err := os.ReadFile(result.DestinationPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Errorf("promoted file does not match expected fixture\ngot:\n%s\nwant:\n%s",
			string(gotBytes), string(wantBytes))
	}
}

// TestPromote_RealOpenCodeFixture_NonInteractive_ZeroBlockingInteraction promotes the same
// real OpenCode-deployed fixture with all questions defaulting to SkippedOne (the autonomous
// path). It asserts:
//   - Promote returns no error (the autonomous path must always complete without blocking)
//   - PromoteResult.VerbatimTools is empty (all permission allow entries were reverse-mapped)
//   - QPromoteCustomTool was never asked (no unmapped harness tools)
//   - The output carries no mode, model, or permission key
func TestPromote_RealOpenCodeFixture_NonInteractive_ZeroBlockingInteraction(t *testing.T) {
	// Arrange
	srcFixture := filepath.Join("testdata", "promote", "codebase-research-new-source.md")
	srcBytes, err := os.ReadFile(srcFixture)
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "codebase-research-new.md")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newOpenCodeFixtureDeps(t, stub)
	svc := app.New(deps)

	// Act
	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "Research",
		HarnessID: openCodeFixtureHarness.ID,
	})

	// Assert — autonomous path completes without error.
	if err != nil {
		t.Fatalf("Promote returned an error on the non-interactive path: %v", err)
	}

	// Assert — complete reverse mapping: every allow entry was mapped, none carried verbatim.
	if len(result.VerbatimTools) != 0 {
		t.Errorf("VerbatimTools = %v; want empty (all OpenCode permission allow entries must "+
			"reverse-map to generic names — none should be carried verbatim)", result.VerbatimTools)
	}

	// Assert — QPromoteCustomTool never asked: the reverse mapping is complete.
	if stub.WasAsked(domain.QPromoteCustomTool, srcPath) {
		t.Error("QPromoteCustomTool was asked; want zero custom-tool questions because every " +
			"non-by-convention allow entry has a reverse mapping in the OpenCode descriptor")
	}

	// Assert — harness-specific keys absent from the output file.
	for _, key := range []string{"mode", "model", "permission"} {
		if _, present := parseFrontmatterFieldSvc(t, result.DestinationPath, key); present {
			t.Errorf("output carries frontmatter key %q; must be absent from the promoted generic file", key)
		}
	}
}

// TestPromote_RealOpenCodeFixture_Parity_BodyAndFrontmatter asserts the behaviours that were
// already correct before this work remain correct after it:
//
// Body parity:
//   - <... type="core"> blocks carry their prose byte-identical from the source fixture
//   - <... type="project"> regions are empty tag pairs (no content between open/close tags)
//   - <... type="managed"> regions are empty tag pairs
//
// Frontmatter parity:
//   - id is derived from the catalog (empty catalog → "1"), not carried from the source
//   - version, name, role, description match the expected output
func TestPromote_RealOpenCodeFixture_Parity_BodyAndFrontmatter(t *testing.T) {
	// Arrange
	srcFixture := filepath.Join("testdata", "promote", "codebase-research-new-source.md")
	srcBytes, err := os.ReadFile(srcFixture)
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "codebase-research-new.md")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newOpenCodeFixtureDeps(t, stub)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "Research",
		HarnessID: openCodeFixtureHarness.ID,
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	destBytes, err := os.ReadFile(result.DestinationPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	destStr := string(destBytes)

	// --- Frontmatter field parity ---
	for _, tc := range []struct{ key, want string }{
		{"id", "1"},
		{"version", "3.0.0"},
		{"name", "codebase-research-new"},
		{"role", "subagent"},
		{"description", "Analyzes codebase, explores existing patterns, and documents findings to build foundational understanding for downstream agents"},
	} {
		got, present := parseFrontmatterFieldSvc(t, result.DestinationPath, tc.key)
		if !present {
			t.Errorf("frontmatter key %q is absent; want %q", tc.key, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("frontmatter key %q = %q; want %q", tc.key, got, tc.want)
		}
	}

	// --- Body section prose parity ---
	// Spot-check a characteristic excerpt from each canonical SECTION block to confirm the
	// prose was carried byte-identical from the source fixture. The full-file comparison in
	// the interactive test covers exact bytes; these checks document per-section intent.
	// OutputFormat is a retired section and is intentionally absent from this list.
	for _, sc := range []struct {
		section string
		excerpt string
	}{
		{"Identity", "You are the **Codebase Research** agent"},
		{"Capabilities", "### Core Capabilities"},
		{"Constraints", "Stay within your defined role"},
		{"ErrorHandling", "Retry transient errors once"},
		{"ExecutionPhilosophy", "Exploration Mindset"},
	} {
		if !strings.Contains(destStr, sc.excerpt) {
			t.Errorf("output missing expected excerpt from <%s type=\"core\">: %q", sc.section, sc.excerpt)
		}
	}

	// OutputFormat is a retired section. A promoted generic output must not contain it.
	if strings.Contains(destStr, "<OutputFormat type=\"core\">") {
		t.Error("output contains <OutputFormat type=\"core\"> — the OutputFormat section is retired and must be absent from promoted output")
	}

	// --- Injection region parity: must be empty tag pairs ---
	for _, tag := range []string{
		"IdentityExtension",
		"ProtocolExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"ContextLimits",
		"ErrorHandlingExtension",
	} {
		open := "<" + tag + " type=\"project\">"
		closeTag := "</" + tag + ">"
		openIdx := strings.Index(destStr, open)
		if openIdx < 0 {
			t.Errorf("output missing injection open tag <%s type=\"project\">", tag)
			continue
		}
		closeIdx := strings.Index(destStr, closeTag)
		if closeIdx < 0 {
			t.Errorf("output missing injection close tag </%s>", tag)
			continue
		}
		between := destStr[openIdx+len(open) : closeIdx]
		if strings.TrimSpace(between) != "" {
			t.Errorf("<%s type=\"project\"> region is not empty: content = %q", tag, between)
		}
	}

	// --- Deployed region parity: must be empty tag pairs ---
	for _, tag := range []string{
		"CommunicationProtocol",
	} {
		open := "<" + tag + " type=\"managed\">"
		closeTag := "</" + tag + ">"
		openIdx := strings.Index(destStr, open)
		if openIdx < 0 {
			t.Errorf("output missing deployed open tag <%s type=\"managed\">", tag)
			continue
		}
		closeIdx := strings.Index(destStr, closeTag)
		if closeIdx < 0 {
			t.Errorf("output missing deployed close tag </%s>", tag)
			continue
		}
		between := destStr[openIdx+len(open) : closeIdx]
		if strings.TrimSpace(between) != "" {
			t.Errorf("<%s type=\"managed\"> region is not empty: content = %q", tag, between)
		}
	}
}

// ---------------------------------------------------------------------------
// Stage 3: Always-present deployment metadata — service-layer tests
//
// After Stage 3 the promote service must always write recommended_tier,
// tier_rationale, and required_skills to the promoted file — even when the
// corresponding question is skipped, cancelled, or not answered. Unanswered
// questions produce empty values; values the source already carries are
// preserved unchanged.
//
// The "skipped → empty present" tests below are TDD RED: they fail with the
// current implementation because the service+core combination today omits the
// three fields when no answer is supplied. They will pass after I3.1.
//
// The "source carries → retained" tests are regression guards that document
// already-correct behaviour and verify it is not broken by the Stage 3 changes.
// ---------------------------------------------------------------------------

// TestPromote_RecommendedTierSkipped_PresentAsEmptyScalarInWrittenFile verifies that
// when the user skips QPromoteRecommendedTier, the promoted file carries
// recommended_tier with an empty scalar value — never absent. Scoped AD-12 override.
func TestPromote_RecommendedTierSkipped_PresentAsEmptyScalarInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build() // all questions → SkippedOne
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	// Guard: ensure the file was actually written.
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — the file may be empty; guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("recommended_tier")
	if !present {
		t.Error("recommended_tier is absent from promoted file after a skipped answer; the key must be present with an empty value (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("recommended_tier has kind %v, want KindScalar (empty scalar)", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("recommended_tier = %q, want empty string; a skipped answer must produce an empty scalar", v.Scalar)
	}
}

// TestPromote_TierRationaleSkipped_PresentAsEmptyScalarInWrittenFile verifies that when
// the user skips QPromoteTierRationale, the promoted file carries tier_rationale with an
// empty scalar value — never absent. Scoped AD-12 override.
func TestPromote_TierRationaleSkipped_PresentAsEmptyScalarInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("tier_rationale")
	if !present {
		t.Error("tier_rationale is absent from promoted file after a skipped answer; the key must be present with an empty value (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("tier_rationale has kind %v, want KindScalar (empty scalar)", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("tier_rationale = %q, want empty string; a skipped answer must produce an empty scalar", v.Scalar)
	}
}

// TestPromote_RequiredSkillsSkipped_PresentAsEmptyListInWrittenFile verifies that when
// the user skips QPromoteRequiredSkills, the promoted file carries required_skills as an
// empty flow list — never absent. Scoped AD-12 override.
func TestPromote_RequiredSkillsSkipped_PresentAsEmptyListInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("required_skills")
	if !present {
		t.Error("required_skills is absent from promoted file after a skipped answer; the key must be present as an empty flow list (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindList {
		t.Errorf("required_skills has kind %v, want KindList (empty flow list)", v.Kind)
	}
	if len(v.Items) != 0 {
		t.Errorf("required_skills has %d item(s) after a skipped answer; want an empty list: %v", len(v.Items), v.Items)
	}
}

// TestPromote_AllMetadataFieldsSkipped_AllPresentWithEmptyValues verifies that when all
// three promote questions are skipped (the default non-interactive behaviour), the promoted
// file carries all three deployment-metadata fields with empty values. The scoped AD-12
// override must apply to all three fields simultaneously.
func TestPromote_AllMetadataFieldsSkipped_AllPresentWithEmptyValues(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build() // all questions → SkippedOne
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	fm := doc.Frontmatter()
	if _, present := fm.Get("recommended_tier"); !present {
		t.Error("recommended_tier is absent; all three deployment-metadata fields must be present even when all questions were skipped")
	}
	if _, present := fm.Get("tier_rationale"); !present {
		t.Error("tier_rationale is absent; all three deployment-metadata fields must be present even when all questions were skipped")
	}
	if _, present := fm.Get("required_skills"); !present {
		t.Error("required_skills is absent; all three deployment-metadata fields must be present even when all questions were skipped")
	}
}

// TestPromote_TierRationaleCancelled_PresentAsEmptyScalarInWrittenFile verifies that when
// QPromoteTierRationale is explicitly cancelled, the promoted file still carries
// tier_rationale with an empty scalar value. Cancelled is a non-answered outcome and must
// behave identically to skipped under the scoped AD-12 override.
func TestPromote_TierRationaleCancelled_PresentAsEmptyScalarInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerCancelledText(domain.QPromoteTierRationale, srcPath).
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("tier_rationale")
	if !present {
		t.Error("tier_rationale is absent from promoted file after a cancelled answer; a cancelled answer must produce an empty scalar, not omit the key")
		return
	}
	if v.Scalar != "" {
		t.Errorf("tier_rationale = %q, want empty string; a cancelled answer must produce an empty scalar value", v.Scalar)
	}
}

// TestPromote_RequiredSkillsCancelled_PresentAsEmptyListInWrittenFile verifies that when
// QPromoteRequiredSkills is explicitly cancelled, the promoted file still carries
// required_skills as an empty flow list. Cancelled behaves identically to skipped.
func TestPromote_RequiredSkillsCancelled_PresentAsEmptyListInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerCancelledText(domain.QPromoteRequiredSkills, srcPath).
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("required_skills")
	if !present {
		t.Error("required_skills is absent from promoted file after a cancelled answer; a cancelled answer must produce an empty list, not omit the key")
		return
	}
	if v.Kind != domain.KindList {
		t.Errorf("required_skills has kind %v, want KindList (empty flow list)", v.Kind)
	}
	if len(v.Items) != 0 {
		t.Errorf("required_skills has %d item(s) after a cancelled answer; want an empty list: %v", len(v.Items), v.Items)
	}
}

// TestPromote_RecommendedTierCancelled_PresentAsEmptyScalarInWrittenFile verifies that when
// QPromoteRecommendedTier is explicitly cancelled, the promoted file still carries
// recommended_tier with an empty scalar value. Cancelled is a non-answered outcome and must
// behave identically to skipped under the scoped AD-12 override.
func TestPromote_RecommendedTierCancelled_PresentAsEmptyScalarInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerCancelledText(domain.QPromoteRecommendedTier, srcPath).
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("recommended_tier")
	if !present {
		t.Error("recommended_tier is absent from promoted file after a cancelled answer; a cancelled answer must produce an empty scalar, not omit the key")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("recommended_tier has kind %v, want KindScalar (empty scalar)", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("recommended_tier = %q, want empty string; a cancelled answer must produce an empty scalar value", v.Scalar)
	}
}

// ---------------------------------------------------------------------------
// Stage 3: Empty-text answers produce present fields with empty values
//
// These tests verify that when a user explicitly answers a question with an
// empty string (as distinct from skipping or cancelling), the three
// deployment-metadata fields are still written with empty values in the
// promoted file. The scoped AD-12 override applies to empty-text answers
// just as it does to skipped and cancelled answers.
//
// These are TDD RED: they fail with the current implementation and will pass
// after I3.1.
// ---------------------------------------------------------------------------

// TestPromote_RecommendedTierAnsweredEmpty_PresentAsEmptyScalarInWrittenFile verifies that
// when the user answers QPromoteRecommendedTier with an empty string, the promoted file
// carries recommended_tier with an empty scalar value — never absent. The design treats
// an empty-text answer as equivalent to skipped or cancelled for this field.
func TestPromote_RecommendedTierAnsweredEmpty_PresentAsEmptyScalarInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteRecommendedTier, srcPath, "").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("recommended_tier")
	if !present {
		t.Error("recommended_tier is absent from promoted file after an empty-text answer; the key must be present with an empty value (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("recommended_tier has kind %v, want KindScalar (empty scalar)", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("recommended_tier = %q, want empty string; an empty-text answer must produce an empty scalar", v.Scalar)
	}
}

// TestPromote_TierRationaleAnsweredEmpty_PresentAsEmptyScalarInWrittenFile verifies that
// when the user answers QPromoteTierRationale with an empty string, the promoted file
// carries tier_rationale with an empty scalar value — never absent.
func TestPromote_TierRationaleAnsweredEmpty_PresentAsEmptyScalarInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteTierRationale, srcPath, "").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("tier_rationale")
	if !present {
		t.Error("tier_rationale is absent from promoted file after an empty-text answer; the key must be present with an empty value (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("tier_rationale has kind %v, want KindScalar (empty scalar)", v.Kind)
	}
	if v.Scalar != "" {
		t.Errorf("tier_rationale = %q, want empty string; an empty-text answer must produce an empty scalar", v.Scalar)
	}
}

// TestPromote_RequiredSkillsAnsweredEmpty_PresentAsEmptyListInWrittenFile verifies that
// when the user answers QPromoteRequiredSkills with an empty string (yielding no non-empty
// entries after splitting), the promoted file carries required_skills as an empty flow list
// — never absent. An empty-text answer is equivalent to skipped or cancelled.
func TestPromote_RequiredSkillsAnsweredEmpty_PresentAsEmptyListInWrittenFile(t *testing.T) {
	src := eligibleAgentWithoutRecoverableFields()
	srcPath := writeSourceFile(t, src)
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteRequiredSkills, srcPath, "").
		Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	b, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("read promoted file: %v", readErr)
	}
	doc, parseErr := docformat.Parse(b)
	if parseErr != nil {
		t.Fatalf("parse promoted file: %v", parseErr)
	}
	if _, ok := doc.Frontmatter().Get("id"); !ok {
		t.Fatal("promoted file has no `id` key — guard prevents vacuous pass")
	}
	v, present := doc.Frontmatter().Get("required_skills")
	if !present {
		t.Error("required_skills is absent from promoted file after an empty-text answer; the key must be present as an empty flow list (scoped AD-12 override)")
		return
	}
	if v.Kind != domain.KindList {
		t.Errorf("required_skills has kind %v, want KindList (empty flow list)", v.Kind)
	}
	if len(v.Items) != 0 {
		t.Errorf("required_skills has %d item(s) after an empty-text answer; want an empty list: %v", len(v.Items), v.Items)
	}
}

// ---------------------------------------------------------------------------
// Stage 3: Source-carried metadata field values are retained
//
// These regression guards verify that when the source already carries one of
// the three deployment-metadata fields, its value is preserved in the promoted
// file. This behaviour was already correct before Stage 3; the tests ensure it
// is not broken by the AD-12 override change.
// ---------------------------------------------------------------------------

// TestPromote_SourceCarriesRecommendedTier_ValueRetainedInWrittenFile verifies that when
// the source already carries recommended_tier, the promoted file contains the source's
// original value — not an empty string. The "don't re-ask for a field the source already
// carries" rule means the source value passes through unchanged.
func TestPromote_SourceCarriesRecommendedTier_ValueRetainedInWrittenFile(t *testing.T) {
	// eligibleHarnessOnlyAgentBytes carries recommended_tier: MEDIUM.
	srcPath := writeSourceFile(t, eligibleHarnessOnlyAgentBytes())
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotTier, present := parseFrontmatterFieldSvc(t, result.DestinationPath, "recommended_tier")
	if !present {
		t.Fatal("recommended_tier is absent from promoted file; a source-carried value must be preserved in the output")
	}
	if gotTier != "MEDIUM" {
		t.Errorf("recommended_tier = %q, want %q; the source's carried value must be preserved unchanged", gotTier, "MEDIUM")
	}
}

// TestPromote_SourceCarriesTierRationale_ValueRetainedInWrittenFile verifies that when
// the source already carries tier_rationale, the promoted file contains the source's
// original value unchanged.
func TestPromote_SourceCarriesTierRationale_ValueRetainedInWrittenFile(t *testing.T) {
	// eligibleHarnessOnlyAgentBytes carries tier_rationale: "moderate capability needed for this task".
	srcPath := writeSourceFile(t, eligibleHarnessOnlyAgentBytes())
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotRationale, present := parseFrontmatterFieldSvc(t, result.DestinationPath, "tier_rationale")
	if !present {
		t.Fatal("tier_rationale is absent from promoted file; a source-carried value must be preserved in the output")
	}
	const wantRationale = "moderate capability needed for this task"
	if gotRationale != wantRationale {
		t.Errorf("tier_rationale = %q, want %q; the source's carried value must be preserved unchanged", gotRationale, wantRationale)
	}
}

// TestPromote_SourceCarriesRequiredSkills_ValueRetainedInWrittenFile verifies that when
// the source already carries required_skills, the promoted file contains the source's
// list value unchanged.
func TestPromote_SourceCarriesRequiredSkills_ValueRetainedInWrittenFile(t *testing.T) {
	// eligibleHarnessOnlyAgentBytes carries required_skills: [lean-tdd].
	srcPath := writeSourceFile(t, eligibleHarnessOnlyAgentBytes())
	mosaicRoot := t.TempDir()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	gotSkills := parseFrontmatterListSvc(t, result.DestinationPath, "required_skills")
	if gotSkills == nil {
		t.Fatal("required_skills is absent from promoted file; a source-carried list must be preserved in the output")
	}
	wantSkills := []string{"lean-tdd"}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Errorf("required_skills = %v, want %v; the source's carried list must be preserved unchanged", gotSkills, wantSkills)
	}
}
