package main

// mosaiclog_workspaceroot_test.go pins the workspace-root rule for both
// frontends: the run log lives under the parent directory of the run folder
// the session's artifact store is built at.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/runconfig"
)

// refusedRunConfig is a start request every session refuses before dispatching
// anything: its orchestrator file does not exist.
func refusedRunConfig(runFolder, runID, harnessID string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: filepath.Join(filepath.Dir(runFolder), "no-such-orchestrator.md"),
		WorkflowID:           "no-such-workflow",
		Task:                 "task",
		RunID:                runID,
		RunFolder:            runFolder,
		IsNewRun:             true,
		HarnessID:            harnessID,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}
}

// mlTUIConfig is a configuration selection for a real (non-fake) harness whose
// executable can never be launched: the refused start never reaches it.
func mlTUIConfig() runconfig.ConfigSelection {
	cfg := rawInvokerConfig()
	cfg.Harness = "opencode"
	return cfg
}

func TestTUIWiring_RunLogLivesUnderTheRunFolderParent(t *testing.T) {
	in, _ := recordedInput(t, resolvedTestIdentity(t))
	w := buildInteractiveWiring(in)
	runFolder, ws := mlRunFolder(t)
	sess := w.Options.SessionFactory(runFolder, true, "", mlTUIConfig())

	_, _ = sess.Start(context.Background(), refusedRunConfig(runFolder, mlRunID, "opencode"))

	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 1)
	if events[0]["cwd"] != ws || events[0]["run_id"] != mlRunID {
		t.Errorf("run_start = %v, want cwd %q and run_id %s", events[0], ws, mlRunID)
	}
	if events[1]["outcome"] != "aborted" {
		t.Errorf("run_end.outcome = %v, want aborted for a refusal", events[1]["outcome"])
	}
}

func TestTUIWiring_EachStartOnTheSameFactoryWritesItsOwnPair(t *testing.T) {
	in, _ := recordedInput(t, resolvedTestIdentity(t))
	w := buildInteractiveWiring(in)
	runFolder, ws := mlRunFolder(t)

	for i := 0; i < 2; i++ {
		sess := w.Options.SessionFactory(runFolder, true, "", mlTUIConfig())
		_, _ = sess.Start(context.Background(), refusedRunConfig(runFolder, mlRunID, "opencode"))
	}

	mlAssertPairs(t, mlReadEvents(t, mlRunLogPath(ws)), 2)
}

func TestTUIWiring_UnresolvedRunFolderLogsUnderTheMintedFolder(t *testing.T) {
	in, _ := recordedInput(t, deferredTestIdentity(t))
	var mintedID, mintedFolder string
	baseMinter := in.Minter
	in.Minter = func() (string, string) {
		mintedID, mintedFolder = baseMinter()
		return mintedID, mintedFolder
	}
	w := buildInteractiveWiring(in)
	sess := w.Options.SessionFactory("", true, "", mlTUIConfig())

	_, _ = sess.Start(context.Background(), refusedRunConfig(mintedFolder, mintedID, "opencode"))

	if mintedFolder == "" {
		t.Fatal("no run folder was minted for the unresolved run folder")
	}
	path := filepath.Join(filepath.Dir(mintedFolder), "OrchestrationLogs", mintedID, "00_orchestrator_events.jsonl")
	events := mlReadEvents(t, path)
	mlAssertPairs(t, events, 1)
	if events[0]["run_id"] != mintedID {
		t.Errorf("run_id = %v, want the minted %s: logs and artifact must refer to the same run", events[0]["run_id"], mintedID)
	}
}

func TestResolveSessionRunFolder_ReturnsResolvedFolderWithoutMinting(t *testing.T) {
	in, _ := recordedInput(t, resolvedTestIdentity(t))
	mints := 0
	in.Minter = func() (string, string) { mints++; return "", "" }
	runFolder, _ := mlRunFolder(t)

	got := resolveSessionRunFolder(runFolder, in)

	if got != runFolder || mints != 0 {
		t.Fatalf("got %q with %d mint(s), want %q with none", got, mints, runFolder)
	}
}

func TestResolveSessionRunFolder_MintsExactlyOnceWhenUnresolved(t *testing.T) {
	in, _ := recordedInput(t, deferredTestIdentity(t))
	mints := 0
	var minted string
	base := in.Minter
	in.Minter = func() (string, string) {
		mints++
		id, folder := base()
		minted = folder
		return id, folder
	}

	got := resolveSessionRunFolder("", in)

	if mints != 1 || got != minted || !isRunScopedFolder(got) {
		t.Fatalf("got %q with %d mint(s) (minted %q), want exactly one mint and its run-scoped folder", got, mints, minted)
	}
}
