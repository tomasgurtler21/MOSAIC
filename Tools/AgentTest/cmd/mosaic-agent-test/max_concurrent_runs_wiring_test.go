package main

// max_concurrent_runs_wiring_test.go covers T12.4: the resolved max-concurrent-runs
// bound reaches suite.Options field-for-field from both the CLI path (through
// composedSuiteRunner) and the TUI path (through tuiSuiteRunner).
//
// The review identified that the existing tests assert RunConfig.MaxConcurrentRuns
// and Model.maxConcurrentRuns but do not verify that suite.Options.MaxConcurrentRuns
// receives those values at suite-construction time. These tests close that gap.
//
// The newSuite seam on composedSuiteRunner and tuiSuiteRunner captures
// suite.Options before suite.New is called, allowing field-for-field assertions
// without running a real suite.
//
// RED-phase state: all tests compile (composedSuiteRunner.maxConcurrentRuns,
// tuiSuiteRunner.maxConcurrentRuns, and the newSuite seam are declared as stubs),
// but FAIL because composedSuiteRunner.Run does not yet copy r.maxConcurrentRuns
// into suite.Options.MaxConcurrentRuns (I12.5 does that wiring).

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/suite"
	"mosaic-agent-test/internal/workspace"
)

// minimalDepsForMaxConcurrentRunsTest returns a Deps with just enough fields
// populated so composedSuiteRunner.Run can complete an empty plan without
// panicking. The Clock field is required by suite.New; all others are zero.
func minimalDepsForMaxConcurrentRunsTest(t *testing.T) Deps {
	t.Helper()
	return Deps{
		Clock: fakeClock{},
	}
}

// ---------------------------------------------------------------------------
// CLI path (composedSuiteRunner): T12.4
// ---------------------------------------------------------------------------

// TestComposedSuiteRunner_MaxConcurrentRuns_ReachesSuiteOptions verifies that
// composedSuiteRunner threads its maxConcurrentRuns field into
// suite.Options.MaxConcurrentRuns when constructing the suite. This is the
// critical joint between the CLI's parsed flag (which I12.2 puts into
// RunConfig.MaxConcurrentRuns) and the suite's execution bound.
//
// RED-phase note: compiles (composedSuiteRunner.maxConcurrentRuns is declared
// as a stub field), but FAILS because composedSuiteRunner.Run does not yet copy
// r.maxConcurrentRuns into suite.Options.MaxConcurrentRuns. I12.5 adds that
// assignment, which makes this test GREEN.
func TestComposedSuiteRunner_MaxConcurrentRuns_ReachesSuiteOptions(t *testing.T) {
	cases := []struct {
		name  string
		bound int
	}{
		{name: "bound 1 (sequential)", bound: 1},
		{name: "bound 3", bound: 3},
		{name: "bound 8", bound: 8},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedOpts suite.Options
			captureSuite := func(opts suite.Options) *suite.Suite {
				capturedOpts = opts
				return suite.New(opts)
			}

			d := minimalDepsForMaxConcurrentRunsTest(t)
			ws := workspace.NewManager(t.TempDir(), fakeClock{})
			r := composedSuiteRunner{
				deps:              d,
				ws:                ws,
				retention:         domain.RetainNever,
				maxConcurrentRuns: tc.bound,
				newSuite:          captureSuite,
			}

			// Run an empty plan — no tests, suite returns immediately.
			_, _ = r.Run(context.Background(), preflight.Plan{}, fakeProgressSink{})

			if capturedOpts.MaxConcurrentRuns != tc.bound {
				t.Errorf("suite.Options.MaxConcurrentRuns = %d, want %d; composedSuiteRunner must thread its maxConcurrentRuns field into suite.Options (I12.5)",
					capturedOpts.MaxConcurrentRuns, tc.bound)
			}
		})
	}
}

// TestComposedSuiteRunner_MaxConcurrentRunsZero_SuiteReceivesZero verifies that
// when composedSuiteRunner.maxConcurrentRuns is zero (the convention for
// "use DefaultMaxConcurrentRuns"), suite.Options.MaxConcurrentRuns also receives
// zero — letting the suite apply its own default, not fixing a value here.
//
// RED-phase note: same as above; FAILS until I12.5.
func TestComposedSuiteRunner_MaxConcurrentRunsZero_SuiteReceivesZero(t *testing.T) {
	var capturedOpts suite.Options
	captureSuite := func(opts suite.Options) *suite.Suite {
		capturedOpts = opts
		return suite.New(opts)
	}

	d := minimalDepsForMaxConcurrentRunsTest(t)
	ws := workspace.NewManager(t.TempDir(), fakeClock{})
	r := composedSuiteRunner{
		deps:              d,
		ws:                ws,
		retention:         domain.RetainNever,
		maxConcurrentRuns: 0, // zero: let suite apply DefaultMaxConcurrentRuns
		newSuite:          captureSuite,
	}

	_, _ = r.Run(context.Background(), preflight.Plan{}, fakeProgressSink{})

	// Zero must pass through unchanged, not be replaced by DefaultMaxConcurrentRuns
	// here — the suite is the one that resolves the default from a zero value.
	if capturedOpts.MaxConcurrentRuns != 0 {
		t.Errorf("suite.Options.MaxConcurrentRuns = %d when composedSuiteRunner.maxConcurrentRuns = 0; want 0 so the suite applies DefaultMaxConcurrentRuns rather than the composition root doubling that logic (I12.5)",
			capturedOpts.MaxConcurrentRuns)
	}
}

// ---------------------------------------------------------------------------
// TUI path (tuiSuiteRunner): T12.4
// ---------------------------------------------------------------------------

// TestTUISuiteRunner_MaxConcurrentRuns_ReachesSuiteOptions verifies that
// tuiSuiteRunner threads its maxConcurrentRuns field through to
// suite.Options.MaxConcurrentRuns when it delegates to composedSuiteRunner.
// This is the TUI side of the field-for-field assertion T12.4 requires.
//
// RED-phase note: compiles (tuiSuiteRunner.maxConcurrentRuns is declared as a
// stub field and composedSuiteRunner.maxConcurrentRuns is propagated in Run),
// but FAILS because composedSuiteRunner.Run does not yet copy r.maxConcurrentRuns
// into suite.Options.MaxConcurrentRuns. I12.5 closes this gap.
func TestTUISuiteRunner_MaxConcurrentRuns_ReachesSuiteOptions(t *testing.T) {
	cases := []struct {
		name  string
		bound int
	}{
		{name: "bound 2", bound: 2},
		{name: "bound 5", bound: 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedOpts suite.Options
			captureSuite := func(opts suite.Options) *suite.Suite {
				capturedOpts = opts
				return suite.New(opts)
			}

			d := minimalDepsForMaxConcurrentRunsTest(t)
			ws := workspace.NewManager(t.TempDir(), fakeClock{})
			r := tuiSuiteRunner{
				deps:              d,
				ws:                ws,
				maxConcurrentRuns: tc.bound,
				newSuite:          captureSuite,
			}

			// Run an empty plan through the TUI's per-call interface.
			_, _ = r.Run(context.Background(), preflight.Plan{}, fakeProgressSink{}, domain.RetainNever)

			if capturedOpts.MaxConcurrentRuns != tc.bound {
				t.Errorf("suite.Options.MaxConcurrentRuns = %d via TUI path, want %d; tuiSuiteRunner must propagate maxConcurrentRuns through composedSuiteRunner into suite.Options (I12.5)",
					capturedOpts.MaxConcurrentRuns, tc.bound)
			}
		})
	}
}

// TestTUISuiteRunner_RetentionAndMaxConcurrentRuns_BothReachSuiteOptions
// verifies that tuiSuiteRunner's per-call retention argument and its
// maxConcurrentRuns field both reach suite.Options together. The TUI builds
// composedSuiteRunner fresh per call for retention; this test ensures that
// freshness does not reset maxConcurrentRuns to zero.
//
// RED-phase note: compiles, but FAILS until I12.5.
func TestTUISuiteRunner_RetentionAndMaxConcurrentRuns_BothReachSuiteOptions(t *testing.T) {
	var capturedOpts suite.Options
	captureSuite := func(opts suite.Options) *suite.Suite {
		capturedOpts = opts
		return suite.New(opts)
	}

	d := minimalDepsForMaxConcurrentRunsTest(t)
	ws := workspace.NewManager(t.TempDir(), fakeClock{})
	r := tuiSuiteRunner{
		deps:              d,
		ws:                ws,
		maxConcurrentRuns: 4,
		newSuite:          captureSuite,
	}

	_, _ = r.Run(context.Background(), preflight.Plan{}, fakeProgressSink{}, domain.RetainAlways)

	if capturedOpts.MaxConcurrentRuns != 4 {
		t.Errorf("suite.Options.MaxConcurrentRuns = %d after tuiSuiteRunner.Run with retention=%v, want 4; fresh composedSuiteRunner construction must not drop maxConcurrentRuns (I12.5)",
			capturedOpts.MaxConcurrentRuns, domain.RetainAlways)
	}
	if capturedOpts.Retention != domain.RetainAlways {
		t.Errorf("suite.Options.Retention = %v after tuiSuiteRunner.Run with retention=RetainAll, want RetainAll; retention must still be forwarded from the per-call argument (I12.5)",
			capturedOpts.Retention)
	}
}
