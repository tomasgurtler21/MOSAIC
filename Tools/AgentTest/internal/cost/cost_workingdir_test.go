package cost_test

// Tests for the working-directory seam on the cost provider.
//
// The log analyzer resolves MosaicLogAnalyzer/config/pricing.yaml relative to
// its own working directory and exposes no root-override flag, so the only way
// to price a model correctly is to run the subprocess in the MOSAIC root. These
// tests assert that Options.WorkingDir reaches the CommandRunner's workingDir
// parameter, and that an absent WorkingDir passes an empty string through the
// seam — preserving the pre-existing "inherit the parent process's working
// directory" behaviour.
//
// The tests reference the new CommandRunner signature:
//
//	func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error)
//
// and the new Options.WorkingDir field, neither of which exist yet. They will
// not compile against the current implementation; that is the expected TDD RED
// state.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
)

// TestCost_ConfiguredWorkingDir_IsPassedToCommandRunner verifies that when
// Options.WorkingDir is set to a non-empty path, the CommandRunner seam
// receives that path as its workingDir argument.
//
// This is the core of the fix: the composition root supplies the resolved
// MOSAIC root here, and the runner must forward it to exec.Cmd.Dir on the real
// path. The seam makes that observable without a real subprocess.
func TestCost_ConfiguredWorkingDir_IsPassedToCommandRunner(t *testing.T) {
	const wantWorkingDir = "/home/user/mosaic"

	var gotWorkingDir string

	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		WorkingDir:     wantWorkingDir,
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			gotWorkingDir = workingDir
			return []byte(`{"schema_version":"1","run_id":"run-1","currency":"USD","money":{"state":"known","amount":"0.10"},"complete":true}`), 0, nil
		},
	})

	_, err := p.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if gotWorkingDir != wantWorkingDir {
		t.Errorf("CommandRunner received workingDir = %q, want %q — the MOSAIC root must reach the seam exactly", gotWorkingDir, wantWorkingDir)
	}
}

// TestCost_EmptyWorkingDir_PassesEmptyStringToCommandRunner verifies that when
// Options.WorkingDir is not set (empty string), the CommandRunner seam receives
// an empty string rather than any synthesised value.
//
// An empty workingDir means "inherit the parent process's working directory",
// which is what every caller had before this field existed. The provider must
// not invent a directory when none was configured.
func TestCost_EmptyWorkingDir_PassesEmptyStringToCommandRunner(t *testing.T) {
	var gotWorkingDir = "sentinel — must be overwritten by the provider"

	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		// WorkingDir intentionally absent (zero value).
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			gotWorkingDir = workingDir
			return []byte(`{"schema_version":"1","run_id":"run-1","currency":"USD","money":{"state":"known","amount":"0.00"},"complete":true}`), 0, nil
		},
	})

	_, err := p.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if gotWorkingDir != "" {
		t.Errorf("CommandRunner received workingDir = %q, want empty string — an unconfigured WorkingDir must pass the empty string through the seam so exec.Cmd.Dir remains unset and the subprocess inherits the parent's working directory", gotWorkingDir)
	}
}

// TestCost_WorkingDirDoesNotAffectCommandArguments verifies that the working
// directory value is threaded through the seam parameter and does not leak into
// the command-line arguments passed to the log-analysis tool.
//
// The log-analysis tool's CLI contract is unchanged: workingDir is how we
// position the process, not something we pass as a flag.
func TestCost_WorkingDirDoesNotAffectCommandArguments(t *testing.T) {
	const configuredDir = "/home/user/mosaic"

	var gotArgs []string

	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		WorkingDir:     configuredDir,
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			gotArgs = append([]string(nil), args...)
			return []byte(`{"schema_version":"1","run_id":"run-1","currency":"USD","money":{"state":"known","amount":"0.05"},"complete":true}`), 0, nil
		},
	})

	_, err := p.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	for _, arg := range gotArgs {
		if arg == configuredDir {
			t.Errorf("working directory %q appeared in command arguments — it must be passed only as the subprocess working directory, not as a CLI flag", configuredDir)
		}
	}
}
