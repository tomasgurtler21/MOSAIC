package logscan_test

// Tests for the logscan package.
//
// Coverage:
//
//   Default — default-location probing:
//   - An OrchestrationLogs/ folder under workDir is found and classified as SourceLogsRoot.
//   - Default returns the path to the OrchestrationLogs/ directory when found.
//   - When OrchestrationLogs/ is absent, Default returns SourceNotFound.
//   - When workDir itself does not exist, Default returns SourceNotFound without panicking.
//
//   Classify — path classification:
//   - A path containing a run-id-shaped subfolder is classified as SourceLogsRoot.
//   - A path containing an unknown-run/ subfolder is classified as SourceLogsRoot.
//   - A path containing 00_orchestrator_events.jsonl is classified as SourceSingleRun.
//   - A path containing an agent-instance folder with 03_events.jsonl is classified as SourceSingleRun.
//   - A path that does not exist is classified as SourceNotFound.
//   - An existing path matching no recognised structure is classified as SourceUnusable with a Reason.
//   - A path that is itself the unknown-run folder (with run-folder structure) is SourceSingleRun.
//   - The Source.Path returned by Classify matches the supplied path.
//
//   Enumerate — run enumeration:
//   - Run folders with run-id-shaped names are enumerated with the correct RunRef.
//   - unknown-run/ is enumerated as the unattributable bucket, not as a named run.
//   - unknown-run/ with an orchestrator file and an agent folder has OrchestratorFile/Agents populated (same code path as named runs).
//   - Plain files in the logs root are silently ignored and produce no finding.
//   - Subdirectories with names that are neither run-id-shaped nor unknown-run yield FindingUnrecognisedFolder.
//   - Near-miss run-id folder names (wrong hex length, lowercase 't', missing 'Z') produce FindingUnrecognisedFolder.
//   - Unrecognised folders are excluded from Runs and do not appear as runs.
//   - Named runs are sorted by run_id ascending.
//   - A non-usable source (SourceUnusable) yields an empty Inventory with the source's Path and Reason propagated unchanged.
//   - RunEntry.Dir is the absolute path to the run folder.
//   - A completely empty run-id-shaped folder (no orchestrator file, no agents) is still enumerated as a bare RunEntry.
//
//   Enumerate — agent-instance and event-file enumeration within a run:
//   - A run with only an orchestrator file has an empty Agents slice.
//   - OrchestratorFile is set when 00_orchestrator_events.jsonl is present.
//   - OrchestratorFile is "" when the orchestrator file is absent.
//   - AgentEntry.EventFile is set when 03_events.jsonl is present in the agent folder.
//   - AgentEntry.EventFile is "" when no event file is present, and FindingMissingEventFile is raised.
//   - Multiple agent folders are sorted by Dir.
//   - AgentEntry.InstanceHint is derived from the folder name.
//   - Event files with invalid content do not cause Enumerate to fail (contents are never read).
//
//   Classify — additional input variants:
//   - A path that is an existing plain file (not a directory) is classified as SourceUnusable.
//
//   InstanceHintFromDir — folder name to instance hint mapping:
//   - A non-empty folder name produces a non-empty AgentInstanceID hint.
//   - A folder name that already matches the AgentInstanceID format round-trips unchanged.
//
//   Empty-but-valid source:
//   - A usable logs root with zero runs yields Inventory.IsEmpty() == true.
//   - The empty outcome has Kind == SourceLogsRoot, not SourceNotFound.
//   - An empty logs root produces no findings.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/logscan"
)

// ---- constants ----

const (
	validRunID1 = "20260101T000000Z-aaaa"
	validRunID2 = "20260102T000000Z-bbbb"
	validRunID3 = "20260103T000000Z-cccc"
)

// ---- fixture helpers ----

// makeLogsRoot creates an OrchestrationLogs/ folder inside workDir and returns
// both workDir and the OrchestrationLogs path.
func makeLogsRoot(t *testing.T) (workDir, logsRoot string) {
	t.Helper()
	workDir = t.TempDir()
	logsRoot = filepath.Join(workDir, logscan.LogsDirName)
	if err := os.MkdirAll(logsRoot, 0700); err != nil {
		t.Fatalf("makeLogsRoot: %v", err)
	}
	return workDir, logsRoot
}

// makeRunFolder creates a run subfolder with the given run_id inside root and
// returns its absolute path.
func makeRunFolder(t *testing.T, root, runID string) string {
	t.Helper()
	path := filepath.Join(root, runID)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("makeRunFolder: %v", err)
	}
	return path
}

// makeOrchestratorFile creates the 00_orchestrator_events.jsonl file inside
// runDir (empty, for structural tests) and returns its absolute path.
func makeOrchestratorFile(t *testing.T, runDir string) string {
	t.Helper()
	path := filepath.Join(runDir, logscan.OrchestratorEventFile)
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatalf("makeOrchestratorFile: %v", err)
	}
	return path
}

// makeAgentFolder creates an agent-instance subfolder with the given name inside
// runDir and returns its absolute path.
func makeAgentFolder(t *testing.T, runDir, name string) string {
	t.Helper()
	path := filepath.Join(runDir, name)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("makeAgentFolder: %v", err)
	}
	return path
}

// makeAgentEventFile creates the 03_events.jsonl file inside agentDir (empty,
// for structural tests) and returns its absolute path.
func makeAgentEventFile(t *testing.T, agentDir string) string {
	t.Helper()
	path := filepath.Join(agentDir, logscan.AgentEventFile)
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatalf("makeAgentEventFile: %v", err)
	}
	return path
}

// ---- tests: Default — default-location probing ----

func TestDefault_ExistingOrchestrationLogs_ReturnsLogsRoot(t *testing.T) {
	// When OrchestrationLogs/ is present under workDir, Default must return
	// Source{Kind: SourceLogsRoot}.
	workDir, _ := makeLogsRoot(t)
	s := logscan.NewScanner()

	src := s.Default(workDir)

	if src.Kind != domain.SourceLogsRoot {
		t.Errorf("Default() Kind = %v, want SourceLogsRoot", src.Kind)
	}
}

func TestDefault_ExistingOrchestrationLogs_PathPointsToLogsRoot(t *testing.T) {
	// When OrchestrationLogs/ is present, the returned Source.Path must be the
	// absolute path to that folder, not workDir itself.
	workDir, logsRoot := makeLogsRoot(t)
	s := logscan.NewScanner()

	src := s.Default(workDir)

	if src.Path != logsRoot {
		t.Errorf("Default() Path = %q, want %q", src.Path, logsRoot)
	}
}

func TestDefault_AbsentOrchestrationLogs_ReturnsNotFound(t *testing.T) {
	// When OrchestrationLogs/ does not exist under workDir, Default must return
	// Source{Kind: SourceNotFound}. It must never return an error or panic.
	workDir := t.TempDir() // no OrchestrationLogs/ created
	s := logscan.NewScanner()

	src := s.Default(workDir)

	if src.Kind != domain.SourceNotFound {
		t.Errorf("Default() Kind = %v, want SourceNotFound", src.Kind)
	}
}

func TestDefault_NonExistentWorkDir_ReturnsNotFound(t *testing.T) {
	// When workDir itself does not exist, Default must return SourceNotFound
	// without error or panic — a missing directory is a described outcome.
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	s := logscan.NewScanner()

	src := s.Default(nonExistent)

	if src.Kind != domain.SourceNotFound {
		t.Errorf("Default() Kind = %v, want SourceNotFound for non-existent workDir", src.Kind)
	}
}

// ---- tests: Classify — path classification ----

func TestClassify_PathContainingRunIDFolder_ReturnsLogsRoot(t *testing.T) {
	// A path containing at least one run-id-shaped subfolder must be classified
	// as SourceLogsRoot.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1)
	s := logscan.NewScanner()

	src := s.Classify(root)

	if src.Kind != domain.SourceLogsRoot {
		t.Errorf("Classify() Kind = %v, want SourceLogsRoot", src.Kind)
	}
}

func TestClassify_PathContainingUnknownRunSubfolder_ReturnsLogsRoot(t *testing.T) {
	// A path containing an unknown-run/ subfolder must be classified as
	// SourceLogsRoot (unknown-run is a structural marker for a logs root).
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, domain.UnknownRunDirName), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()

	src := s.Classify(root)

	if src.Kind != domain.SourceLogsRoot {
		t.Errorf("Classify() Kind = %v, want SourceLogsRoot", src.Kind)
	}
}

func TestClassify_PathWithOrchestratorEventFile_ReturnsSingleRun(t *testing.T) {
	// A path that contains 00_orchestrator_events.jsonl must be classified as
	// SourceSingleRun — the path itself is a run folder.
	runDir := t.TempDir()
	makeOrchestratorFile(t, runDir)
	s := logscan.NewScanner()

	src := s.Classify(runDir)

	if src.Kind != domain.SourceSingleRun {
		t.Errorf("Classify() Kind = %v, want SourceSingleRun", src.Kind)
	}
}

func TestClassify_PathWithAgentFolderContainingEventFile_ReturnsSingleRun(t *testing.T) {
	// A path containing an agent-instance subfolder with 03_events.jsonl must
	// be classified as SourceSingleRun.
	runDir := t.TempDir()
	agentDir := makeAgentFolder(t, runDir, "test-agent#1")
	makeAgentEventFile(t, agentDir)
	s := logscan.NewScanner()

	src := s.Classify(runDir)

	if src.Kind != domain.SourceSingleRun {
		t.Errorf("Classify() Kind = %v, want SourceSingleRun", src.Kind)
	}
}

func TestClassify_NonExistentPath_ReturnsNotFound(t *testing.T) {
	// A path that does not exist must be classified as SourceNotFound.
	// Classify must never return an error for a missing path.
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	s := logscan.NewScanner()

	src := s.Classify(nonExistent)

	if src.Kind != domain.SourceNotFound {
		t.Errorf("Classify() Kind = %v, want SourceNotFound", src.Kind)
	}
}

func TestClassify_ExistingEmptyDirectory_ReturnsUnusableWithReason(t *testing.T) {
	// An existing path that matches no recognised structure (no run-id folders,
	// no unknown-run/, no orchestrator file, no agent event files) must be
	// classified as SourceUnusable. The Reason field must be non-empty.
	root := t.TempDir()
	s := logscan.NewScanner()

	src := s.Classify(root)

	if src.Kind != domain.SourceUnusable {
		t.Errorf("Classify() Kind = %v, want SourceUnusable for unrecognised directory", src.Kind)
	}
	if src.Reason == "" {
		t.Error("Source.Reason must be non-empty when Kind == SourceUnusable")
	}
}

func TestClassify_UnknownRunPathWithRunFolderStructure_ReturnsSingleRun(t *testing.T) {
	// A path that is itself named unknown-run and has run-folder structure must
	// be classified as SourceSingleRun.
	parent := t.TempDir()
	unknownRunPath := filepath.Join(parent, domain.UnknownRunDirName)
	if err := os.MkdirAll(unknownRunPath, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	makeOrchestratorFile(t, unknownRunPath)
	s := logscan.NewScanner()

	src := s.Classify(unknownRunPath)

	if src.Kind != domain.SourceSingleRun {
		t.Errorf("Classify() Kind = %v, want SourceSingleRun for unknown-run path with run structure", src.Kind)
	}
}

func TestClassify_PathReturned_MatchesSuppliedPath(t *testing.T) {
	// The Source.Path returned by Classify must be the path that was supplied.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1)
	s := logscan.NewScanner()

	src := s.Classify(root)

	if src.Path != root {
		t.Errorf("Classify() Path = %q, want %q", src.Path, root)
	}
}

// ---- tests: Enumerate — run enumeration ----

func TestEnumerate_RunFolderEnumeratedByRunID(t *testing.T) {
	// A run-id-shaped folder must appear in Inventory.Runs with the correct
	// RunRef: Kind == RunNamed and ID matching the folder name.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) != 1 {
		t.Fatalf("Inventory.Runs = %d, want 1", len(inv.Runs))
	}
	if inv.Runs[0].Run.Kind != domain.RunNamed {
		t.Errorf("Run.Kind = %v, want RunNamed", inv.Runs[0].Run.Kind)
	}
	if inv.Runs[0].Run.ID != validRunID1 {
		t.Errorf("Run.ID = %q, want %q", inv.Runs[0].Run.ID, validRunID1)
	}
}

func TestEnumerate_UnknownRunFolder_IsUnattributableBucket(t *testing.T) {
	// The unknown-run/ folder must be placed in Inventory.Unattributable, never
	// in Inventory.Runs.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, domain.UnknownRunDirName), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if inv.Unattributable == nil {
		t.Fatal("Inventory.Unattributable is nil, want non-nil for unknown-run/ folder")
	}
	if !inv.Unattributable.Run.IsUnattributable() {
		t.Error("Unattributable.Run.IsUnattributable() = false, want true")
	}
	if len(inv.Runs) != 0 {
		t.Errorf("Inventory.Runs = %d, want 0 (unknown-run must not appear as a named run)", len(inv.Runs))
	}
}

func TestEnumerate_UnknownRunFolder_WithInnerStructure_PopulatesFields(t *testing.T) {
	// AC5.4 (same code path as named runs): when unknown-run/ contains an
	// orchestrator event file and an agent-instance folder, Inventory.Unattributable
	// must have OrchestratorFile and Agents populated exactly as a named run would.
	// An implementation that only sets Unattributable.Run (skipping the inner
	// enumeration step for the bucket) would fail this test.
	root := t.TempDir()
	unknownRunDir := filepath.Join(root, domain.UnknownRunDirName)
	if err := os.MkdirAll(unknownRunDir, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orchFile := makeOrchestratorFile(t, unknownRunDir)
	agentDir := makeAgentFolder(t, unknownRunDir, "test-agent#1")
	agentEventFile := makeAgentEventFile(t, agentDir)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if inv.Unattributable == nil {
		t.Fatal("Inventory.Unattributable is nil, want non-nil")
	}
	if inv.Unattributable.OrchestratorFile != orchFile {
		t.Errorf("Unattributable.OrchestratorFile = %q, want %q", inv.Unattributable.OrchestratorFile, orchFile)
	}
	if len(inv.Unattributable.Agents) != 1 {
		t.Fatalf("Unattributable.Agents = %d, want 1", len(inv.Unattributable.Agents))
	}
	if inv.Unattributable.Agents[0].EventFile != agentEventFile {
		t.Errorf("Unattributable.Agents[0].EventFile = %q, want %q", inv.Unattributable.Agents[0].EventFile, agentEventFile)
	}
}

func TestEnumerate_PlainFile_IsSilentlyIgnored(t *testing.T) {
	// A plain file in the logs root must be silently ignored — it must not
	// appear as a run or produce a FindingUnrecognisedFolder.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, findings := s.Enumerate(src)

	if len(inv.Runs) != 1 {
		t.Errorf("Inventory.Runs = %d, want 1 (plain files must not add runs)", len(inv.Runs))
	}
	for _, f := range findings {
		if f.Kind == domain.FindingUnrecognisedFolder && filepath.Base(f.Path) == "README.md" {
			t.Error("plain file README.md must not produce FindingUnrecognisedFolder")
		}
	}
}

func TestEnumerate_UnrecognisedSubdirectory_ProducesFinding(t *testing.T) {
	// A subfolder whose name is neither run-id-shaped nor unknown-run must yield
	// a FindingUnrecognisedFolder finding.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1)
	if err := os.MkdirAll(filepath.Join(root, "not-a-run-id"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := s.Classify(root)

	_, findings := s.Enumerate(src)

	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingUnrecognisedFolder {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FindingUnrecognisedFolder for a subfolder with an unrecognised name")
	}
}

func TestEnumerate_NearMissRunID_ThreeHexChars_ProducesFinding(t *testing.T) {
	// A folder whose name is one hex character short of a valid run-id
	// (3-char suffix instead of 4) must yield FindingUnrecognisedFolder, not be
	// silently dropped or admitted as a run. This guards the IsRunID boundary
	// against off-by-one acceptance.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "20260101T000000Z-aaa"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}

	_, findings := s.Enumerate(src)

	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingUnrecognisedFolder {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FindingUnrecognisedFolder for near-miss run-id with 3-hex-char suffix")
	}
}

func TestEnumerate_NearMissRunID_LowercaseTSeparator_ProducesFinding(t *testing.T) {
	// A folder whose name uses a lowercase 't' separator instead of the required
	// uppercase 'T' must be treated as unrecognised and yield FindingUnrecognisedFolder.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "20260101t000000Z-aaaa"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}

	_, findings := s.Enumerate(src)

	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingUnrecognisedFolder {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FindingUnrecognisedFolder for near-miss run-id with lowercase 't' separator")
	}
}

func TestEnumerate_NearMissRunID_MissingTrailingZ_ProducesFinding(t *testing.T) {
	// A folder whose name omits the trailing 'Z' before the hex suffix must be
	// treated as unrecognised, not as a valid run-id.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "20260101T000000-aaaa"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}

	_, findings := s.Enumerate(src)

	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingUnrecognisedFolder {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FindingUnrecognisedFolder for near-miss run-id missing trailing 'Z'")
	}
}

func TestEnumerate_UnrecognisedSubdirectory_NotIncludedInRuns(t *testing.T) {
	// A subfolder with an unrecognised name must be excluded from Inventory.Runs.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1)
	if err := os.MkdirAll(filepath.Join(root, "not-a-run-id"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) != 1 {
		t.Errorf("Inventory.Runs = %d, want 1 (unrecognised folder must be excluded)", len(inv.Runs))
	}
}

func TestEnumerate_NamedRuns_SortedByRunIDAscending(t *testing.T) {
	// Named runs must appear in Inventory.Runs sorted by run_id ascending.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID3)
	makeRunFolder(t, root, validRunID1)
	makeRunFolder(t, root, validRunID2)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) != 3 {
		t.Fatalf("Inventory.Runs = %d, want 3", len(inv.Runs))
	}
	if inv.Runs[0].Run.ID != validRunID1 {
		t.Errorf("Runs[0].ID = %q, want %q (ascending order)", inv.Runs[0].Run.ID, validRunID1)
	}
	if inv.Runs[1].Run.ID != validRunID2 {
		t.Errorf("Runs[1].ID = %q, want %q", inv.Runs[1].Run.ID, validRunID2)
	}
	if inv.Runs[2].Run.ID != validRunID3 {
		t.Errorf("Runs[2].ID = %q, want %q", inv.Runs[2].Run.ID, validRunID3)
	}
}

func TestEnumerate_NonUsableSource_PropagatesSourceFieldsUnchanged(t *testing.T) {
	// A non-usable source must be threaded through Enumerate unchanged into
	// Inventory.Source. This test uses SourceUnusable with a distinctive Path and
	// Reason so that the assertion can only pass when the argument is genuinely
	// propagated — not because the expected values coincide with the zero value
	// (as they would if SourceNotFound were used instead).
	distinctivePath := filepath.Join(t.TempDir(), "logs-that-exist-but-are-not-usable")
	if err := os.MkdirAll(distinctivePath, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	distinctiveReason := "no recognised structure: test-sentinel-reason"
	src := domain.Source{
		Kind:   domain.SourceUnusable,
		Path:   distinctivePath,
		Reason: distinctiveReason,
	}
	s := logscan.NewScanner()

	inv, _ := s.Enumerate(src)

	if inv.Source.Kind != domain.SourceUnusable {
		t.Errorf("Inventory.Source.Kind = %v, want SourceUnusable", inv.Source.Kind)
	}
	if inv.Source.Path != distinctivePath {
		t.Errorf("Inventory.Source.Path = %q, want %q", inv.Source.Path, distinctivePath)
	}
	if inv.Source.Reason != distinctiveReason {
		t.Errorf("Inventory.Source.Reason = %q, want %q", inv.Source.Reason, distinctiveReason)
	}
	if len(inv.Runs) != 0 {
		t.Errorf("Inventory.Runs = %d, want 0 for non-usable source", len(inv.Runs))
	}
	if inv.Unattributable != nil {
		t.Error("Inventory.Unattributable must be nil for a non-usable source")
	}
}

func TestEnumerate_RunEntry_DirIsAbsolutePath(t *testing.T) {
	// RunEntry.Dir must be the absolute path to the run folder.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	if inv.Runs[0].Dir != runDir {
		t.Errorf("RunEntry.Dir = %q, want %q", inv.Runs[0].Dir, runDir)
	}
}

func TestEnumerate_EmptyRunIDFolder_IsEnumeratedAsBareRunEntry(t *testing.T) {
	// A run-id-shaped folder with no orchestrator file and no agent-instance
	// folders must still appear in Inventory.Runs as a RunEntry with
	// OrchestratorFile == "" and an empty Agents slice. An empty run is a valid
	// structural outcome, not a reason to omit the run from the inventory.
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1) // empty: no files, no subfolders
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) != 1 {
		t.Fatalf("Inventory.Runs = %d, want 1 for an empty but structurally valid run folder", len(inv.Runs))
	}
	if inv.Runs[0].OrchestratorFile != "" {
		t.Errorf("OrchestratorFile = %q, want \"\" for completely empty run folder", inv.Runs[0].OrchestratorFile)
	}
	if len(inv.Runs[0].Agents) != 0 {
		t.Errorf("Agents = %d, want 0 for completely empty run folder", len(inv.Runs[0].Agents))
	}
}

// ---- tests: Enumerate — agent-instance and event-file enumeration ----

func TestEnumerate_OrchestratorOnly_AgentsSliceIsEmpty(t *testing.T) {
	// A run folder containing only the orchestrator event file must produce a
	// RunEntry with an empty Agents slice.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	makeOrchestratorFile(t, runDir)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	if len(inv.Runs[0].Agents) != 0 {
		t.Errorf("RunEntry.Agents = %d, want 0 for orchestrator-only run", len(inv.Runs[0].Agents))
	}
}

func TestEnumerate_OrchestratorFile_IsSetWhenPresent(t *testing.T) {
	// When 00_orchestrator_events.jsonl is present, RunEntry.OrchestratorFile
	// must be the absolute path to that file.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	orchFile := makeOrchestratorFile(t, runDir)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	if inv.Runs[0].OrchestratorFile != orchFile {
		t.Errorf("OrchestratorFile = %q, want %q", inv.Runs[0].OrchestratorFile, orchFile)
	}
}

func TestEnumerate_OrchestratorFile_IsEmptyWhenAbsent(t *testing.T) {
	// When 00_orchestrator_events.jsonl is absent from the run folder,
	// RunEntry.OrchestratorFile must be "".
	root := t.TempDir()
	makeRunFolder(t, root, validRunID1) // no orchestrator file
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	if inv.Runs[0].OrchestratorFile != "" {
		t.Errorf("OrchestratorFile = %q, want \"\" when absent", inv.Runs[0].OrchestratorFile)
	}
}

func TestEnumerate_AgentFolder_WithEventFile_EventFileIsSet(t *testing.T) {
	// An agent-instance folder containing 03_events.jsonl must have
	// AgentEntry.EventFile set to the absolute path of that file.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	agentDir := makeAgentFolder(t, runDir, "test-agent#1")
	agentEventFile := makeAgentEventFile(t, agentDir)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	if len(inv.Runs[0].Agents) == 0 {
		t.Fatal("expected 1 agent entry, got 0")
	}
	if inv.Runs[0].Agents[0].EventFile != agentEventFile {
		t.Errorf("AgentEntry.EventFile = %q, want %q", inv.Runs[0].Agents[0].EventFile, agentEventFile)
	}
}

func TestEnumerate_AgentFolder_NoEventFile_EventFileIsEmpty(t *testing.T) {
	// An agent-instance folder with no 03_events.jsonl must have
	// AgentEntry.EventFile == "".
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	makeAgentFolder(t, runDir, "test-agent#1") // no event file
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	if len(inv.Runs[0].Agents) == 0 {
		t.Fatal("expected 1 agent entry, got 0")
	}
	if inv.Runs[0].Agents[0].EventFile != "" {
		t.Errorf("AgentEntry.EventFile = %q, want \"\" when event file is absent", inv.Runs[0].Agents[0].EventFile)
	}
}

func TestEnumerate_AgentFolder_NoEventFile_ProducesMissingEventFileFinding(t *testing.T) {
	// An agent-instance folder with no 03_events.jsonl must yield a
	// FindingMissingEventFile finding. The absence of the event file is
	// communicable, not silently tolerated.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	makeAgentFolder(t, runDir, "test-agent#1") // no event file
	s := logscan.NewScanner()
	src := s.Classify(root)

	_, findings := s.Enumerate(src)

	var found bool
	for _, f := range findings {
		if f.Kind == domain.FindingMissingEventFile {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FindingMissingEventFile when agent folder contains no event file")
	}
}

func TestEnumerate_MultipleAgentFolders_SortedByDir(t *testing.T) {
	// When a run has multiple agent-instance folders, RunEntry.Agents must be
	// sorted by Dir.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	// Create agents in reverse-alphabetical order to expose the sort requirement.
	agentBDir := makeAgentFolder(t, runDir, "test-agent-b#2")
	agentADir := makeAgentFolder(t, runDir, "test-agent-a#1")
	makeAgentEventFile(t, agentADir)
	makeAgentEventFile(t, agentBDir)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 {
		t.Fatal("expected 1 run, got 0")
	}
	agents := inv.Runs[0].Agents
	if len(agents) != 2 {
		t.Fatalf("Agents = %d, want 2", len(agents))
	}
	if agents[0].Dir >= agents[1].Dir {
		t.Errorf("Agents not sorted by Dir: [0]=%q [1]=%q", agents[0].Dir, agents[1].Dir)
	}
}

func TestEnumerate_AgentEntry_InstanceHint_DerivedFromFolderName(t *testing.T) {
	// AgentEntry.InstanceHint must be derived from the agent folder name.
	// The hint is advisory (the event field is authoritative), but must not be
	// empty when the folder name is non-empty.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	agentDir := makeAgentFolder(t, runDir, "test-agent#1")
	makeAgentEventFile(t, agentDir)
	s := logscan.NewScanner()
	src := s.Classify(root)

	inv, _ := s.Enumerate(src)

	if len(inv.Runs) == 0 || len(inv.Runs[0].Agents) == 0 {
		t.Fatal("expected 1 run with 1 agent")
	}
	if inv.Runs[0].Agents[0].InstanceHint == "" {
		t.Error("AgentEntry.InstanceHint must not be empty when the folder name is non-empty")
	}
}

func TestEnumerate_EventFilesWithInvalidContent_SucceedsWithoutReadingContent(t *testing.T) {
	// AC5.6: Enumerate uses directory structure only — file contents are never read.
	// The orchestrator event file and an agent event file are given deliberately
	// invalid content. If Enumerate were to open and parse them, it would fail or
	// raise a content-level finding. Instead it must succeed and list both files by
	// path alone, with no FindingMalformedLine or FindingUnreadableFile findings.
	root := t.TempDir()
	runDir := makeRunFolder(t, root, validRunID1)
	orchFilePath := filepath.Join(runDir, logscan.OrchestratorEventFile)
	if err := os.WriteFile(orchFilePath, []byte("NOT VALID JSON {{{{{ <<< INVALID >>>"), 0600); err != nil {
		t.Fatalf("setup orchestrator file: %v", err)
	}
	agentDir := makeAgentFolder(t, runDir, "test-agent#1")
	agentEventPath := filepath.Join(agentDir, logscan.AgentEventFile)
	if err := os.WriteFile(agentEventPath, []byte("ALSO NOT JSON"), 0600); err != nil {
		t.Fatalf("setup agent event file: %v", err)
	}
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}
	s := logscan.NewScanner()

	inv, findings := s.Enumerate(src)

	// The run and its files must be listed by path — content is irrelevant.
	if len(inv.Runs) != 1 {
		t.Fatalf("Inventory.Runs = %d, want 1 — Enumerate must not reject files with invalid content", len(inv.Runs))
	}
	if inv.Runs[0].OrchestratorFile != orchFilePath {
		t.Errorf("OrchestratorFile = %q, want %q", inv.Runs[0].OrchestratorFile, orchFilePath)
	}
	if len(inv.Runs[0].Agents) != 1 {
		t.Fatalf("Agents = %d, want 1", len(inv.Runs[0].Agents))
	}
	if inv.Runs[0].Agents[0].EventFile != agentEventPath {
		t.Errorf("Agents[0].EventFile = %q, want %q", inv.Runs[0].Agents[0].EventFile, agentEventPath)
	}
	// No content-reading findings should appear — the scanner doesn't open files.
	for _, f := range findings {
		if f.Kind == domain.FindingUnreadableFile || f.Kind == domain.FindingMalformedLine {
			t.Errorf("unexpected content-level finding kind %v — Enumerate must not read file contents", f.Kind)
		}
	}
}

// ---- tests: Classify — additional input variants ----

func TestClassify_ExistingPlainFile_ReturnsUnusable(t *testing.T) {
	// A path that is an existing plain file (not a directory) must be classified
	// as SourceUnusable with a non-empty Reason. The file exists but has no
	// recognisable directory structure, so it cannot be a logs root or run folder.
	// Classify must never panic or return an error for this input.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plain-file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := logscan.NewScanner()

	src := s.Classify(filePath)

	if src.Kind != domain.SourceUnusable {
		t.Errorf("Classify() Kind = %v, want SourceUnusable for an existing plain file", src.Kind)
	}
	if src.Reason == "" {
		t.Error("Source.Reason must be non-empty when Kind == SourceUnusable")
	}
}

// ---- tests: InstanceHintFromDir ----

func TestInstanceHintFromDir_NonEmptyFolderName_ProducesNonEmptyHint(t *testing.T) {
	// A non-empty folder name must produce a non-empty AgentInstanceID hint.
	hint := logscan.InstanceHintFromDir("test-agent#1")
	if hint == "" {
		t.Error("InstanceHintFromDir() returned empty AgentInstanceID for a non-empty folder name")
	}
}

func TestInstanceHintFromDir_ValidAgentIDFolderName_RoundTrips(t *testing.T) {
	// A folder name already in AgentInstanceID format (Name#Number) must
	// round-trip through InstanceHintFromDir unchanged.
	const agentID = "test-agent#1"
	hint := logscan.InstanceHintFromDir(agentID)
	if string(hint) != agentID {
		t.Errorf("InstanceHintFromDir(%q) = %q, want %q", agentID, hint, agentID)
	}
}

// ---- tests: Empty-but-valid source ----

func TestEnumerate_EmptyLogsRoot_IsEmpty(t *testing.T) {
	// A logs root containing no run folders and no unknown-run/ folder must
	// yield Inventory.IsEmpty() == true. This is the explicit no-data outcome.
	root := t.TempDir()
	// Supply a usable Source directly to test the no-data path without relying
	// on Classify recognising an empty directory as SourceLogsRoot.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}
	s := logscan.NewScanner()

	inv, _ := s.Enumerate(src)

	if !inv.IsEmpty() {
		t.Error("Inventory.IsEmpty() = false, want true for a logs root with zero runs")
	}
}

func TestEnumerate_EmptyLogsRoot_KindIsLogsRoot_NotNotFound(t *testing.T) {
	// The empty-but-valid outcome must carry Source.Kind == SourceLogsRoot,
	// making it unambiguously distinct from SourceNotFound.
	root := t.TempDir()
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}
	s := logscan.NewScanner()

	inv, _ := s.Enumerate(src)

	if inv.Source.Kind == domain.SourceNotFound {
		t.Error("empty-but-valid source must not carry Kind == SourceNotFound")
	}
	if !inv.IsEmpty() {
		t.Error("Inventory.IsEmpty() = false, want true")
	}
}

func TestEnumerate_EmptyLogsRoot_ProducesNoFindings(t *testing.T) {
	// A genuinely empty logs root (no entries at all) must produce zero
	// findings — absence of runs is a described outcome, not a data-quality problem.
	root := t.TempDir()
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: root}
	s := logscan.NewScanner()

	_, findings := s.Enumerate(src)

	if len(findings) != 0 {
		t.Errorf("Enumerate() findings = %d, want 0 for an empty logs root", len(findings))
	}
}
