package app_test

// transform_service_test.go covers the service-level TransformHarness flow:
// single-file happy path, batch directory input, overwrite protection, and interactive
// question routing.
//
// All tests call svc.TransformHarness, which currently returns errTransformNotImplemented
// (TDD RED phase). Every assertion that expects a successful result or a specific per-file
// outcome will fail until I6.1 and I6.2 are delivered and the stub is replaced.
//
// Verified behaviours (T6.1-T6.4):
//
// Single-file flow (T6.1):
//   - A pre-answered request pointing to a source-harness file produces one
//     TransformFileOutcome with StatusTransformed and a non-empty DestinationPath.
//   - SourceHarnessID and TargetHarnessID are echoed into TransformHarnessResult.
//   - No interactive questions are asked when all request fields are pre-answered.
//   - InputIsDirectory is false for a single-file input.
//   - DryRun=true produces StatusTransformed with no file written to disk.
//
// Batch directory input (T6.2):
//   - A directory containing a matching file, a mismatching file, and a non-agent file
//     produces three outcomes in enumeration order.
//   - The matching file gets StatusTransformed; the mismatching file gets
//     StatusSkippedMismatch; the non-agent file gets StatusSkippedNotAgent.
//   - Count fields (Transformed, SkippedMismatch, SkippedNotAgent) match the outcome list.
//   - InputIsDirectory is true for a directory input.
//   - A per-file failure (mismatch) does not stop the batch; remaining files are still processed.
//
// Overwrite protection (T6.3):
//   - When the destination file already exists and Overwrite=false, the outcome is
//     StatusFailed with a reason mentioning the destination path.
//   - When the destination file already exists and Overwrite=true, the outcome is
//     StatusTransformed and the destination is replaced.
//   - A refused overwrite leaves the existing destination unchanged.
//
// Interactive question routing (T6.4):
//   - When TargetHarnessID is empty, QTransformTargetHarness is asked through the
//     Interaction port.
//   - When TargetHarnessID is non-empty, QTransformTargetHarness is NOT asked.
//   - When TargetModel is empty, QTransformTargetModel is asked.
//   - When TargetModel is non-empty, QTransformTargetModel is NOT asked.
//   - A cancelled QTransformTargetHarness returns a non-nil error and no file is written.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Transform test fixtures
// ---------------------------------------------------------------------------

// transformSourceHarness is the harness the input files were deployed for.
var transformSourceHarness = domain.HarnessRef{
	ID:          "source-harness",
	DisplayName: "Source Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// transformTargetHarness is the harness to transform files into.
var transformTargetHarness = domain.HarnessRef{
	ID:          "target-harness",
	DisplayName: "Target Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// newTransformSourceModule returns a stubHarnessModule for the source harness with a
// model key that distinguishes it from the target harness.
func newTransformSourceModule() *stubHarnessModule {
	return &stubHarnessModule{
		ref: transformSourceHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "source-harness",
			DisplayName: "Source Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "src_model",
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".src.md",
			},
		},
	}
}

// newTransformTargetModule returns a stubHarnessModule for the target harness. Its model
// key ("tgt_model") is unknown to the source harness, yielding HarnessMatchNo for source
// files that carry it. Paths.Agents is configured so ResolveTargetPath can compute a
// non-empty destination.
func newTransformTargetModule() *stubHarnessModule {
	return &stubHarnessModule{
		ref: transformTargetHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "target-harness",
			DisplayName: "Target Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "tgt_model",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   "agents",
				},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".tgt.md",
			},
		},
	}
}

// newTransformRegistry returns a stubRegistry backed by both transform harness modules.
func newTransformRegistry() *stubRegistry {
	srcMod := newTransformSourceModule()
	tgtMod := newTransformTargetModule()
	return &stubRegistry{
		list: []domain.HarnessRef{transformSourceHarness, transformTargetHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness": srcMod,
			"target-harness": tgtMod,
		},
	}
}

// newTransformDeps returns an app.Deps wired for transform tests — same as newBaseDeps
// but with a two-harness registry instead of the minimal single-harness one.
func newTransformDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = newTransformRegistry()
	return deps, workspace
}

// matchingAgentBytes returns the raw content of an agent file that belongs to the source
// harness. The file carries transform_version (eligibility signal 1), canonical boundary
// tags (eligibility signal 2), and the source harness's model key in frontmatter (positive
// identification evidence for the source harness field classifier).
func matchingAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"src_model: claude-3-sonnet\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the Source Harness Agent.\n" +
		"[[/SECTION:Identity]]\n")
}

// mismatchAgentBytes returns content that is a valid deployed agent (has transform_version
// and boundary tags) but belongs to the target harness. The target harness's model key
// ("tgt_model") is ClassUnknown to the source harness field classifier, yielding
// HarnessMatchNo when DetectHarnessMatch is called with the source harness module.
func mismatchAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"tgt_model: gpt-4\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are the Target Harness Agent.\n" +
		"[[/SECTION:Identity]]\n")
}

// nonAgentBytes returns content that is not a deployed MOSAIC agent: it lacks both
// transform_version and canonical boundary tags, so eligibleHarnessOnly returns false and
// DetectHarnessMatch returns HarnessMatchNotAgent.
func nonAgentBytes() []byte {
	return []byte("# Some Plain Markdown\n\nThis is not a deployed MOSAIC agent.\n")
}

// writeTransformFile writes content to a file named name inside dir and returns its path.
func writeTransformFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeTransformFile mkdir: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeTransformFile: %v", err)
	}
	return p
}

// newPreAnsweredTransformRequest returns a TransformHarnessRequest with all interactive
// fields pre-answered, so no questions are asked through the Interaction port during the
// service call.
func newPreAnsweredTransformRequest(path string) app.TransformHarnessRequest {
	return app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: transformTargetHarness.ID,
		Path:            path,
		TargetModel:     "target-model-x",
		Overwrite:       false,
		DryRun:          false,
	}
}

// ---------------------------------------------------------------------------
// T6.1: Single-file transform flow
// ---------------------------------------------------------------------------

// TestTransformHarness_SingleFile_MatchingSource_ProducesTransformedOutcome verifies
// the happy-path service flow for a single matching agent file. The result must contain
// exactly one file outcome with StatusTransformed and a non-empty DestinationPath.
func TestTransformHarness_SingleFile_MatchingSource_ProducesTransformedOutcome(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// Act
	// TDD RED: TransformHarness currently returns errTransformNotImplemented.
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned unexpected error: %v\n(RED: stub not yet replaced by I6.1)", err)
	}

	// Assert: result fields echoed correctly.
	if result.SourceHarnessID != req.SourceHarnessID {
		t.Errorf("SourceHarnessID = %q, want %q", result.SourceHarnessID, req.SourceHarnessID)
	}
	if result.TargetHarnessID != req.TargetHarnessID {
		t.Errorf("TargetHarnessID = %q, want %q", result.TargetHarnessID, req.TargetHarnessID)
	}
	if result.InputIsDirectory {
		t.Error("InputIsDirectory = true for single-file input; want false")
	}

	// Assert: exactly one file outcome.
	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Errorf("Files[0].Status = %q, want StatusTransformed", out.Status)
	}
	if out.DestinationPath == "" {
		t.Error("Files[0].DestinationPath is empty; want a non-empty resolved target path")
	}
	if out.SourcePath != agentPath {
		t.Errorf("Files[0].SourcePath = %q, want %q", out.SourcePath, agentPath)
	}

	// Assert: count tallies match.
	if result.Transformed != 1 {
		t.Errorf("Transformed count = %d, want 1", result.Transformed)
	}
	if result.SkippedMismatch != 0 || result.SkippedNotAgent != 0 || result.Failed != 0 {
		t.Errorf("unexpected non-zero skip/fail counts: mismatch=%d notAgent=%d failed=%d",
			result.SkippedMismatch, result.SkippedNotAgent, result.Failed)
	}
}

// TestTransformHarness_SingleFile_MatchingSource_NoQuestionsAsked verifies that when all
// request fields are pre-answered, no interactive questions are routed through the
// Interaction port.
func TestTransformHarness_SingleFile_MatchingSource_NoQuestionsAsked(t *testing.T) {
	// Arrange: an interaction stub that records every question asked. Any unexpected
	// question call causes the stub to fail the test.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: no questions asked through the interaction port.
	if len(stub.QuestionOrder()) != 0 {
		t.Errorf("unexpected questions asked: %v; want none (all fields pre-answered)", stub.QuestionOrder())
	}
}

// TestTransformHarness_DryRun_NoFileWritten verifies that DryRun=true produces a
// StatusTransformed outcome but writes nothing to the destination path.
func TestTransformHarness_DryRun_NoFileWritten(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness dry-run returned error: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Errorf("DryRun Files[0].Status = %q, want StatusTransformed", out.Status)
	}
	if !result.DryRun {
		t.Error("result.DryRun = false; want true")
	}

	// Assert: destination file was not written.
	if out.DestinationPath != "" {
		if _, statErr := os.Stat(out.DestinationPath); statErr == nil {
			t.Errorf("destination file exists at %q after dry run; want no file written", out.DestinationPath)
		}
	}
}

// ---------------------------------------------------------------------------
// T6.2: Batch directory input
// ---------------------------------------------------------------------------

// TestTransformHarness_DirectoryInput_ThreeFileTypes_ProducesThreeOutcomes verifies that
// a directory containing a matching file, a mismatching file, and a non-agent file yields
// three per-file outcomes with the correct statuses.
//
// Per the directory enumeration contract (ContractsDesign.md): only files whose name ends
// with the source harness's agent extension (".src.md") are enumerated. Non-matching-extension
// files are silently excluded and produce no outcome entry. Therefore:
//   - "c-not-agent.src.md" has the matching extension but non-agent content → StatusSkippedNotAgent
//   - A ".txt" file would be silently excluded → never appears in Files
//
// Note: the previously-named "c-plain.txt" fixture was renamed to "c-not-agent.src.md" to
// match the enumeration contract. See TestTransformHarness_DirectoryInput_NonMatchingExtension_ExcludedFromFiles
// for the complementary test asserting that ".txt" files produce no outcome entry at all.
func TestTransformHarness_DirectoryInput_ThreeFileTypes_ProducesThreeOutcomes(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	batchDir := t.TempDir()
	// Files named so enumeration order is predictable (lexicographic).
	writeTransformFile(t, batchDir, "a-matching.src.md", matchingAgentBytes())
	writeTransformFile(t, batchDir, "b-mismatch.src.md", mismatchAgentBytes())
	// "c-not-agent.src.md" has the source harness's extension but non-agent content.
	// DetectHarnessMatch returns HarnessMatchNotAgent → StatusSkippedNotAgent.
	writeTransformFile(t, batchDir, "c-not-agent.src.md", nonAgentBytes())

	req := newPreAnsweredTransformRequest(batchDir)

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness batch returned error: %v", err)
	}

	// Assert: directory flag set.
	if !result.InputIsDirectory {
		t.Error("InputIsDirectory = false for directory input; want true")
	}

	// Assert: three file outcomes.
	if len(result.Files) != 3 {
		t.Fatalf("len(Files) = %d, want 3; all files in directory must produce an outcome", len(result.Files))
	}

	// Assert: statuses by file (enumeration is lexicographic by filename).
	if result.Files[0].Status != app.StatusTransformed {
		t.Errorf("Files[0] (matching): Status = %q, want StatusTransformed", result.Files[0].Status)
	}
	if result.Files[1].Status != app.StatusSkippedMismatch {
		t.Errorf("Files[1] (mismatch): Status = %q, want StatusSkippedMismatch", result.Files[1].Status)
	}
	if result.Files[2].Status != app.StatusSkippedNotAgent {
		t.Errorf("Files[2] (non-agent): Status = %q, want StatusSkippedNotAgent", result.Files[2].Status)
	}

	// Assert: count tallies match outcome list.
	if result.Transformed != 1 {
		t.Errorf("Transformed count = %d, want 1", result.Transformed)
	}
	if result.SkippedMismatch != 1 {
		t.Errorf("SkippedMismatch count = %d, want 1", result.SkippedMismatch)
	}
	if result.SkippedNotAgent != 1 {
		t.Errorf("SkippedNotAgent count = %d, want 1", result.SkippedNotAgent)
	}
	if result.Failed != 0 {
		t.Errorf("Failed count = %d, want 0", result.Failed)
	}
}

// TestTransformHarness_DirectoryInput_MismatchDoesNotStopBatch verifies that a mismatch
// on one file does not prevent the remaining files from being processed. Per-file
// independence is the contract: every file in the directory produces an outcome.
func TestTransformHarness_DirectoryInput_MismatchDoesNotStopBatch(t *testing.T) {
	// Arrange: mismatch file first, then a matching file. The batch must continue
	// after the mismatch and produce a StatusTransformed for the second file.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-mismatch.src.md", mismatchAgentBytes())
	writeTransformFile(t, batchDir, "b-matching.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(batchDir)

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned whole-run error: %v; want nil (mismatch is per-file, not whole-run)", err)
	}

	if len(result.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(result.Files))
	}

	// Both files must have outcomes.
	if result.Files[0].Status != app.StatusSkippedMismatch {
		t.Errorf("Files[0] (mismatch): Status = %q, want StatusSkippedMismatch", result.Files[0].Status)
	}
	if result.Files[1].Status != app.StatusTransformed {
		t.Errorf("Files[1] (matching): Status = %q, want StatusTransformed; "+
			"batch must continue after a mismatch", result.Files[1].Status)
	}
}

// ---------------------------------------------------------------------------
// T6.3: Overwrite protection
// ---------------------------------------------------------------------------

// TestTransformHarness_ExistingDestination_WithoutOverwrite_ProducesStatusFailed verifies
// that when the destination file already exists and Overwrite=false, the per-file outcome
// is StatusFailed with a reason mentioning the destination path.
func TestTransformHarness_ExistingDestination_WithoutOverwrite_ProducesStatusFailed(t *testing.T) {
	// Arrange: write the source file and pre-create the destination.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// First call (DryRun) to discover the destination path, then pre-create it.
	dryReq := req
	dryReq.DryRun = true
	dryResult, dryErr := svc.TransformHarness(context.Background(), dryReq)
	if dryErr != nil {
		t.Skipf("skipping overwrite test: dry run failed (implement I6.1 first): %v", dryErr)
	}
	if len(dryResult.Files) == 0 || dryResult.Files[0].DestinationPath == "" {
		t.Skip("skipping overwrite test: destination path unknown without I6.1")
	}

	destPath := dryResult.Files[0].DestinationPath
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("existing content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile (pre-create dest): %v", err)
	}

	req.Overwrite = false

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected whole-run error: %v; overwrite refusal must be per-file, not whole-run", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}

	out := result.Files[0]
	if out.Status != app.StatusFailed {
		t.Errorf("Files[0].Status = %q, want StatusFailed when destination exists and Overwrite=false", out.Status)
	}
	if !strings.Contains(out.Reason, destPath) {
		t.Errorf("Reason = %q; want it to mention the destination path %q", out.Reason, destPath)
	}
	if result.Failed != 1 {
		t.Errorf("Failed count = %d, want 1", result.Failed)
	}

	// Destination must remain unchanged.
	got, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("ReadFile dest: %v", readErr)
	}
	if string(got) != "existing content\n" {
		t.Error("destination file was overwritten despite Overwrite=false; want byte-identical to original")
	}
}

// TestTransformHarness_ExistingDestination_WithOverwrite_Replaces verifies that when the
// destination already exists and Overwrite=true, the file is replaced and the outcome is
// StatusTransformed.
func TestTransformHarness_ExistingDestination_WithOverwrite_Replaces(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// Discover destination via dry run.
	dryReq := req
	dryReq.DryRun = true
	dryResult, dryErr := svc.TransformHarness(context.Background(), dryReq)
	if dryErr != nil {
		t.Skipf("skipping overwrite test: dry run failed (implement I6.1 first): %v", dryErr)
	}
	if len(dryResult.Files) == 0 || dryResult.Files[0].DestinationPath == "" {
		t.Skip("skipping overwrite test: destination path unknown without I6.1")
	}

	destPath := dryResult.Files[0].DestinationPath
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile (pre-create dest): %v", err)
	}

	req.Overwrite = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness with Overwrite=true returned error: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}

	if result.Files[0].Status != app.StatusTransformed {
		t.Errorf("Files[0].Status = %q, want StatusTransformed when Overwrite=true", result.Files[0].Status)
	}

	// Destination must have been replaced (content differs from the old sentinel).
	got, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("ReadFile dest after overwrite: %v", readErr)
	}
	if string(got) == "old content\n" {
		t.Error("destination file still has old content after Overwrite=true; want it replaced")
	}
}

// TestTransformHarness_DryRun_ExistingDestination_ProducesStatusFailed verifies that
// DryRun=true does NOT suppress the overwrite-protection check. When the destination file
// already exists and Overwrite=false, the per-file outcome is StatusFailed even in dry-run
// mode. Only the actual write is suppressed by DryRun — the existence check and its reported
// failure are not.
//
// This is a regression test for a fix that removed `&& !req.DryRun` from the overwrite-
// protection condition so that a dry-run against a pre-existing destination correctly previews
// the StatusFailed outcome a real run would produce (per the DryRun doc comment: "computes and
// reports every outcome").
func TestTransformHarness_DryRun_ExistingDestination_ProducesStatusFailed(t *testing.T) {
	// Arrange: create the source agent file; discover the destination path via a first
	// dry-run performed before the destination exists, so that discovery run succeeds.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// Discovery dry-run: destination does not yet exist, so this produces StatusTransformed
	// and reveals the resolved destination path.
	discoverReq := req
	discoverReq.DryRun = true
	discoverResult, discoverErr := svc.TransformHarness(context.Background(), discoverReq)
	if discoverErr != nil {
		t.Skipf("skipping dry-run overwrite regression: discovery run failed (implement I6.1 first): %v", discoverErr)
	}
	if len(discoverResult.Files) == 0 || discoverResult.Files[0].DestinationPath == "" {
		t.Skip("skipping dry-run overwrite regression: destination path unknown without I6.1")
	}

	destPath := discoverResult.Files[0].DestinationPath

	// Pre-create the destination file so that the overwrite-protection check fires on the next call.
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("existing content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile (pre-create dest): %v", err)
	}

	// Act: dry-run with Overwrite=false against the now-existing destination.
	req.DryRun = true
	req.Overwrite = false
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected whole-run error: %v; overwrite refusal must be per-file, not whole-run", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}

	out := result.Files[0]

	// Assert: StatusFailed — overwrite check runs under dry-run mode too.
	if out.Status != app.StatusFailed {
		t.Errorf("DryRun Files[0].Status = %q, want StatusFailed when destination exists and Overwrite=false; "+
			"DryRun suppresses the write but must not suppress the existence check", out.Status)
	}
	if result.Failed != 1 {
		t.Errorf("Failed count = %d, want 1", result.Failed)
	}
	if result.Transformed != 0 {
		t.Errorf("Transformed count = %d, want 0 (overwrite refused)", result.Transformed)
	}

	// Assert: the existing destination file is unchanged — dry-run wrote nothing.
	got, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("ReadFile dest: %v", readErr)
	}
	if string(got) != "existing content\n" {
		t.Error("destination file was modified by dry-run with Overwrite=false; want byte-identical to original")
	}
}

// ---------------------------------------------------------------------------
// T6.4: Interactive question routing
// ---------------------------------------------------------------------------

// TestTransformHarness_EmptyTargetHarness_AsksQTransformTargetHarness verifies that when
// TargetHarnessID is empty, the service asks QTransformTargetHarness through the
// Interaction port with an option for the target harness.
func TestTransformHarness_EmptyTargetHarness_AsksQTransformTargetHarness(t *testing.T) {
	// Arrange: pre-answer QTransformTargetHarness with the target harness ID.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTransformTargetHarness, "", transformTargetHarness.ID).
		Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = "" // unset → must trigger question

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: QTransformTargetHarness was asked.
	if !containsQuestion(stub.QuestionOrder(), domain.QTransformTargetHarness) {
		t.Errorf("QTransformTargetHarness was not asked; question order: %v; "+
			"want QTransformTargetHarness asked when TargetHarnessID is empty", stub.QuestionOrder())
	}
}

// TestTransformHarness_PreAnsweredTargetHarness_SkipsQTransformTargetHarness verifies that
// when TargetHarnessID is non-empty, QTransformTargetHarness is NOT asked.
//
// NOT RED-VERIFIED: this test asserts a purely negative condition ("question was not asked").
// Before I6.3, the stub returns errTransformNotImplemented and never calls the Interaction
// port — so no questions are ever asked, making "not asked" trivially true. The test passes
// in the RED phase for the wrong reason. The implementer must verify manually during I6.3 that:
//   - TestTransformHarness_EmptyTargetHarness_AsksQTransformTargetHarness correctly fails in RED
//     (that test does fail correctly today)
//   - This test remains GREEN after I6.3 because a pre-answered TargetHarnessID skips the question
func TestTransformHarness_PreAnsweredTargetHarness_SkipsQTransformTargetHarness(t *testing.T) {
	// Arrange: no answer wired for QTransformTargetHarness — the stub must not be asked.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	// TargetHarnessID is already set by newPreAnsweredTransformRequest.

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: QTransformTargetHarness was not asked.
	if containsQuestion(stub.QuestionOrder(), domain.QTransformTargetHarness) {
		t.Errorf("QTransformTargetHarness was asked despite TargetHarnessID being pre-answered; "+
			"question order: %v", stub.QuestionOrder())
	}
}

// TestTransformHarness_EmptyTargetModel_AsksQTransformTargetModel verifies that when
// TargetModel is empty, the service asks QTransformTargetModel through the Interaction port.
func TestTransformHarness_EmptyTargetModel_AsksQTransformTargetModel(t *testing.T) {
	// Arrange: pre-answer QTransformTargetModel with a model ID.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTransformTargetModel, "", "model-a").
		Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetModel = "" // unset → must trigger question

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: QTransformTargetModel was asked.
	if !containsQuestion(stub.QuestionOrder(), domain.QTransformTargetModel) {
		t.Errorf("QTransformTargetModel was not asked; question order: %v; "+
			"want QTransformTargetModel asked when TargetModel is empty", stub.QuestionOrder())
	}
}

// TestTransformHarness_PreAnsweredTargetModel_SkipsQTransformTargetModel verifies that
// when TargetModel is non-empty, QTransformTargetModel is NOT asked.
//
// NOT RED-VERIFIED: same purely-negative assertion issue as SkipsQTransformTargetHarness above.
// Before I6.3, no questions are ever asked (stub returns errTransformNotImplemented immediately),
// so "not asked" is trivially satisfied. Re-verify this test manually once I6.3 is delivered
// alongside TestTransformHarness_EmptyTargetModel_AsksQTransformTargetModel.
func TestTransformHarness_PreAnsweredTargetModel_SkipsQTransformTargetModel(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	// TargetModel is already set.

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: QTransformTargetModel was not asked.
	if containsQuestion(stub.QuestionOrder(), domain.QTransformTargetModel) {
		t.Errorf("QTransformTargetModel was asked despite TargetModel being pre-answered; "+
			"question order: %v", stub.QuestionOrder())
	}
}

// TestTransformHarness_CancelledTargetHarnessQuestion_ReturnsError verifies that
// cancellation of QTransformTargetHarness causes TransformHarness to return a non-nil
// error and write no files.
//
// NOT RED-VERIFIED (partially): The assertion "err != nil" and "zero counts" pass today for
// the wrong reason — the stub returns errTransformNotImplemented (err != nil, zero result).
// The "QTransformTargetHarness was asked" assertion below DOES fail correctly today (stub
// never calls Interaction), providing genuine RED-phase protection for the question-routing
// path. Once I6.3 is delivered and the stub is replaced, ALL three assertions must hold:
//   - QTransformTargetHarness IS asked (service calls Interaction with empty TargetHarnessID)
//   - err != nil (cancellation causes whole-run abort)
//   - zero file counts (aborted run produces no per-file outcomes)
func TestTransformHarness_CancelledTargetHarnessQuestion_ReturnsError(t *testing.T) {
	// Arrange: interaction stub configured to cancel QTransformTargetHarness.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QTransformTargetHarness, "").
		Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = "" // unset to trigger the question

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: QTransformTargetHarness must have been asked before it could be cancelled.
	// This assertion fails correctly today (stub implementation never calls Interaction port).
	// Once I6.3 is delivered, the service will ask QTransformTargetHarness and the stub will
	// return Cancelled, satisfying this assertion.
	if !containsQuestion(stub.QuestionOrder(), domain.QTransformTargetHarness) {
		t.Error("QTransformTargetHarness was not asked; the service must invoke the Interaction port " +
			"when TargetHarnessID is empty — cancellation can only occur after the question is asked")
	}

	// Assert: error returned.
	if err == nil {
		t.Error("TransformHarness returned nil error after QTransformTargetHarness cancelled; want non-nil error")
	}

	// Assert: no file outcomes when the whole run is aborted.
	if result.Transformed > 0 || result.Failed > 0 {
		t.Errorf("result has outcomes (transformed=%d failed=%d) after cancellation; want zero counts",
			result.Transformed, result.Failed)
	}
}

// TestTransformHarness_SameSourceAndTargetHarness_ReturnsError verifies that requesting a
// transform where source and target harness are the same returns a whole-run error wrapping
// ErrTransformSameHarness, without calling any interactive questions.
func TestTransformHarness_SameSourceAndTargetHarness_ReturnsError(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = req.SourceHarnessID // same → must be rejected

	// Act
	_, err := svc.TransformHarness(context.Background(), req)

	// Assert: error wrapping ErrTransformSameHarness.
	if err == nil {
		t.Fatal("TransformHarness returned nil error when source == target harness; want error wrapping ErrTransformSameHarness")
	}
	if !errors.Is(err, app.ErrTransformSameHarness) {
		t.Errorf("error = %v; want errors.Is(err, ErrTransformSameHarness) = true", err)
	}

	// No interactive questions should have been asked.
	if len(stub.QuestionOrder()) != 0 {
		t.Errorf("questions asked: %v; want none when same-harness is detected up front", stub.QuestionOrder())
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// containsQuestion reports whether questions contains id.
func containsQuestion(questions []domain.QuestionID, id domain.QuestionID) bool {
	for _, q := range questions {
		if q == id {
			return true
		}
	}
	return false
}

// indeterminateAgentBytes returns content that is a valid deployed agent (has transform_version
// and canonical boundary tags) but carries no harness-distinguishing frontmatter field (no
// src_model, no tgt_model). DetectHarnessMatch with the source harness module should return
// HarnessMatchIndeterminate — the conservative verdict that proceeds with a warning rather than
// blocking a potentially legitimate file.
func indeterminateAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		// No src_model or tgt_model field — no harness-distinguishing signal in frontmatter.
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are an agent with no harness-distinguishing frontmatter field.\n" +
		"[[/SECTION:Identity]]\n")
}

// ---------------------------------------------------------------------------
// T6.2 supplementary: Directory enumeration edge cases
// ---------------------------------------------------------------------------

// TestTransformHarness_DirectoryInput_NonMatchingExtension_ExcludedFromFiles verifies the
// directory enumeration contract: a file whose name does not end with the source harness's
// agent extension (".src.md") is silently excluded from the result and does not produce any
// outcome entry. A ".txt" file must NOT appear in result.Files at all — not even as
// StatusSkippedNotAgent — and must not increment any count.
//
// This is the complementary test to TestTransformHarness_DirectoryInput_ThreeFileTypes_ProducesThreeOutcomes:
// that test verifies that a file WITH the matching extension but non-agent content yields
// StatusSkippedNotAgent; this test verifies that a file WITHOUT the matching extension is
// silently excluded from enumeration.
func TestTransformHarness_DirectoryInput_NonMatchingExtension_ExcludedFromFiles(t *testing.T) {
	// Arrange: one matching file (produces an outcome) and one .txt file (must be excluded).
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-matching.src.md", matchingAgentBytes())
	writeTransformFile(t, batchDir, "b-plain.txt", nonAgentBytes()) // non-matching extension

	req := newPreAnsweredTransformRequest(batchDir)

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: exactly one file outcome (the matching .src.md file only).
	// The .txt file must NOT appear in result.Files.
	if len(result.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1; a .txt file must be silently excluded from directory "+
			"enumeration (not enumerated at all), not reported as StatusSkippedNotAgent. "+
			"Directory enumeration includes only files ending with the source extension (.src.md).",
			len(result.Files))
		for i, f := range result.Files {
			t.Logf("  Files[%d]: status=%q path=%q", i, f.Status, f.SourcePath)
		}
	}

	// SkippedNotAgent count must be 0: .txt files are not enumerated, not skipped.
	if result.SkippedNotAgent != 0 {
		t.Errorf("SkippedNotAgent = %d, want 0; non-matching-extension files are silently "+
			"excluded during enumeration and must not increment any count", result.SkippedNotAgent)
	}
}

// TestTransformHarness_DirectoryInput_Subdirectory_Ignored verifies that subdirectories
// inside the batch directory are silently skipped (non-recursive enumeration). A subdirectory
// must not appear in result.Files and must not increment any count.
func TestTransformHarness_DirectoryInput_Subdirectory_Ignored(t *testing.T) {
	// Arrange: one matching file and one subdirectory containing a matching file.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-matching.src.md", matchingAgentBytes())
	// Create a subdirectory with its own matching file — must NOT be recursed into.
	subDir := filepath.Join(batchDir, "subdir")
	writeTransformFile(t, subDir, "nested.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(batchDir)

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: exactly one file outcome (the top-level matching file only).
	// Subdirectories are enumerated non-recursively: the subdir itself and its contents
	// must produce no outcome entry.
	if len(result.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1; directory enumeration must be non-recursive. "+
			"The subdirectory 'subdir' and its contents must be silently ignored.", len(result.Files))
		for i, f := range result.Files {
			t.Logf("  Files[%d]: status=%q path=%q", i, f.Status, f.SourcePath)
		}
	}

	if result.Transformed != 1 {
		t.Errorf("Transformed count = %d, want 1 (only the top-level file)", result.Transformed)
	}
}

// ---------------------------------------------------------------------------
// T6.1 supplementary: HarnessMatchIndeterminate produces a warning
// ---------------------------------------------------------------------------

// TestTransformHarness_HarnessMatchIndeterminate_ProducesWarningWithStatusTransformed verifies
// the HarnessMatchIndeterminate consumption contract (ContractsDesign.md lines 736-744):
// when DetectHarnessMatch returns HarnessMatchIndeterminate, the file is transformed
// (StatusTransformed) and TransformFileOutcome.Warning is non-empty. The indeterminate verdict
// must not block a legitimate file — it proceeds conservatively with a warning.
func TestTransformHarness_HarnessMatchIndeterminate_ProducesWarningWithStatusTransformed(t *testing.T) {
	// Arrange: an agent file that has eligibility signals (transform_version + boundary tags)
	// but no harness-specific frontmatter field — yields HarnessMatchIndeterminate.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "indeterminate-agent.src.md", indeterminateAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// Act
	// TDD RED: stub returns errTransformNotImplemented → err != nil → t.Fatalf below.
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned unexpected error: %v\n"+
			"(RED: HarnessMatchIndeterminate must not cause a whole-run error; "+
			"deliver I6.2 to replace the stub with real detection and transform logic)", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1 (one indeterminate file produces exactly one outcome)", len(result.Files))
	}

	out := result.Files[0]

	// HarnessMatchIndeterminate → must still produce StatusTransformed (not StatusSkippedMismatch).
	if out.Status != app.StatusTransformed {
		t.Errorf("Files[0].Status = %q for HarnessMatchIndeterminate input; want StatusTransformed; "+
			"an indeterminate verdict proceeds with the transform rather than skipping the file",
			out.Status)
	}

	// Warning must be non-empty: the reason for the indeterminate verdict must be surfaced.
	if out.Warning == "" {
		t.Error("Files[0].Warning is empty for HarnessMatchIndeterminate input; " +
			"want a non-empty warning explaining why the harness could not be positively identified; " +
			"Warning carries the verdict Reason (ContractsDesign.md line 740)")
	}

	// DestinationPath must be set (file was transformed, not skipped).
	if out.DestinationPath == "" {
		t.Error("Files[0].DestinationPath is empty for HarnessMatchIndeterminate; " +
			"want the resolved target path (file was transformed, not skipped)")
	}

	if result.Transformed != 1 {
		t.Errorf("Transformed count = %d, want 1 (indeterminate counts as transformed)", result.Transformed)
	}
}

// ---------------------------------------------------------------------------
// Whole-run error cases (ContractsDesign.md Error Handling Rule #2)
// ---------------------------------------------------------------------------

// TestTransformHarness_UnresolvableTargetHarness_ReturnsWholeRunError verifies that when the
// target harness ID cannot be resolved by the registry (harness not found), TransformHarness
// returns a whole-run error identifying the unresolvable harness. No file outcomes are
// produced because the run cannot proceed without a target harness module.
func TestTransformHarness_UnresolvableTargetHarness_ReturnsWholeRunError(t *testing.T) {
	// Arrange: registry has source harness but not the target harness.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{transformSourceHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness": newTransformSourceModule(),
			// "target-harness" deliberately absent → Resolve("target-harness") errors.
		},
	}
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())
	req := newPreAnsweredTransformRequest(agentPath)
	// TargetHarnessID is "target-harness" — not in the registry above.

	// Act
	_, err := svc.TransformHarness(context.Background(), req)

	// Assert: whole-run error must be returned.
	if err == nil {
		t.Fatal("TransformHarness returned nil error for unresolvable target harness; want a whole-run error")
	}
	// The error must mention the unresolvable harness ID so the user can identify the problem.
	// (errTransformNotImplemented does NOT mention "target-harness", so this assertion correctly
	// fails in the RED phase before I6.1 replaces the stub.)
	if !strings.Contains(err.Error(), "target-harness") {
		t.Errorf("error = %q; want it to identify the unresolvable harness ID 'target-harness'", err.Error())
	}
}

// TestTransformHarness_UnresolvableSourceHarness_ReturnsWholeRunError verifies that when the
// source harness ID cannot be resolved by the registry, TransformHarness returns a whole-run
// error identifying the unresolvable harness.
func TestTransformHarness_UnresolvableSourceHarness_ReturnsWholeRunError(t *testing.T) {
	// Arrange: registry has target harness but not the source harness.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{transformTargetHarness},
		modules: map[string]domain.HarnessModule{
			"target-harness": newTransformTargetModule(),
			// "source-harness" deliberately absent → Resolve("source-harness") errors.
		},
	}
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())
	req := newPreAnsweredTransformRequest(agentPath)
	// SourceHarnessID is "source-harness" — not in the registry above.

	// Act
	_, err := svc.TransformHarness(context.Background(), req)

	// Assert: whole-run error must be returned.
	if err == nil {
		t.Fatal("TransformHarness returned nil error for unresolvable source harness; want a whole-run error")
	}
	// The error must mention the unresolvable harness ID.
	// (errTransformNotImplemented does NOT mention "source-harness", so this assertion correctly
	// fails in the RED phase before I6.1 replaces the stub.)
	if !strings.Contains(err.Error(), "source-harness") {
		t.Errorf("error = %q; want it to identify the unresolvable harness ID 'source-harness'", err.Error())
	}
}

// TestTransformHarness_UnreadableInputPath_ReturnsWholeRunError verifies that when the
// input path does not exist (unreadable), TransformHarness returns a whole-run error
// identifying the problematic path. Per ContractsDesign.md Error Handling Rule #2, an
// unreadable input path is a whole-run failure, not a per-file failure.
func TestTransformHarness_UnreadableInputPath_ReturnsWholeRunError(t *testing.T) {
	// Arrange: path that does not exist on disk.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	nonexistentPath := filepath.Join(t.TempDir(), "does-not-exist", "agent.src.md")
	req := newPreAnsweredTransformRequest(nonexistentPath)

	// Act
	_, err := svc.TransformHarness(context.Background(), req)

	// Assert: whole-run error must be returned.
	if err == nil {
		t.Fatal("TransformHarness returned nil error for a non-existent input path; want a whole-run error")
	}
	// The error must mention the unreadable path so the user can identify the problem.
	// (errTransformNotImplemented does NOT contain the path, so this assertion correctly
	// fails in the RED phase before I6.1 replaces the stub.)
	if !strings.Contains(err.Error(), nonexistentPath) {
		t.Errorf("error = %q; want it to identify the unreadable input path %q", err.Error(), nonexistentPath)
	}
}

// TestTransformHarness_UserConfigLoadError_ReturnsWholeRunError verifies that a
// UserConfig.Load() failure causes TransformHarness to return a whole-run error rather than
// silently continuing on a zero-value config or surfacing the error only as a per-file outcome.
//
// This is a regression test for a fix that changed `userCfg, _ := s.deps.UserConfig.Load()`
// to check and propagate the error, matching the established convention in deploy.go and
// update.go. A malformed or unreadable user config must abort the run immediately: using a
// zero-value config would produce a silently wrong tool-mappings-version stamp, defeating the
// staleness detection that stamp is meant to support.
func TestTransformHarness_UserConfigLoadError_ReturnsWholeRunError(t *testing.T) {
	// Arrange: inject a UserConfig store that always fails on Load.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	loadErr := errors.New("user-config.yaml: parse user config: yaml: line 3: did not find expected key")
	deps.UserConfig = &spyUserConfig{loadErr: loadErr}
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: whole-run error returned.
	if err == nil {
		t.Fatal("TransformHarness returned nil error after UserConfig.Load failed; want a whole-run error")
	}
	if !errors.Is(err, loadErr) {
		t.Errorf("errors.Is(err, loadErr) = false; the Load error must propagate unmodified; got: %v", err)
	}

	// Assert: no per-file outcomes when the whole run is aborted before any file processing.
	if len(result.Files) != 0 {
		t.Errorf("len(Files) = %d, want 0; no files should be processed when UserConfig.Load fails", len(result.Files))
	}
	if result.Transformed != 0 || result.Failed != 0 {
		t.Errorf("result has counts (transformed=%d failed=%d) after UserConfig.Load failure; want all zeros",
			result.Transformed, result.Failed)
	}
}

// ---------------------------------------------------------------------------
// T6.4 supplementary: Skipped model leaves TargetModel empty
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Regression: nested Paths.Agents.Project layout (implementation-review#76 Critical #1)
// ---------------------------------------------------------------------------

// customTargetPathModule is a stubHarnessModule variant whose TargetPath method is driven
// by a configurable function. Used only by the nested-agents-path regression test, where the
// target harness must return a workspace-root-relative path that includes the target
// harness directory (e.g. ".tgt-harness/agents/my-agent.tgt.md").
type customTargetPathModule struct {
	ref          domain.HarnessRef
	descriptor   domain.HarnessDescriptor
	targetPathFn func(req domain.TargetPathRequest) (string, error)
}

func (m *customTargetPathModule) Ref() domain.HarnessRef { return m.ref }
func (m *customTargetPathModule) Descriptor() *domain.HarnessDescriptor {
	return &m.descriptor
}
func (m *customTargetPathModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *customTargetPathModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *customTargetPathModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	if m.targetPathFn != nil {
		return m.targetPathFn(req)
	}
	return req.Key + ".md", nil
}
func (m *customTargetPathModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *customTargetPathModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *customTargetPathModule) Close() error { return nil }

// TestTransformHarness_NestedAgentsPath_DestinationLandsUnderTargetRoot is a regression
// test for the destination-path derivation fix documented in implementation-review#76
// Critical #1.
//
// When a source harness declares Paths.Agents.Project (e.g. ".src-harness/agents"), the
// service must:
//  1. Recognise that the source file's directory ends with that declared suffix.
//  2. Strip the suffix to recover the workspace root.
//  3. Join the workspace root with the target module's workspace-root-relative path.
//
// Without the fix the service called:
//
//	filepath.Join(filepath.Dir(srcPath), tgtRelPath)
//
// which anchored the destination inside the source harness's own agents directory,
// producing a wrong path such as:
//
//	<workspace>/.src-harness/agents/.tgt-harness/agents/my-agent.tgt.md
//
// With the fix the service produces the correct sibling path:
//
//	<workspace>/.tgt-harness/agents/my-agent.tgt.md
//
// This test triggers the fix by using realistic harness directories and asserting the
// exact destination path, so any future regression to the old behaviour will be caught.
func TestTransformHarness_NestedAgentsPath_DestinationLandsUnderTargetRoot(t *testing.T) {
	// Arrange: a workspace containing the source file inside the declared agents directory.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)

	workspace := t.TempDir()
	srcAgentsDir := filepath.Join(workspace, ".src-harness", "agents")
	srcFilePath := writeTransformFile(t, srcAgentsDir, "my-agent.src.md", matchingAgentBytes())

	// Source harness: declares Paths.Agents.Project so the service can strip that suffix
	// from the source file's directory to recover the workspace root.
	srcModule := &stubHarnessModule{
		ref: transformSourceHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "source-harness",
			DisplayName: "Source Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "src_model",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   ".src-harness/agents",
				},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".src.md",
			},
		},
	}

	// Target harness: TargetPath returns a path relative to the workspace root, as a real
	// harness module would. The path includes the target harness directory so the assertion
	// can verify that the destination lands under the correct sibling directory.
	tgtModule := &customTargetPathModule{
		ref: transformTargetHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "target-harness",
			DisplayName: "Target Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "tgt_model",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   ".tgt-harness/agents",
				},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".tgt.md",
			},
		},
		targetPathFn: func(req domain.TargetPathRequest) (string, error) {
			// Return a workspace-root-relative path, the contract documented in
			// ContractsDesign.md for descriptor.ResolveTargetPath. The service joins
			// this with the recovered workspace root.
			return filepath.Join(".tgt-harness", "agents", req.Key+".tgt.md"), nil
		},
	}

	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{transformSourceHarness, transformTargetHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness": srcModule,
			"target-harness": tgtModule,
		},
	}
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(srcFilePath)
	req.DryRun = true // DestinationPath is still resolved and returned under DryRun=true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(result.Files))
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("Files[0].Status = %q, want StatusTransformed", out.Status)
	}
	if out.DestinationPath == "" {
		t.Fatal("Files[0].DestinationPath is empty; want the resolved target path")
	}

	// The destination must be exactly <workspace>/.tgt-harness/agents/my-agent.tgt.md.
	// "my-agent" is the agent key derived from "my-agent.src.md" by stripping ".src.md".
	wantDestPath := filepath.Join(workspace, ".tgt-harness", "agents", "my-agent.tgt.md")
	if out.DestinationPath != wantDestPath {
		t.Errorf("DestinationPath = %q\n  want %q\n\n"+
			"  If DestinationPath starts with %q it means the fix from\n"+
			"  implementation-review#76 Critical #1 has been reverted:\n"+
			"  the destination is anchored to the source harness's agents directory\n"+
			"  instead of the workspace root, nesting it incorrectly.",
			out.DestinationPath, wantDestPath,
			filepath.Join(workspace, ".src-harness"))
	}
}

// TestTransformHarness_SkippedModel_LeavesTargetModelEmpty verifies that when QTransformTargetModel
// is answered with SkippedOne (user skips model selection), the result's TargetModel is empty.
// This exercises FR-15: "a skipped or empty answer leaves the output's model field empty."
func TestTransformHarness_SkippedModel_LeavesTargetModelEmpty(t *testing.T) {
	// Arrange: TargetModel empty triggers QTransformTargetModel; stub returns SkippedOne by default.
	stub := interactiontest.NewBuilder().Build() // no scripted QTransformTargetModel answer → SkippedOne
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetModel = "" // unset → triggers QTransformTargetModel → stub returns SkippedOne

	// Act
	// TDD RED: stub returns errTransformNotImplemented → err != nil → t.Fatalf below.
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error with skipped model: %v\n"+
			"(RED: deliver I6.3 to replace the stub with real question routing)", err)
	}

	// Assert: TargetModel must be empty when the user skipped model selection.
	if result.TargetModel != "" {
		t.Errorf("TargetModel = %q after skipped model question; want empty string; "+
			"FR-15: a skipped model answer must leave the output's model field empty", result.TargetModel)
	}
}
