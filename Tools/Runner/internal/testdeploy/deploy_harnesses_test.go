package testdeploy_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testdeploy"
)

// =============================================================================
// Multi-harness: one invocation per harness
// =============================================================================

// TestDeploy_SingleHarness_ExactlyOneInvocation asserts that a single harness
// produces exactly one CommandRunner invocation.
func TestDeploy_SingleHarness_ExactlyOneInvocation(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	if invocations != 1 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 1 for a single harness", invocations)
	}
}

// TestDeploy_TwoHarnesses_TwoInvocations asserts that two harnesses produce
// exactly two CommandRunner invocations. The deploy tool's --harness flag
// accepts a single value, so multi-harness deployment requires one call per
// harness.
func TestDeploy_TwoHarnesses_TwoInvocations(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"}, nil)

	if invocations != 2 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 2 for two harnesses (one per harness)", invocations)
	}
}

// TestDeploy_ThreeHarnesses_ThreeInvocations asserts that three harnesses
// produce exactly three CommandRunner invocations.
func TestDeploy_ThreeHarnesses_ThreeInvocations(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code", "ghcp-cli"}, []string{"smoke-single"}, nil)

	if invocations != 3 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 3 for three harnesses", invocations)
	}
}

// TestDeploy_TwoHarnesses_EachInvocationGetsCorrectHarnessID asserts that each
// per-harness invocation receives its own harness ID in --harness and not the
// other harness's ID. Without this, both invocations would receive the same
// harness and one harness would go undeployed.
func TestDeploy_TwoHarnesses_EachInvocationGetsCorrectHarnessID(t *testing.T) {
	var capturedByHarness []string // harness IDs extracted from each invocation

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--harness")
			if idx >= 0 && idx+1 < len(args) {
				capturedByHarness = append(capturedByHarness, args[idx+1])
			} else {
				capturedByHarness = append(capturedByHarness, "")
			}
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"}, nil)

	if len(capturedByHarness) != 2 {
		t.Fatalf("CommandRunner invoked %d time(s), want 2", len(capturedByHarness))
	}
	// Order may vary; assert that exactly the expected harness IDs appear.
	seen := map[string]bool{}
	for _, h := range capturedByHarness {
		seen[h] = true
	}
	if !seen["auto"] {
		t.Errorf("harness IDs across invocations = %v, want \"auto\" to appear", capturedByHarness)
	}
	if !seen["claude-code"] {
		t.Errorf("harness IDs across invocations = %v, want \"claude-code\" to appear", capturedByHarness)
	}
}

// TestDeploy_TwoHarnesses_SharedCatalogFolderAcrossInvocations asserts that
// all per-harness invocations share the same --catalog-folder value. The
// catalog path does not change per harness.
func TestDeploy_TwoHarnesses_SharedCatalogFolderAcrossInvocations(t *testing.T) {
	const catalog = "/opt/mosaic/Tools/Runner/TestCatalog"
	var catalogsObserved []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--catalog-folder")
			if idx >= 0 && idx+1 < len(args) {
				catalogsObserved = append(catalogsObserved, args[idx+1])
			} else {
				catalogsObserved = append(catalogsObserved, "")
			}
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), catalog, "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"}, nil)

	if len(catalogsObserved) != 2 {
		t.Fatalf("CommandRunner invoked %d time(s), want 2", len(catalogsObserved))
	}
	for i, c := range catalogsObserved {
		if c != catalog {
			t.Errorf("invocation %d: --catalog-folder = %q, want %q (same across all harness invocations)", i, c, catalog)
		}
	}
}
