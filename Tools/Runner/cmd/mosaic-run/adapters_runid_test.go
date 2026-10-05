package main

// adapters_runid_test.go covers the run id the composition root binds to the
// harness adapter. The adapter derives the MOSAIC role environment of its
// script-orchestrator calls from that id, so both frontends must supply the id
// of the run they are building the session for: the CLI through buildAdapter
// with the resolved run folder, the TUI through the interactive wiring's
// per-session dependency construction.

import (
	"path/filepath"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/tui/screens/runconfig"
)

// adapterRunID is the narrow assertion target for the run id an adapter was
// bound to.
type adapterRunID interface {
	RunID() string
}

var realHarnessIDs = []string{
	commonharness.HarnessIDClaudeCode,
	commonharness.HarnessIDOpenCode,
	commonharness.HarnessIDGHCPCLI,
}

func TestBuildAdapter_WithRunFolder_BindsTheRunIDOnEveryRealHarness(t *testing.T) {
	const runID = "20261003T185044Z-07e9"
	runFolder := testRunFolder(t, runID)

	for _, harnessID := range realHarnessIDs {
		harnessID := harnessID
		t.Run(harnessID, func(t *testing.T) {
			h := buildAdapter(runFolder, harnessID, "some-path", "blanket", 5*time.Minute)

			reporter, ok := h.(adapterRunID)
			if !ok {
				t.Fatalf("buildAdapter(%q) returned %T, which does not report its run id", harnessID, h)
			}
			if got := reporter.RunID(); got != runID {
				t.Errorf("adapter RunID() = %q, want %q", got, runID)
			}
		})
	}
}

func TestBuildAdapter_WithoutRunFolder_BindsNoRunID(t *testing.T) {
	for _, harnessID := range realHarnessIDs {
		harnessID := harnessID
		t.Run(harnessID, func(t *testing.T) {
			h := buildAdapter("", harnessID, "some-path", "blanket", 5*time.Minute)

			reporter, ok := h.(adapterRunID)
			if !ok {
				t.Fatalf("buildAdapter(%q) returned %T, which does not report its run id", harnessID, h)
			}
			if got := reporter.RunID(); got != "" {
				t.Errorf("adapter RunID() = %q, want empty for an empty run folder", got)
			}
		})
	}
}

func TestBuildAdapter_WithNonRunFolder_BindsNoRunID(t *testing.T) {
	notARunFolder := filepath.Join(t.TempDir(), "not-a-run-folder")

	h := buildAdapter(notARunFolder, commonharness.HarnessIDClaudeCode, "some-path", "", 5*time.Minute)

	reporter, ok := h.(adapterRunID)
	if !ok {
		t.Fatalf("buildAdapter returned %T, which does not report its run id", h)
	}
	if got := reporter.RunID(); got != "" {
		t.Errorf("adapter RunID() = %q, want empty for a folder that is not Orchestration-{run_id}", got)
	}
}

func TestInteractiveWiring_NewDeps_BindsTheSessionRunIDToTheHarnessAdapter(t *testing.T) {
	cases := []struct {
		name     string
		harness  string
		ghcpMode string
	}{
		{name: "claude-code", harness: commonharness.HarnessIDClaudeCode},
		{name: "opencode", harness: commonharness.HarnessIDOpenCode},
		{name: "ghcp-cli", harness: commonharness.HarnessIDGHCPCLI, ghcpMode: "blanket"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Arrange -- a resolved identity, and a rebuilt session for another
			// run folder: the adapter must follow the folder argument.
			in, _ := recordedInput(t, resolvedTestIdentity(t))
			w := buildInteractiveWiring(in)
			const runID = "20261003T185044Z-07e9"
			cfg := runconfig.ConfigSelection{Harness: tc.harness, GHCPCLIMode: tc.ghcpMode}

			// Act
			deps := w.NewDeps(testRunFolder(t, runID), true, "", cfg)

			// Assert
			reporter, ok := deps.Harness.(adapterRunID)
			if !ok {
				t.Fatalf("Deps.Harness is %T, which does not report its run id", deps.Harness)
			}
			if got := reporter.RunID(); got != runID {
				t.Errorf("Deps.Harness RunID() = %q, want %q: script-orchestrator calls would carry the wrong run", got, runID)
			}
		})
	}
}
