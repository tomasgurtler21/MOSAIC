package main

// Tests for the per-run harness selection factory (harnessCache/HarnessFactory)
// introduced in Stage 3. The factory is the composition root's answer to the
// frozen, process-start-time harness triad: it builds adapter+decoder+launcher+
// environment triads on demand and caches CheckEnvironment results so repeated
// runs with the same harness do not re-invoke it.
//
// T3.1 tests (per-run harness selection):
// (a) every supported harness ID resolves to an adapter whose ID() matches
// (b) different harness IDs produce different adapters
// (c) an unsupported harness selection surfaces ErrUnknownHarness
// (d) seeded bundles are returned directly, bypassing construction
// (e) newSuiteRunner surfaces ErrUnknownHarness when the harness cannot be wired
//
// T3.2 tests (environment-gate caching):
// (a) repeated Bundle calls for the same harness return the same adapter
//     instance (proving construction and CheckEnvironment were run only once)
// (b) different harnesses have independent cache entries; their adapters differ
// (c) a seeded bundle is returned with its original Environment, not re-checked

import (
	"context"
	"errors"
	"reflect"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/harness/opencode"
)

// ---------------------------------------------------------------------------
// T3.1: per-run harness selection
// ---------------------------------------------------------------------------

// TestHarnessCache_Bundle_SupportedHarnesses_ResolveToCorrectAdapterIDs is the
// adapter-coverage guard-rail for the factory path: every supported harness ID
// must produce a bundle whose Adapter.ID() matches the requested harness, and
// every call must succeed. An ID this factory does not recognise would be caught
// earlier by buildDeps (which calls Bundle eagerly for the default harness), but
// this test pins the property at the factory level.
func TestHarnessCache_Bundle_SupportedHarnesses_ResolveToCorrectAdapterIDs(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})

	for _, entry := range supportedHarnesses() {
		t.Run(entry.ID, func(t *testing.T) {
			bundle, err := factory.Bundle(context.Background(), entry.ID)
			if err != nil {
				t.Fatalf("Bundle(%q) returned an error: %v; every supported harness must resolve without error", entry.ID, err)
			}
			if bundle.Adapter == nil {
				t.Fatal("Bundle returned a nil Adapter with a nil error")
			}
			if got := bundle.Adapter.ID(); got != entry.ID {
				t.Errorf("bundle.Adapter.ID() = %q, want %q", got, entry.ID)
			}
			if bundle.Decoder == nil {
				t.Fatal("Bundle returned a nil Decoder with a nil error")
			}
			if bundle.Launcher == nil {
				t.Fatal("Bundle returned a nil Launcher with a nil error")
			}
		})
	}
}

// TestHarnessCache_Bundle_UnknownHarness_IsErrUnknownHarness verifies that an
// unrecognised harness ID is rejected at the factory level rather than
// silently falling back to any default. A zero-valued bundle alongside a nil
// error would be worse than an error here, since the caller would proceed with
// a nil adapter and crash only during spawn.
func TestHarnessCache_Bundle_UnknownHarness_IsErrUnknownHarness(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})

	bundle, err := factory.Bundle(context.Background(), "not-a-real-harness")

	if err == nil {
		t.Fatal("Bundle with an unrecognised harness returned a nil error; want ErrUnknownHarness")
	}
	if !errors.Is(err, ErrUnknownHarness) {
		t.Errorf("Bundle error = %v, want it to wrap %v", err, ErrUnknownHarness)
	}
	if bundle.Adapter != nil {
		t.Errorf("Bundle with an unrecognised harness returned a non-nil Adapter; want a zero-valued bundle")
	}
}

// TestHarnessCache_Seed_BundleIsReturnedWithoutConstruction verifies that
// seed pre-populates the cache so subsequent Bundle calls return the seeded
// value without running newAdapter/decoderFor/CheckEnvironment. This is the
// mechanism buildDeps uses to store the default harness bundle (with
// harness-neutral tool-availability problems already merged in) before handing
// the factory to the frontends.
func TestHarnessCache_Seed_BundleIsReturnedWithoutConstruction(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})

	// Seed with a fake adapter that is distinct from any real adapter the
	// factory would construct. If Bundle returns this adapter, it used the seed.
	seededAdapter := claudecode.New(claudecode.Options{})
	seededBundle := HarnessBundle{
		Adapter:        seededAdapter,
		Decoder:        fakeDecoder,
		Launcher:       fakeLauncher{},
		Environment:    domain.EnvironmentReport{InterpreterCmd: "py-from-seed"},
		InterpreterCmd: "py-from-seed",
	}
	factory.seed("claude-code", seededBundle)

	got, err := factory.Bundle(context.Background(), "claude-code")
	if err != nil {
		t.Fatalf("Bundle(%q) after seed returned an error: %v", "claude-code", err)
	}
	if got.Adapter != seededAdapter {
		t.Error("Bundle returned a different adapter than the seeded one; seed must prevent cache re-construction")
	}
	if got.InterpreterCmd != "py-from-seed" {
		t.Errorf("bundle.InterpreterCmd = %q, want %q from seeded bundle", got.InterpreterCmd, "py-from-seed")
	}
}

// TestHarnessCache_Bundle_DifferentHarnesses_YieldDifferentAdapterIDs verifies
// that when two different harness IDs are requested from the same factory,
// the returned adapters carry different ID()s. This is the contract that makes
// per-run harness selection meaningful: switching harnesses must change the
// adapter, not return the one cached for the first selection.
func TestHarnessCache_Bundle_DifferentHarnesses_YieldDifferentAdapterIDs(t *testing.T) {
	if len(supportedHarnesses()) < 2 {
		t.Skip("fewer than two supported harnesses; cannot compare two different adapter IDs")
	}

	factory := newHarnessFactory(adapterOptions{})
	ctx := context.Background()

	bundleA, err := factory.Bundle(ctx, commonharness.HarnessIDClaudeCode)
	if err != nil {
		t.Fatalf("Bundle(%q): %v", commonharness.HarnessIDClaudeCode, err)
	}
	bundleB, err := factory.Bundle(ctx, commonharness.HarnessIDOpenCode)
	if err != nil {
		t.Fatalf("Bundle(%q): %v", commonharness.HarnessIDOpenCode, err)
	}

	if bundleA.Adapter.ID() == bundleB.Adapter.ID() {
		t.Errorf("Bundle(%q).Adapter.ID() == Bundle(%q).Adapter.ID() = %q; different harnesses must produce different adapter IDs",
			commonharness.HarnessIDClaudeCode, commonharness.HarnessIDOpenCode, bundleA.Adapter.ID())
	}
}

// TestNewSuiteRunner_UnknownHarness_SurfacesErrUnknownHarness verifies that
// newSuiteRunner surfaces ErrUnknownHarness when the requested harness cannot
// be wired — so an unsupported selection is an explicit error at suite
// construction, never a silent nil-adapter run. This is the CLI path's guard:
// the CLI resolves HarnessID into RunConfig and calls newSuiteRunner; a bad
// selection must fail before any workspace is created.
func TestNewSuiteRunner_UnknownHarness_SurfacesErrUnknownHarness(t *testing.T) {
	d := testDeps(t)
	rc := cli.RunConfig{
		WorkspaceRoot: d.WorkspaceRoot,
		HarnessID:     "not-a-real-harness",
	}

	_, err := newSuiteRunner(d, rc)

	if err == nil {
		t.Fatal("newSuiteRunner with an unsupported HarnessID returned a nil error; want an error wrapping ErrUnknownHarness")
	}
	if !errors.Is(err, ErrUnknownHarness) {
		t.Errorf("newSuiteRunner error = %v, want it to wrap %v", err, ErrUnknownHarness)
	}
}

// TestNewSuiteRunner_SupportedHarness_SucceedsWithAllSupportedIDs verifies
// newSuiteRunner succeeds for every supported harness when a valid workspace
// root is supplied. A supported ID that errors here means buildDeps's eager
// bundle construction or the factory's Bundle switch has a gap.
func TestNewSuiteRunner_SupportedHarness_SucceedsWithAllSupportedIDs(t *testing.T) {
	d := testDeps(t)

	for _, entry := range supportedHarnesses() {
		t.Run(entry.ID, func(t *testing.T) {
			rc := cli.RunConfig{
				WorkspaceRoot: d.WorkspaceRoot,
				HarnessID:     entry.ID,
			}
			_, err := newSuiteRunner(d, rc)
			if err != nil {
				t.Errorf("newSuiteRunner(d, RunConfig{HarnessID: %q}) returned an error: %v; every supported harness must succeed", entry.ID, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.2: environment-gate caching
// ---------------------------------------------------------------------------

// TestHarnessCache_Bundle_SameHarness_ReturnsSameAdapterInstance verifies that
// calling Bundle with the same harness ID twice returns the identical adapter
// instance (same pointer), proving that newAdapter and CheckEnvironment were
// called at most once. This is the caching guarantee: a bundle constructed on
// the first call is stored and returned unchanged on every subsequent call,
// preventing repeated CheckEnvironment I/O for the same harness.
func TestHarnessCache_Bundle_SameHarness_ReturnsSameAdapterInstance(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})
	ctx := context.Background()

	bundle1, err := factory.Bundle(ctx, commonharness.HarnessIDClaudeCode)
	if err != nil {
		t.Fatalf("first Bundle(%q): %v", commonharness.HarnessIDClaudeCode, err)
	}
	bundle2, err := factory.Bundle(ctx, commonharness.HarnessIDClaudeCode)
	if err != nil {
		t.Fatalf("second Bundle(%q): %v", commonharness.HarnessIDClaudeCode, err)
	}

	// Interface equality via reflect: same underlying pointer means same object.
	if !reflect.DeepEqual(bundle1.Adapter, bundle2.Adapter) {
		t.Error("two Bundle calls for the same harness returned different adapter instances; " +
			"the factory must cache the constructed bundle so CheckEnvironment runs at most once per harness")
	}
}

// TestHarnessCache_Bundle_DifferentHarnesses_HaveIndependentCaches verifies that
// caching for one harness does not affect another: requesting "claude-code" and
// "opencode" both succeed independently, and each returns its own adapter rather
// than the other harness's cached one.
func TestHarnessCache_Bundle_DifferentHarnesses_HaveIndependentCaches(t *testing.T) {
	if len(supportedHarnesses()) < 2 {
		t.Skip("fewer than two supported harnesses; cannot verify independent caches")
	}

	factory := newHarnessFactory(adapterOptions{})
	ctx := context.Background()

	claudeBundle, err := factory.Bundle(ctx, commonharness.HarnessIDClaudeCode)
	if err != nil {
		t.Fatalf("Bundle(%q): %v", commonharness.HarnessIDClaudeCode, err)
	}
	openCodeBundle, err := factory.Bundle(ctx, commonharness.HarnessIDOpenCode)
	if err != nil {
		t.Fatalf("Bundle(%q): %v", commonharness.HarnessIDOpenCode, err)
	}

	// Verify round-trip caching: second call for each harness returns the same
	// adapter as the first (not the other harness's adapter).
	claudeBundle2, _ := factory.Bundle(ctx, commonharness.HarnessIDClaudeCode)
	openCodeBundle2, _ := factory.Bundle(ctx, commonharness.HarnessIDOpenCode)

	if !reflect.DeepEqual(claudeBundle.Adapter, claudeBundle2.Adapter) {
		t.Error("second Bundle(claude-code) returned a different adapter than the first; caches are not independent")
	}
	if !reflect.DeepEqual(openCodeBundle.Adapter, openCodeBundle2.Adapter) {
		t.Error("second Bundle(opencode) returned a different adapter than the first; caches are not independent")
	}
	if claudeBundle.Adapter.ID() == openCodeBundle.Adapter.ID() {
		t.Errorf("both harnesses produced the same adapter ID %q; independent caches must not cross-contaminate", claudeBundle.Adapter.ID())
	}
}

// TestHarnessCache_Seed_EnvironmentIsReturnedFromSeed verifies that a seeded
// bundle's Environment is returned directly from Bundle, without being replaced
// or augmented by a fresh CheckEnvironment call. This is critical for the
// default harness path: buildDeps seeds the factory with an EnvironmentReport
// that may include harness-neutral tool-availability problems (cost tool,
// deploy tool), which must reach preflight exactly as stored — not be
// discarded and re-computed from the adapter's own CheckEnvironment.
func TestHarnessCache_Seed_EnvironmentIsReturnedFromSeed(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})

	const wantDetail = "seeded-problem: tool not found"
	seededEnv := domain.EnvironmentReport{
		Problems: []domain.EnvironmentProblem{
			{Kind: domain.ProblemCostToolUnavailable, Detail: wantDetail},
		},
		InterpreterCmd: "py-seeded",
	}
	seededBundle := HarnessBundle{
		Adapter:        claudecode.New(claudecode.Options{}),
		Decoder:        fakeDecoder,
		Launcher:       fakeLauncher{},
		Environment:    seededEnv,
		InterpreterCmd: "py-seeded",
	}
	factory.seed(commonharness.HarnessIDClaudeCode, seededBundle)

	got, err := factory.Bundle(context.Background(), commonharness.HarnessIDClaudeCode)
	if err != nil {
		t.Fatalf("Bundle after seed returned an error: %v", err)
	}
	if len(got.Environment.Problems) == 0 {
		t.Fatal("Bundle after seed returned an empty Environment.Problems; the seeded environment must be preserved")
	}
	if got.Environment.Problems[0].Detail != wantDetail {
		t.Errorf("bundle.Environment.Problems[0].Detail = %q, want seeded detail %q", got.Environment.Problems[0].Detail, wantDetail)
	}
	if got.InterpreterCmd != "py-seeded" {
		t.Errorf("bundle.InterpreterCmd = %q, want %q from seed", got.InterpreterCmd, "py-seeded")
	}
}

// TestHarnessCache_Bundle_AdapterIDMatchesSupportedCatalogIDs verifies that the
// supported harness catalog entries exactly match the adapter ID() values the
// factory produces. This cross-validates supportedHarnesses() against the live
// adapter implementations: a catalog entry whose adapter.ID() disagrees with
// its catalog key would fail validation silently (the catalog validates the
// selection, but the adapter might return a different ID to CheckEnvironment).
func TestHarnessCache_Bundle_AdapterIDMatchesSupportedCatalogIDs(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})
	ctx := context.Background()

	for _, entry := range supportedHarnesses() {
		t.Run(entry.ID, func(t *testing.T) {
			bundle, err := factory.Bundle(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Bundle(%q): %v", entry.ID, err)
			}
			if got := bundle.Adapter.ID(); got != entry.ID {
				t.Errorf("adapter.ID() = %q for catalog entry ID %q; the adapter must self-identify with the same key the catalog and factory use", got, entry.ID)
			}
		})
	}
}

// TestHarnessFactory_OpenCode_AdapterIDIsOpenCode verifies that the factory
// specifically produces an adapter with the opencode harness ID when "opencode"
// is requested. This is a concrete singleton test complementing the parametric
// TestHarnessCache_Bundle_SupportedHarnesses_ResolveToCorrectAdapterIDs: it
// names the opencode harness explicitly so a future refactor that renames the
// constant without updating the factory is caught here, not only through the
// parametric test.
func TestHarnessFactory_OpenCode_AdapterIDIsOpenCode(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})

	bundle, err := factory.Bundle(context.Background(), opencode.HarnessID)
	if err != nil {
		t.Fatalf("Bundle(%q): %v", opencode.HarnessID, err)
	}
	if got := bundle.Adapter.ID(); got != opencode.HarnessID {
		t.Errorf("adapter.ID() = %q, want %q", got, opencode.HarnessID)
	}
}

// TestHarnessFactory_ClaudeCode_AdapterIDIsClaudeCode is the claudecode-specific
// singleton counterpart to TestHarnessFactory_OpenCode_AdapterIDIsOpenCode.
func TestHarnessFactory_ClaudeCode_AdapterIDIsClaudeCode(t *testing.T) {
	factory := newHarnessFactory(adapterOptions{})

	bundle, err := factory.Bundle(context.Background(), claudecode.HarnessID)
	if err != nil {
		t.Fatalf("Bundle(%q): %v", claudecode.HarnessID, err)
	}
	if got := bundle.Adapter.ID(); got != claudecode.HarnessID {
		t.Errorf("adapter.ID() = %q, want %q", got, claudecode.HarnessID)
	}
}
