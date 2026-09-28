package testdeploy_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testdeploy"
)

// =============================================================================
// Infrastructure flag: nil/empty/populated semantics
// =============================================================================

// TestDeploy_InfrastructureKeysNil_FlagAbsent asserts that when infrastructureKeys
// is nil, --infrastructure is NOT present in the emitted args. Nil means "don't
// know" -- the deploy tool will ask interactively. Emitting --infrastructure ""
// when keys are nil would suppress the interactive prompt incorrectly.
func TestDeploy_InfrastructureKeysNil_FlagAbsent(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto"}, []string{"smoke-single"}, nil)

	if len(capturedArgs) == 0 {
		t.Fatal("Deploy did not invoke CommandRunner; cannot assert argument absence")
	}
	if flagIndex(capturedArgs, "--infrastructure") >= 0 {
		t.Errorf("args = %v, want --infrastructure to be ABSENT when infrastructureKeys is nil "+
			"(nil means 'don't know'; omitting the flag lets the deploy tool ask interactively)", capturedArgs)
	}
}

// TestDeploy_InfrastructureKeysEmpty_FlagPresentWithEmptyValue asserts that when
// infrastructureKeys is a non-nil empty slice, --infrastructure "" is emitted.
// Non-nil empty means "explicitly none" -- the deploy tool deploys no infra agents.
func TestDeploy_InfrastructureKeysEmpty_FlagPresentWithEmptyValue(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto"}, []string{"smoke-single"}, []string{})

	idx := flagIndex(capturedArgs, "--infrastructure")
	if idx < 0 {
		t.Fatalf("args = %v, want --infrastructure to be PRESENT when infrastructureKeys is non-nil empty "+
			"(non-nil empty means 'explicitly none'; the flag must be emitted with an empty value)", capturedArgs)
	}
	if idx+1 >= len(capturedArgs) {
		t.Fatalf("args = %v, --infrastructure flag has no following value argument", capturedArgs)
	}
	if capturedArgs[idx+1] != "" {
		t.Errorf("--infrastructure value = %q, want \"\" (empty string) for non-nil empty infrastructureKeys; "+
			"strings.Join([]string{}, \",\") must produce \"\"",
			capturedArgs[idx+1])
	}
}

// TestDeploy_InfrastructureKeysPopulated_FlagPresentWithCommaJoinedKeys asserts
// that when infrastructureKeys is a populated slice, --infrastructure key1,key2,key3
// is emitted with the keys comma-joined in order.
func TestDeploy_InfrastructureKeysPopulated_FlagPresentWithCommaJoinedKeys(t *testing.T) {
	var capturedArgs []string
	infraKeys := []string{"mosaictest-checkpoint", "mosaictest-commit", "mosaictest-review"}

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto"}, []string{"smoke-single"}, infraKeys)

	idx := flagIndex(capturedArgs, "--infrastructure")
	if idx < 0 {
		t.Fatalf("args = %v, want --infrastructure <keys> when infrastructureKeys is populated", capturedArgs)
	}
	if idx+1 >= len(capturedArgs) {
		t.Fatalf("args = %v, --infrastructure flag has no following value argument", capturedArgs)
	}
	const want = "mosaictest-checkpoint,mosaictest-commit,mosaictest-review"
	if capturedArgs[idx+1] != want {
		t.Errorf("--infrastructure value = %q, want %q (comma-joined, order preserved)",
			capturedArgs[idx+1], want)
	}
}

// TestDeploy_InfrastructureKeysSingleKey_NoTrailingComma asserts that a single
// infrastructure key produces a plain ID with no comma.
func TestDeploy_InfrastructureKeysSingleKey_NoTrailingComma(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto"}, []string{"smoke-single"}, []string{"mosaictest-review"})

	idx := flagIndex(capturedArgs, "--infrastructure")
	if idx < 0 || idx+1 >= len(capturedArgs) {
		t.Fatalf("args = %v, want --infrastructure <key>", capturedArgs)
	}
	if capturedArgs[idx+1] != "mosaictest-review" {
		t.Errorf("--infrastructure value = %q, want %q (single key, no comma)",
			capturedArgs[idx+1], "mosaictest-review")
	}
}

// TestDeploy_InfrastructureFlag_EmittedBeforeAutoConfirm asserts that --infrastructure
// appears BEFORE --auto-confirm in the arg list. The deploy CLI processes flags in
// registration order; emitting infrastructure before the auto-confirm flag preserves
// a stable, predictable argument order.
func TestDeploy_InfrastructureFlag_EmittedBeforeAutoConfirm(t *testing.T) {
	var capturedArgs []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			capturedArgs = append([]string(nil), args...)
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto"}, []string{"smoke-single"}, []string{"mosaictest-review"})

	infraIdx := flagIndex(capturedArgs, "--infrastructure")
	confirmIdx := flagIndex(capturedArgs, "--auto-confirm")
	if infraIdx < 0 {
		t.Fatal("--infrastructure not present in args")
	}
	if confirmIdx < 0 {
		t.Fatal("--auto-confirm not present in args")
	}
	if infraIdx > confirmIdx {
		t.Errorf("--infrastructure (index %d) appears AFTER --auto-confirm (index %d); "+
			"--infrastructure must precede --auto-confirm in the arg list",
			infraIdx, confirmIdx)
	}
}

// TestDeploy_InfrastructureKeys_MultiHarness_SharedAcrossInvocations asserts
// that the same infrastructure keys are passed to every per-harness invocation.
func TestDeploy_InfrastructureKeys_MultiHarness_SharedAcrossInvocations(t *testing.T) {
	infraKeys := []string{"mosaictest-checkpoint", "mosaictest-review"}
	var observedValues []string

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			idx := flagIndex(args, "--infrastructure")
			if idx >= 0 && idx+1 < len(args) {
				observedValues = append(observedValues, args[idx+1])
			} else {
				observedValues = append(observedValues, "<absent>")
			}
			return nil, nil, exitSuccess, nil
		},
	})

	_ = d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"}, infraKeys)

	if len(observedValues) != 2 {
		t.Fatalf("CommandRunner invoked %d time(s), want 2", len(observedValues))
	}
	const want = "mosaictest-checkpoint,mosaictest-review"
	for i, v := range observedValues {
		if v != want {
			t.Errorf("invocation %d: --infrastructure value = %q, want %q "+
				"(same keys for every harness invocation)",
				i, v, want)
		}
	}
}
