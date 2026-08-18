package e2e

// T18.7 — side-effect and isolation end-to-end tests: a stub's declared
// files exist before the response is delivered and are gone after teardown;
// the workspace is removed and nothing the run did not create is touched;
// and two runs do not observe each other's state, log or artifacts.
//
// Mid-run probing (TestSideEffects_DeclaredFileExists_BeforeResponseDelivered)
// needs test-only stand-in wiring beyond what T18.1-T18.6 required, because
// the fact under test — a side effect existing *before* the collaborator's
// reply is delivered — can only be observed from inside the sandbox while
// the run is still executing; the parent test process and the sandboxed run
// never share that window. See standin_test.go's standinScript.ProbeFiles:
// the stand-in checks the declared path's existence immediately after the
// pre-invocation interception that applies the side effect returns — which
// internal/interceptor.Run's own ordering guarantees happens before that
// call's reply is written (side effects are applied, from the matched
// stub's declared side_effects, during the pre-invocation phase, strictly
// before TranslateOutcome's reply leaves the process) — and reports the
// result back to the parent over a sidecar file, the same out-of-band
// channel pattern SleepBeforeExit and SeedDeadLock already established.
//
// "Two runs executing concurrently do not observe each other's state, log
// or artifacts" is demonstrated here as two genuinely independent runs
// (TestSideEffects_IndependentRuns_DoNotObserveEachOthersState), not as two
// runs literally overlapping in wall-clock time. That is a real,
// acknowledged scope limit of this suite's own scripting mechanism, not an
// interpretation of convenience: RunScenario drives each run through
// process-wide state — t.Setenv'd environment variables (PATH, the
// stand-in's sidecar path) that a spawned child process inherits at fork
// time — and Go's own os/exec documents that concurrent os.Setenv and
// process creation are not safe together. Two RunScenario calls running as
// literal concurrent goroutines in this same test binary would therefore
// risk one run's child inheriting the other's sidecar path — a race that
// would make the isolation claim itself untrustworthy, exactly the kind of
// flaky, non-deterministic test this suite exists to avoid. What this test
// instead demonstrates, soundly, is the structural property that actually
// backs the isolation claim: internal/workspace.Manager scopes every
// sandbox, and internal/runstate/internal/invlog scope every lock and log,
// by domain.RunKey — so two runs (here, sequential; the same guarantee
// governs the internal/suite package's own sequential test loop today)
// never share a sandbox, a lock file or a log file. Making this genuinely
// concurrent would mean moving the stand-in's sidecar channel off shared
// process environment variables (e.g. into a per-sandbox control-directory
// file, discovered via the "--sandbox" flag every stand-in invocation
// already receives) — real test-harness engineering, out of this session's
// scope, and recorded for a successor in Stage-18/PlanProgress.md.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// effectTimingScript is the one-collaborator sequence
// testdata/e2e/side-effects/effect-timing.test.yaml's stub registry expects:
// dispatch "effecter" (whose stub declares effect.txt as a side effect),
// then finish.
func effectTimingScript() domain.HarnessCapabilities {
	return domain.HarnessCapabilities{SupportsDirectSubstitution: true}
}

func TestSideEffects_DeclaredFileExists_BeforeResponseDelivered(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/side-effects",
		SuitePath: "side-effects.suite.yaml",
		Script: fakeOptionsFor(
			effectTimingScript(),
			[]subjectStep{
				invoke("dispatch", "effecter"),
				finish(`{"agent_instance_id":"standin-subject#1","run_id":"side-effects-run","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		ProbeFiles: []string{"effect.txt"},
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "effect-timing", domain.VerdictPass, cli.ExitSuccess)

	if existed, ok := out.ProbeResults["effect.txt"]; !ok || !existed {
		t.Errorf("ProbeResults[%q] = (%v, ok=%v), want (true, true) — the declared side effect must exist by the time the pre-invocation interception that applied it returns, before the collaborator's reply is delivered\nstdout: %s",
			"effect.txt", existed, ok, out.Stdout)
	}

	assertSandboxRemoved(t, out.WorkspaceRoot)
}

func TestSideEffects_WorkspaceRemoved_AndUntouchedFilesSurvive(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "untouched-marker.txt")
	const markerContent = "this file predates the run and must survive it\n"
	if err := os.WriteFile(marker, []byte(markerContent), 0o644); err != nil {
		t.Fatalf("writing pre-existing marker file: %v", err)
	}

	sc := Scenario{
		Dir:       "testdata/e2e/side-effects",
		SuitePath: "side-effects.suite.yaml",
		Script: fakeOptionsFor(
			effectTimingScript(),
			[]subjectStep{
				invoke("dispatch", "effecter"),
				finish(`{"agent_instance_id":"standin-subject#1","run_id":"side-effects-run","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		WorkspaceRoot: root,
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "effect-timing", domain.VerdictPass, cli.ExitSuccess)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading workspace root after the run: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "untouched-marker.txt" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("workspace root after teardown = %v, want exactly [untouched-marker.txt] — the run's own sandbox must be removed and nothing else touched\nstdout: %s",
			names, out.Stdout)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading marker file after the run: %v", err)
	}
	if string(got) != markerContent {
		t.Errorf("marker file content = %q, want %q — a file the run did not create must be left byte-for-byte alone", got, markerContent)
	}
}

// TestSideEffects_IndependentRuns_DoNotObserveEachOthersState covers two
// independently scripted, independently sandboxed runs — see this file's
// own package doc comment for why they are driven sequentially rather than
// as literal concurrent goroutines, and for what that sequential execution
// still soundly demonstrates about RunKey-scoped isolation.
func TestSideEffects_IndependentRuns_DoNotObserveEachOthersState(t *testing.T) {
	scA := Scenario{
		Dir:       "testdata/e2e/isolation-a",
		SuitePath: "isolation-a.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "worker-a"),
				finish(`{"agent_instance_id":"standin-subject#1","run_id":"isolation-a-run","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
	}
	scB := Scenario{
		Dir:       "testdata/e2e/isolation-b",
		SuitePath: "isolation-b.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "worker-b"),
				finish(`{"agent_instance_id":"standin-subject#1","run_id":"isolation-b-run","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
	}

	outA := RunScenario(t, scA)
	outB := RunScenario(t, scB)

	// Each run's own artifact_created/artifact_not_created pair is the
	// direct evidence of isolation: A's sandbox has A's artifact and not B's,
	// and vice versa, even though both runs execute against the same
	// production packages, the same fake adapter package and the same
	// stand-in binary.
	assertSingleTestVerdict(t, outA, "isolation-a-test", domain.VerdictPass, cli.ExitSuccess)
	assertSingleTestVerdict(t, outB, "isolation-b-test", domain.VerdictPass, cli.ExitSuccess)

	if outA.WorkspaceRoot == outB.WorkspaceRoot {
		t.Fatalf("both runs used the same workspace root %q, want distinct sandboxes", outA.WorkspaceRoot)
	}
	assertSandboxRemoved(t, outA.WorkspaceRoot)
	assertSandboxRemoved(t, outB.WorkspaceRoot)
}

// assertSandboxRemoved reports a test failure unless root — a workspace
// root RunScenario created and populated with exactly one sandbox — is
// empty afterward, proving internal/workspace.Manager.Teardown ran.
func assertSandboxRemoved(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading workspace root %q after the run: %v", root, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("workspace root %q after the run = %v, want empty — the sandbox must be removed by teardown", root, names)
	}
}
