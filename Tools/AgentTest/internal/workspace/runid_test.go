package workspace_test

// T10.2: the logger bundle's run-id shape must survive sandbox-name
// sanitization intact. The layout's "T", "Z" and "-" characters are not
// unsafe path characters, but this pins that fact so a future tightening of
// the sanitizer cannot silently break run-id-derived sandbox naming without
// a test noticing.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/workspace"
)

func TestCreate_LoggerBundleShapedRunIDSurvivesSandboxNaming(t *testing.T) {
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	const runID = "20260809T171229Z-79ca"
	key := domain.RunKey{RunID: runID, TestID: "example", RunNumber: 1}

	sb, err := mgr.Create(key)
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}

	if !strings.Contains(sb.Root, runID) {
		t.Errorf("sandbox root %q does not contain the run id %q unchanged — sanitization must not mangle the logger bundle's required shape", sb.Root, runID)
	}
}

func TestCreate_TwoRunIDsDifferingOnlyInEntropySuffixProduceDistinctSandboxRoots(t *testing.T) {
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	keyA := domain.RunKey{RunID: "20260809T171229Z-79ca", TestID: "example", RunNumber: 1}
	keyB := domain.RunKey{RunID: "20260809T171229Z-ab12", TestID: "example", RunNumber: 1}

	sbA, err := mgr.Create(keyA)
	if err != nil {
		t.Fatalf("Create(keyA) returned unexpected error: %v", err)
	}
	sbB, err := mgr.Create(keyB)
	if err != nil {
		t.Fatalf("Create(keyB) returned unexpected error: %v", err)
	}

	if sbA.Root == sbB.Root {
		t.Errorf("two run ids differing only in entropy suffix produced the same sandbox root %q, want distinct roots", sbA.Root)
	}
}
