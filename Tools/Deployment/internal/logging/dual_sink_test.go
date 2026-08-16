package logging_test

// Tests for the dual-sink logger (T15.1) and run-scoped context (T15.2).
//
// The logger writes to two sinks simultaneously:
//   - latest.log: truncated at each StartRun; contains records from the current run only.
//   - history.log: appended; retains records from every prior run.
//
// Both sinks receive the same run-scoped context: harness identity, provision tier, source
// path, workspace path, fallback location, and external module information when present.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// ---------------------------------------------------------------------------
// Shared test helpers (used across all logging test files in this package)
// ---------------------------------------------------------------------------

// makeLogRoot creates a temporary MosaicDeploy root directory for a test.
func makeLogRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// defaultLogConfig returns a ToolConfig with sensible defaults for logging tests.
func defaultLogConfig() config.ToolConfig {
	return config.ToolConfig{LogRetentionRuns: 0}
}

// sampleRunContext returns a minimal, valid RunContext identified by the given runID.
func sampleRunContext(runID string) logging.RunContext {
	return logging.RunContext{
		RunID:          runID,
		StartedAt:      time.Now(),
		Mode:           domain.ModeDeployWorkspace,
		Harness:        domain.HarnessRef{ID: "test-harness", Tier: domain.TierBuiltin},
		WorkspacePath:  "/workspace/test",
		DeploymentRoot: "/workspace/test/agents",
		Fallback:       domain.FallbackNone,
	}
}

// sampleEvent returns a minimal Event for a named subject.
func sampleEvent(subject string) logging.Event {
	return logging.Event{
		Time:    time.Now(),
		Level:   logging.LevelInfo,
		Kind:    "write",
		Subject: subject,
		Message: "artifact written",
	}
}

// readSinkFile reads the content of a log sink file; fails the test if it cannot be read.
func readSinkFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readSinkFile(%q): %v", path, err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// T15.1: Dual-sink behaviour
// ---------------------------------------------------------------------------

// TestDualSink_LatestContainsOnlyCurrentRun verifies that after two consecutive runs the
// latest.log file contains records from the second run only, not from the first.
func TestDualSink_LatestContainsOnlyCurrentRun(t *testing.T) {
	root := makeLogRoot(t)
	cfg := defaultLogConfig()

	// First run — establishes initial content in both sinks.
	log1 := logging.New(root, cfg)
	rc1 := sampleRunContext("run-aaa-111")
	log1.StartRun(rc1)
	log1.Event(sampleEvent("agent-alpha"))
	log1.EndRun(domain.OutcomeSuccess)

	// Second run — latest.log must be truncated; only this run's records should remain.
	log2 := logging.New(root, cfg)
	rc2 := sampleRunContext("run-bbb-222")
	log2.StartRun(rc2)
	log2.Event(sampleEvent("agent-beta"))
	log2.EndRun(domain.OutcomeSuccess)

	content := readSinkFile(t, log2.Paths().Latest)

	if strings.Contains(content, rc1.RunID) {
		t.Errorf("latest.log still contains run-1 RunID %q after run-2; want truncation at StartRun", rc1.RunID)
	}
	if !strings.Contains(content, rc2.RunID) {
		t.Errorf("latest.log missing run-2 RunID %q", rc2.RunID)
	}
}

// TestDualSink_HistoryRetainsAllRuns verifies that history.log accumulates records from
// every prior run without truncation.
func TestDualSink_HistoryRetainsAllRuns(t *testing.T) {
	root := makeLogRoot(t)
	cfg := defaultLogConfig()

	log1 := logging.New(root, cfg)
	rc1 := sampleRunContext("run-ccc-333")
	log1.StartRun(rc1)
	log1.EndRun(domain.OutcomeSuccess)

	log2 := logging.New(root, cfg)
	rc2 := sampleRunContext("run-ddd-444")
	log2.StartRun(rc2)
	log2.EndRun(domain.OutcomeSuccess)

	content := readSinkFile(t, log2.Paths().History)

	if !strings.Contains(content, rc1.RunID) {
		t.Errorf("history.log missing run-1 RunID %q; want all runs retained", rc1.RunID)
	}
	if !strings.Contains(content, rc2.RunID) {
		t.Errorf("history.log missing run-2 RunID %q", rc2.RunID)
	}
}

// TestDualSink_BothSinksReceiveSameRecords verifies that a single run's records appear in
// both the latest and history files with the same RunID.
func TestDualSink_BothSinksReceiveSameRecords(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-eee-555")
	log.StartRun(rc)
	log.Event(sampleEvent("agent-gamma"))
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	latest := readSinkFile(t, paths.Latest)
	history := readSinkFile(t, paths.History)

	if !strings.Contains(latest, rc.RunID) {
		t.Errorf("latest.log missing RunID %q", rc.RunID)
	}
	if !strings.Contains(history, rc.RunID) {
		t.Errorf("history.log missing RunID %q", rc.RunID)
	}
}

// TestDualSink_ThreeRunsHistoryHasAll verifies that history.log retains records across more
// than two consecutive runs.
func TestDualSink_ThreeRunsHistoryHasAll(t *testing.T) {
	root := makeLogRoot(t)
	cfg := defaultLogConfig()
	ids := []string{"run-f1", "run-f2", "run-f3"}

	for _, id := range ids {
		l := logging.New(root, cfg)
		l.StartRun(sampleRunContext(id))
		l.EndRun(domain.OutcomeSuccess)
	}

	lastLog := logging.New(root, cfg)
	content := readSinkFile(t, lastLog.Paths().History)

	for _, id := range ids {
		if !strings.Contains(content, id) {
			t.Errorf("history.log missing RunID %q after three runs", id)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.2: Run-scoped context in both sinks
// ---------------------------------------------------------------------------

// TestRunContext_HarnessIDInBothSinks verifies that the harness ID from RunContext appears
// in both log sinks.
func TestRunContext_HarnessIDInBothSinks(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-harness-id")
	rc.Harness = domain.HarnessRef{ID: "claude-code", Tier: domain.TierBuiltin}
	log.StartRun(rc)
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	for _, tc := range []struct {
		name string
		path string
	}{
		{"latest", paths.Latest},
		{"history", paths.History},
	} {
		content := readSinkFile(t, tc.path)
		if !strings.Contains(content, rc.Harness.ID) {
			t.Errorf("%s.log missing harness ID %q", tc.name, rc.Harness.ID)
		}
	}
}

// TestRunContext_ProvisionTierInBothSinks verifies that the harness provision tier appears
// in both sinks so log readers can distinguish builtin, descriptor, and external sources.
func TestRunContext_ProvisionTierInBothSinks(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-tier")
	rc.Harness = domain.HarnessRef{ID: "ext-harness", Tier: domain.TierExternal}
	log.StartRun(rc)
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	for _, tc := range []struct {
		name string
		path string
	}{
		{"latest", paths.Latest},
		{"history", paths.History},
	} {
		content := readSinkFile(t, tc.path)
		if !strings.Contains(content, string(rc.Harness.Tier)) {
			t.Errorf("%s.log missing provision tier %q", tc.name, rc.Harness.Tier)
		}
	}
}

// TestRunContext_SourcePathInBothSinks verifies that a non-empty HarnessRef.SourcePath
// (present for descriptor and external tiers) appears in both sinks.
func TestRunContext_SourcePathInBothSinks(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-source-path")
	rc.Harness = domain.HarnessRef{
		ID:         "desc-harness",
		Tier:       domain.TierDescriptor,
		SourcePath: "/harnesses/my-harness/harness.yaml",
	}
	log.StartRun(rc)
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	for _, tc := range []struct {
		name string
		path string
	}{
		{"latest", paths.Latest},
		{"history", paths.History},
	} {
		content := readSinkFile(t, tc.path)
		if !strings.Contains(content, rc.Harness.SourcePath) {
			t.Errorf("%s.log missing harness source path %q", tc.name, rc.Harness.SourcePath)
		}
	}
}

// TestRunContext_WorkspacePathInBothSinks verifies that the workspace path appears in both sinks.
func TestRunContext_WorkspacePathInBothSinks(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-workspace")
	rc.WorkspacePath = "/some/distinctive/workspace/path"
	log.StartRun(rc)
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	for _, tc := range []struct {
		name string
		path string
	}{
		{"latest", paths.Latest},
		{"history", paths.History},
	} {
		content := readSinkFile(t, tc.path)
		if !strings.Contains(content, rc.WorkspacePath) {
			t.Errorf("%s.log missing workspace path %q", tc.name, rc.WorkspacePath)
		}
	}
}

// TestRunContext_FallbackLocationInBothSinks verifies that when a fallback deployment location
// was used, the fallback tier name appears in both sinks.
func TestRunContext_FallbackLocationInBothSinks(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-fallback")
	rc.Fallback = domain.FallbackMosaicRoot
	log.StartRun(rc)
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	for _, tc := range []struct {
		name string
		path string
	}{
		{"latest", paths.Latest},
		{"history", paths.History},
	} {
		content := readSinkFile(t, tc.path)
		if !strings.Contains(content, string(rc.Fallback)) {
			t.Errorf("%s.log missing fallback tier %q", tc.name, rc.Fallback)
		}
	}
}

// TestRunContext_ExternalModuleHarnessIDInBothSinks verifies that when an external harness
// module ran, its harness ID appears in both sinks.
func TestRunContext_ExternalModuleHarnessIDInBothSinks(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())
	rc := sampleRunContext("run-external")
	rc.External = &domain.ExternalModuleInfo{
		HarnessID:       "my-external-harness",
		ExecutablePath:  "/usr/local/bin/my-harness",
		ProtocolVersion: "1",
	}
	log.StartRun(rc)
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	for _, tc := range []struct {
		name string
		path string
	}{
		{"latest", paths.Latest},
		{"history", paths.History},
	} {
		content := readSinkFile(t, tc.path)
		if !strings.Contains(content, rc.External.HarnessID) {
			t.Errorf("%s.log missing external harness ID %q", tc.name, rc.External.HarnessID)
		}
		if !strings.Contains(content, rc.External.ExecutablePath) {
			t.Errorf("%s.log missing external executable path %q", tc.name, rc.External.ExecutablePath)
		}
	}
}

// TestPaths_PointToExpectedSinkFiles verifies that Paths() returns the paths mandated by
// contract regardless of whether the files have been written yet.
func TestPaths_PointToExpectedSinkFiles(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, defaultLogConfig())

	paths := log.Paths()
	wantLatest := filepath.Join(root, "MosaicDeploy", "logs", "latest.log")
	wantHistory := filepath.Join(root, "MosaicDeploy", "logs", "history.log")

	if paths.Latest != wantLatest {
		t.Errorf("Paths().Latest = %q; want %q", paths.Latest, wantLatest)
	}
	if paths.History != wantHistory {
		t.Errorf("Paths().History = %q; want %q", paths.History, wantHistory)
	}
}
