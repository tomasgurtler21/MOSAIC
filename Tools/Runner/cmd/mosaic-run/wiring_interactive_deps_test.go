package main

// wiring_interactive_deps_test.go continues wiring_interactive_test.go's
// coverage of buildInteractiveWiring: the composition seam's session.Deps
// construction (artifact store derivation, raw-invoker extraction, harness
// executable resolution) and the shared-debug-logger invariants between
// tui.Options and the produced Deps. See wiring_interactive_test.go for the
// shared fixtures these tests use.

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens/runconfig"
)

func TestInteractiveWiring_DepsStore_DerivesFromTheRunFolderArgument(t *testing.T) {
	// Arrange -- a construction-time identity with a run-scoped folder, so the
	// only source of a non-run-scoped path is the per-invocation argument.
	in, rec := recordedInput(t, resolvedTestIdentity(t))
	w := buildInteractiveWiring(in)

	// An absolute but deliberately non-run-scoped folder: the store builder
	// records the path it was given rather than staying quiet.
	argFolder := filepath.Join(t.TempDir(), "not-a-run-scoped-folder")

	// Act
	deps := w.NewDeps(argFolder, true, "", runconfig.ConfigSelection{})

	// Assert
	if deps.Store == nil {
		t.Fatal("Deps.Store is nil")
	}
	got, found := findLoggedField(rec, domain.EventArtifactPathNonRunScoped, "path")
	if !found {
		t.Fatalf("no %s entry recorded for a non-run-scoped run folder: the store was not built from the runFolder argument", domain.EventArtifactPathNonRunScoped)
	}
	if want := filepath.Join(argFolder, "Orchestration.md"); got != want {
		t.Errorf("store built at %q, want %q: the seam is not deriving the store path from its runFolder argument, so every rebuilt session would write to another run's folder", got, want)
	}
}

// TestInteractiveWiring_UnresolvedRunFolder_MintsAndBuildsAScopedStore drives
// the NewDeps("") boundary -- a real production call, not a defensive
// hypothetical. On a deferred (multi-candidate) identity, identity.RunFolder is
// "", and the eager placeholder construction calls the producer with exactly
// that. The seam must mint a fresh identity, notify, and build the store at the
// minted scoped path; dropping the branch leaves a store on a bare relative path
// where every Create fails, and silently, since the notice is what the branch
// itself emits.
func TestInteractiveWiring_UnresolvedRunFolder_MintsAndBuildsAScopedStore(t *testing.T) {
	// Arrange -- the identity shape that reaches this call in production.
	in, rec := recordedInput(t, deferredTestIdentity(t))
	w := buildInteractiveWiring(in)

	// Act
	deps := w.NewDeps("", true, "", runconfig.ConfigSelection{})

	// Assert
	if deps.Store == nil {
		t.Fatal("Deps.Store is nil for an unresolved run folder")
	}
	minted, found := findLoggedField(rec, domain.EventRunnerError, "path")
	if !found {
		t.Fatalf("no %s entry recorded for an empty run folder: the unresolved-run-folder branch was not taken", domain.EventRunnerError)
	}
	if !isRunScopedFolder(minted) {
		t.Errorf("minted run folder = %q, want an absolute Orchestration-{run_id} folder: the store would be built on a path where every Create fails", minted)
	}
	if _, rejected := findLoggedField(rec, domain.EventArtifactPathRejected, "path"); rejected {
		t.Errorf("%s was recorded: the store was built on a non-absolute path instead of the minted folder", domain.EventArtifactPathRejected)
	}
}

// TestInteractiveWiring_ExtractsTheRawInvokerIntoTheRoutingConsultant covers the
// raw-invoker extraction, an enumerated seam responsibility that the Deps
// completeness check cannot see: the consultant is constructed unconditionally,
// so dropping the type assertion leaves Deps.Routing non-nil and only its
// transport nil -- breaking orchestrator consultation for the whole interactive
// frontend under a green suite.
func TestInteractiveWiring_ExtractsTheRawInvokerIntoTheRoutingConsultant(t *testing.T) {
	// Arrange
	in, _ := recordedInput(t, resolvedTestIdentity(t))
	w := buildInteractiveWiring(in)

	// Act -- a harness whose adapter implements domain.RawInvoker.
	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0009"), true, "", rawInvokerConfig())

	// Assert
	consultant, ok := deps.Routing.(*deviation.OrchestratorConsultant)
	if !ok {
		t.Fatalf("Deps.Routing is %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
	if consultant.Invoker == nil {
		t.Fatal("the routing consultant has a nil Invoker: the raw-invoker extraction was lost, so orchestrator consultation has no transport")
	}
	wantInvoker, ok := deps.Harness.(domain.RawInvoker)
	if !ok {
		t.Fatalf("Deps.Harness is %T, which does not implement domain.RawInvoker: the fixture cannot discriminate the extraction", deps.Harness)
	}
	if consultant.Invoker != wantInvoker {
		t.Error("the routing consultant's Invoker is not the session's own harness adapter: consultation would run through a second, separately constructed transport")
	}
}

// TestInteractiveWiring_HarnessExecutable_PrefersTheConfigOverride covers the
// executable-path selection the seam performs before it builds the harness
// adapter: the per-invocation override carried on the configuration selection
// wins, and the process-scoped pre-scanned path is the fallback.
//
// Both halves are invisible to every other test here, and both fail silently in
// production. Dropping in.ExecutablePath sends every --executable-path invocation
// back to the harness's own default executable, because the adapter builder applies
// that default to an empty override itself -- nothing errors, the wrong binary
// is simply spawned. Dropping cfg.ExecutablePath breaks the exec-override
// recovery screen: a user who supplies a corrected path after a failed spawn
// keeps spawning the old one, so the retry appears to do nothing.
//
// The selection is observed through domain.ExecutableRevealer on the produced
// Deps.Harness rather than through a concrete adapter type. The fake adapter
// does not implement that port (it spawns no process), so these cases use the
// claude-code harness, whose construction spawns nothing either.
func TestInteractiveWiring_HarnessExecutable_PrefersTheConfigOverride(t *testing.T) {
	cases := []struct {
		name     string
		runID    string
		override string
		want     string
	}{
		{
			name:     "no per-invocation override",
			runID:    "20260101T000000Z-0010",
			override: "",
			want:     sentinelExecPath,
		},
		{
			name:     "per-invocation override present",
			runID:    "20260101T000000Z-0011",
			override: sentinelOverridePath,
			want:     sentinelOverridePath,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			in, _ := recordedInput(t, resolvedTestIdentity(t))
			w := buildInteractiveWiring(in)
			cfg := rawInvokerConfig()
			cfg.ExecutablePath = tc.override

			// Act
			deps := w.NewDeps(testRunFolder(t, tc.runID), true, "", cfg)

			// Assert
			revealer, ok := deps.Harness.(domain.ExecutableRevealer)
			if !ok {
				t.Fatalf("Deps.Harness is %T, which does not implement domain.ExecutableRevealer: the fixture cannot observe executable selection", deps.Harness)
			}
			if got := revealer.ExecutablePath(); got != tc.want {
				t.Errorf("produced Deps.Harness spawns %q, want %q: the seam is not selecting the harness executable from the configuration override with the pre-scanned path as fallback", got, tc.want)
			}
		})
	}
}

// TestInteractiveWiring_OptionsArtifactStoreFactory_LogsThroughTheSharedDebugLogger
// pins the TUI artifact-store factory's closure over in.Debug.
//
// The completeness test already asserts the factory is present and returns a
// store, which a closure over a nop logger satisfies just as well. The
// consequence of that substitution is narrow but real: the path diagnostics the
// store builder emits for a folder the TUI cannot write the COMPLETED marker
// into vanish from the one process log, and the failure becomes silent.
func TestInteractiveWiring_OptionsArtifactStoreFactory_LogsThroughTheSharedDebugLogger(t *testing.T) {
	// Arrange -- a run-scoped construction-time identity, so the only source of
	// a non-run-scoped path entry is the factory call below.
	in, rec := recordedInput(t, resolvedTestIdentity(t))
	opts := buildInteractiveWiring(in).Options
	if opts.ArtifactStoreFactory == nil {
		t.Fatal("Options.ArtifactStoreFactory is nil")
	}
	argFolder := filepath.Join(t.TempDir(), "not-a-run-scoped-folder")

	// Act
	if store := opts.ArtifactStoreFactory(argFolder); store == nil {
		t.Fatal("Options.ArtifactStoreFactory returned a nil store")
	}

	// Assert
	got, found := findLoggedField(rec, domain.EventArtifactPathNonRunScoped, "path")
	if !found {
		t.Fatalf("no %s entry recorded: the artifact store factory is not logging through the debug logger handed to the seam, so the TUI's marker-write path diagnostics are lost", domain.EventArtifactPathNonRunScoped)
	}
	if want := filepath.Join(argFolder, "Orchestration.md"); got != want {
		t.Errorf("factory built a store at %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// The TUI and the session share one debug logger instance.
//
// Both halves of the graceful-stop lifecycle are logged: the TUI records the
// confirmation gate and the arming, the session records which dispatch
// checkpoint observed the signal. They are only reconstructible as one sequence
// if they land in one ordered log, which means one logger instance, not two
// sinks whose entries must afterwards be correlated by timestamp.
//
// As with the stop signal, both sides are observed through the whole values the
// seam yields -- the tui.Options handed to tui.Run and the session.Deps handed
// to session.New. Comparing two bare handles taken from one call would pass
// even with either literal's field left unset, and both fields are silently
// normalised when omitted, so the unwired case would look identical.
// ---------------------------------------------------------------------------

func TestInteractiveWiring_OptionsAndDeps_ShareTheSameDebugLogger(t *testing.T) {
	// Arrange
	in, rec := recordedInput(t, resolvedTestIdentity(t))

	// Act
	w := buildInteractiveWiring(in)
	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0007"), true, "", fullyWiredConfig())

	// Assert -- each side carries a logger at all. A nil field is normalised
	// downstream (newRootModel and session.New both substitute a nop logger),
	// so an omitted field is silent everywhere else.
	if w.Options.Debug == nil {
		t.Fatal("Options.Debug is nil: the TUI would log the stop lifecycle to a nop logger, " +
			"and the gate entries would be absent from the process log entirely")
	}
	if deps.Debug == nil {
		t.Fatal("the produced Deps.Debug is nil: session.New would substitute a nop logger")
	}

	// Assert -- and it is the same instance on both sides, and the instance the
	// seam was handed.
	if w.Options.Debug != in.Debug {
		t.Error("Options.Debug is not the debug logger handed to the seam: the TUI-side stop " +
			"entries would land in a different sink from the session-side ones")
	}
	if deps.Debug != in.Debug {
		t.Error("the produced Deps.Debug is not the debug logger handed to the seam")
	}
	if w.Options.Debug != deps.Debug {
		t.Error("Options.Debug and Deps.Debug are different instances: the stop lifecycle would " +
			"span two logs and could only be reconstructed by correlating them on timestamps")
	}

	// Assert -- writes through both sides land in one ordered record. This is
	// the property the shared instance exists to provide, asserted rather than
	// inferred from the pointer comparisons above.
	w.Options.Debug.Log(domain.EventTUIStopSignalArmed, "tui-side entry")
	deps.Debug.Log(domain.EventSessionStopObserved, "session-side entry",
		domain.F("checkpoint", session.StopCheckpointEngineStep))

	var order []string
	for _, e := range rec.snapshot() {
		switch e.event {
		case domain.EventTUIStopSignalArmed, domain.EventSessionStopObserved:
			order = append(order, e.event)
		}
	}
	want := []string{domain.EventTUIStopSignalArmed, domain.EventSessionStopObserved}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("the one recorder saw the stop entries as %v, want %v -- both halves of the "+
			"lifecycle must reach a single ordered log", order, want)
	}
}
