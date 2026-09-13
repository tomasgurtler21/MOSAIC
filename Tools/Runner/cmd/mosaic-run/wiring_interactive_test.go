package main

// wiring_interactive_test.go covers buildInteractiveWiring, the interactive
// frontend's composition seam.
//
// The seam exists so that the two consumers of the shared graceful-stop signal
// -- the tui.Options handed to tui.Run and the session.Deps handed to
// session.New -- are observable as whole values. That shape is load-bearing for
// these tests: both structs are large literals of optional fields, and both
// stop-related fields are silently normalised when omitted (newRootModel
// substitutes a fresh private signal; session.New substitutes a constant-false
// predicate). A test that reached for a bare *session.StopSignal or a bare
// func() bool out of the seam would pass even with either field left unset,
// which is exactly the wiring defect these tests exist to pin.
//
// Every assertion below therefore goes through w.Options and through the
// session.Deps value w.NewDeps produces, never through a separate handle.

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// testRunFolder returns an absolute, run-scoped run folder path under a fresh
// temporary directory. Run-scoped so that the artifact store the seam builds
// takes the quiet path and the recorded debug entries stay uncluttered.
func testRunFolder(t *testing.T, runID string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "Orchestration-"+runID)
}

// resolvedTestIdentity is the resolved run-identity shape: a known run folder
// and no deferred run-selection question.
func resolvedTestIdentity(t *testing.T) tuiRunIdentity {
	t.Helper()
	const runID = "20260101T000000Z-0001"
	return tuiRunIdentity{
		RunID:     runID,
		RunFolder: testRunFolder(t, runID),
		IsNewRun:  true,
		Workflow:  "test-workflow",
	}
}

// deferredTestIdentity is the deferred run-identity shape: no run folder yet,
// and a scan result plus selection question the run-select screen resolves.
// This is the shape that makes the Selection and ScanResult rows of the
// options-completeness table non-trivial.
func deferredTestIdentity(t *testing.T) tuiRunIdentity {
	t.Helper()
	return tuiRunIdentity{
		ScanResult: &runscan.ScanResult{},
		Selection:  &runselect.Question{},
	}
}

// sentinelClaudePath is the pre-scanned executable path the fixture hands the
// seam. It is deliberately not "claude": that value is byte-identical to the
// per-harness default buildAdapter applies to an empty override for the
// claude-code harness, so a fixture using it cannot tell a seam that forwards
// in.ClaudePath from a seam that drops it. The path never has to exist -- no
// adapter constructed in this file spawns a process.
const sentinelClaudePath = "/sentinel/claude-from-input"

// sentinelOverridePath is the per-invocation executable override carried on the
// configuration selection. Distinct from sentinelClaudePath so the two sources
// are distinguishable in an assertion.
const sentinelOverridePath = "/sentinel/claude-from-config"

// testWiringInput assembles a complete interactiveWiringInput around the given
// stop signal. Every process-scoped value is a test double or a temporary
// directory, so the seam can be exercised without a real harness or run folder.
func testWiringInput(t *testing.T, signal *session.StopSignal, identity tuiRunIdentity) interactiveWiringInput {
	t.Helper()
	return interactiveWiringInput{
		ClaudePath:  sentinelClaudePath,
		ProgramRef:  tui.NewProgramRef(),
		Minter:      newTUIRunIdentityMinter(t.TempDir()),
		Identity:    identity,
		StopSignal:  signal,
		Debug:       &recordingLogger{},
		DispatchLog: &sentinelDispatchLogger{},
		Clock:       &realClock{},
	}
}

// fullyWiredConfig exercises every conditional branch of the dependency
// builder, so a Deps completeness check has no field left legitimately nil.
func fullyWiredConfig() screens.ConfigSelection {
	return screens.ConfigSelection{
		Settings: domain.RunSettings{
			Mode:             domain.ExecutionModeOrchestrated,
			ManualResolution: true,
			PreConsultation:  true,
		},
		Harness: "fake",
	}
}

// rawInvokerConfig selects a harness whose adapter implements domain.RawInvoker.
// The fake adapter does not (it has Invoke and no InvokeRaw), so it cannot
// discriminate a dropped raw-invoker extraction. The claude-code adapter does,
// and constructing it spawns no process.
func rawInvokerConfig() screens.ConfigSelection {
	return screens.ConfigSelection{
		Settings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
		Harness: "claude-code",
	}
}

// recordedInput builds a wiring input whose debug logger is a recorder the
// caller keeps a handle on. The seam's non-stop wiring -- artifact store path
// resolution and the unresolved-run-folder mint branch -- is only observable
// through the debug events it emits, because artifact.FileStore exposes no path.
func recordedInput(t *testing.T, identity tuiRunIdentity) (interactiveWiringInput, *recordingLogger) {
	t.Helper()
	rec := &recordingLogger{}
	in := testWiringInput(t, session.NewStopSignal(), identity)
	in.Debug = rec
	return in, rec
}

// findLoggedField returns the value of the named field on the first recorded
// entry carrying the given event, and whether such an entry was found.
func findLoggedField(rec *recordingLogger, event, field string) (string, bool) {
	for _, e := range rec.snapshot() {
		if e.event != event {
			continue
		}
		for _, f := range e.fields {
			if f.Key == field {
				return f.Value, true
			}
		}
	}
	return "", false
}

// isRunScopedFolder reports whether path is an absolute Orchestration-{run_id}
// folder with a valid run_id -- the shape a minted run folder must have for the
// artifact store built beneath it to be run-scoped.
func isRunScopedFolder(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	runID, ok := domain.ParseRunFolder(filepath.Base(path))
	return ok && domain.IsValidRunID(runID)
}

// ---------------------------------------------------------------------------
// T1.1: the composition-root regression test.
//
// Arms the stop signal through the tui.Options value the seam produces -- the
// value runTUIMode passes to tui.Run verbatim -- and observes it through the
// session.Deps value the seam produces, the value sessFactory passes to
// session.New unchanged. Neither side is touched through a bare handle.
//
// The Deps value must be read before session.New consumes it: session.New
// returns the session.Session interface and sessionImpl.deps is unexported, so
// a constructed session cannot be interrogated. That is why the seam yields the
// Deps rather than the session.
// ---------------------------------------------------------------------------

func TestInteractiveWiring_ArmingThroughOptions_IsObservedByProducedDeps(t *testing.T) {
	// Arrange
	signal := session.NewStopSignal()
	w := buildInteractiveWiring(testWiringInput(t, signal, resolvedTestIdentity(t)))

	if w.Options.StopSignal == nil {
		t.Fatal("seam produced tui.Options with a nil StopSignal: the TUI would arm a fresh private signal that no session holds a reference to")
	}
	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0002"), true, "", screens.ConfigSelection{})
	if deps.StopRequested == nil {
		t.Fatal("seam produced session.Deps with a nil StopRequested: session.New would substitute a constant-false predicate and the dispatch loop would never see a stop")
	}
	if deps.StopRequested() {
		t.Fatal("StopRequested() reports true before any stop has been requested")
	}

	// Act -- through the exact value production hands to tui.Run.
	w.Options.StopSignal.Request()

	// Assert -- through the exact value production hands to session.New.
	if !deps.StopRequested() {
		t.Error("arming w.Options.StopSignal did not make w.NewDeps(...).StopRequested() report true: the TUI and the session are not sharing one signal")
	}
}

// TestInteractiveWiring_ResetThroughOptions_IsObservedByProducedDeps pins the
// disarming direction of the same shared instance. The restart paths call
// Reset() on the TUI's signal, and a resumed session must observe that.
func TestInteractiveWiring_ResetThroughOptions_IsObservedByProducedDeps(t *testing.T) {
	// Arrange
	signal := session.NewStopSignal()
	w := buildInteractiveWiring(testWiringInput(t, signal, resolvedTestIdentity(t)))
	if w.Options.StopSignal == nil {
		t.Fatal("seam produced tui.Options with a nil StopSignal")
	}
	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0003"), false, "", screens.ConfigSelection{})
	if deps.StopRequested == nil {
		t.Fatal("seam produced session.Deps with a nil StopRequested")
	}
	w.Options.StopSignal.Request()
	if !deps.StopRequested() {
		t.Fatal("arming through w.Options.StopSignal was not observed by the produced Deps")
	}

	// Act
	w.Options.StopSignal.Reset()

	// Assert
	if deps.StopRequested() {
		t.Error("resetting w.Options.StopSignal left w.NewDeps(...).StopRequested() reporting true: a resumed run would stop immediately")
	}
}

// ---------------------------------------------------------------------------
// T1.2: the shared dependency builder stays unwired.
//
// buildDeps carries no build tag and serves both frontends. The non-interactive
// frontend has no stop UI, so setting StopRequested there would arm a session
// against a signal nothing can reach. The stop field belongs to the interactive
// seam alone.
// ---------------------------------------------------------------------------

func TestBuildDeps_LeavesStopRequestedNil(t *testing.T) {
	cases := []struct {
		name     string
		settings domain.RunSettings
	}{
		{"zero settings", domain.RunSettings{}},
		{"auto", domain.RunSettings{Mode: domain.ExecutionModeAuto}},
		{"orchestrated", domain.RunSettings{Mode: domain.ExecutionModeOrchestrated}},
		{"manual resolution enabled", domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: true}},
		{"pre-consultation enabled", domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true}},
		{"every branch enabled", domain.RunSettings{Mode: domain.ExecutionModeOrchestrated, ManualResolution: true, PreConsultation: true}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			deps := buildDeps(tc.settings, nil, nil, nil, &sentinelDispatchLogger{})
			if deps.StopRequested != nil {
				t.Error("buildDeps set StopRequested: the shared builder must leave the stop dependency unwired so the non-interactive frontend keeps session.New's constant-false default")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T1.3: the seam returns complete values on both sides.
//
// This is the guard against the omitted-optional-field defect recurring. Both
// tui.Options and session.Deps are large literals of nil-safe optional fields:
// a field the seam forgets still compiles, and still passes every other test in
// this stage. So completeness is asserted as an enumeration rather than as a
// prose claim -- one assertion per field the call-site literal sets today.
// ---------------------------------------------------------------------------

func TestInteractiveWiring_ProducesCompleteOptions(t *testing.T) {
	identities := []struct {
		name  string
		build func(*testing.T) tuiRunIdentity
	}{
		{"resolved identity", resolvedTestIdentity},
		{"deferred identity", deferredTestIdentity},
	}

	for _, ic := range identities {
		ic := ic
		t.Run(ic.name, func(t *testing.T) {
			// Arrange
			signal := session.NewStopSignal()
			in := testWiringInput(t, signal, ic.build(t))

			var resolvedRunIDs []string
			in.OnRunIDResolved = func(runID string) {
				resolvedRunIDs = append(resolvedRunIDs, runID)
			}

			// Act
			opts := buildInteractiveWiring(in).Options

			// Assert -- the shared stop dependency.
			if opts.StopSignal != in.StopSignal {
				t.Error("Options.StopSignal is not the signal handed to the seam")
			}

			// Assert -- the values carried from the run identity.
			if opts.Interaction != in.ProgramRef {
				t.Error("Options.Interaction is not the ProgramRef handed to the seam")
			}
			if opts.Selection != in.Identity.Selection {
				t.Errorf("Options.Selection = %v, want the identity's selection question %v", opts.Selection, in.Identity.Selection)
			}
			if opts.ScanResult != in.Identity.ScanResult {
				t.Errorf("Options.ScanResult = %v, want the identity's scan result %v", opts.ScanResult, in.Identity.ScanResult)
			}
			if opts.ResolvedRunID != in.Identity.RunID {
				t.Errorf("Options.ResolvedRunID = %q, want %q", opts.ResolvedRunID, in.Identity.RunID)
			}
			if opts.IsNewRun != in.Identity.IsNewRun {
				t.Errorf("Options.IsNewRun = %v, want %v", opts.IsNewRun, in.Identity.IsNewRun)
			}
			if opts.RecordedWorkflowID != domain.WorkflowID(in.Identity.Workflow) {
				t.Errorf("Options.RecordedWorkflowID = %q, want %q", opts.RecordedWorkflowID, in.Identity.Workflow)
			}
			if opts.InitialRunFolder != in.Identity.RunFolder {
				t.Errorf("Options.InitialRunFolder = %q, want %q", opts.InitialRunFolder, in.Identity.RunFolder)
			}
			if opts.Clock != in.Clock {
				t.Error("Options.Clock is not the clock handed to the seam")
			}

			// Assert -- the function-valued fields. Functions are not
			// comparable in Go beyond nil, so these are checked non-nil and,
			// where the effect is observable, exercised.
			if opts.SessionFactory == nil {
				t.Error("Options.SessionFactory is nil: the TUI could not build a session")
			} else if sess := opts.SessionFactory(testRunFolder(t, "20260101T000000Z-0004"), true, "", screens.ConfigSelection{}); sess == nil {
				t.Error("Options.SessionFactory returned a nil session")
			}
			if opts.MintRunIdentity == nil {
				t.Error("Options.MintRunIdentity is nil: the run-select screen could not mint a new run")
			} else if runID, runFolder := opts.MintRunIdentity(); runID == "" || runFolder == "" {
				t.Errorf("Options.MintRunIdentity() = (%q, %q), want a usable run_id and folder pair", runID, runFolder)
			}
			if opts.OrchestratorDiscoverer == nil {
				t.Error("Options.OrchestratorDiscoverer is nil: orchestrator discovery would not run")
			}
			if opts.ArtifactStoreFactory == nil {
				t.Error("Options.ArtifactStoreFactory is nil: the TUI could not read the run's artifact")
			} else if store := opts.ArtifactStoreFactory(testRunFolder(t, "20260101T000000Z-0005")); store == nil {
				t.Error("Options.ArtifactStoreFactory returned a nil store")
			}

			// OnRunIDResolved is the sharp case: dropping it leaves a
			// deferred-identity run's log files stranded at their out-of-run
			// startup names, and nothing else in this stage would notice.
			if opts.OnRunIDResolved == nil {
				t.Error("Options.OnRunIDResolved is nil: a late-resolved run_id would never reach the log files")
			} else {
				opts.OnRunIDResolved("20260101T000000Z-9999")
				if len(resolvedRunIDs) != 1 || resolvedRunIDs[0] != "20260101T000000Z-9999" {
					t.Errorf("invoking Options.OnRunIDResolved recorded %v, want the callback handed to the seam to have received the run_id", resolvedRunIDs)
				}
			}
		})
	}
}

func TestInteractiveWiring_ProducesCompleteDeps(t *testing.T) {
	// Arrange
	signal := session.NewStopSignal()
	in := testWiringInput(t, signal, resolvedTestIdentity(t))
	w := buildInteractiveWiring(in)

	// Act -- a configuration that exercises every conditional wiring branch, so
	// no field below is legitimately nil.
	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0006"), true, "", fullyWiredConfig())

	// Assert -- the shared stop dependency.
	if deps.StopRequested == nil {
		t.Error("Deps.StopRequested is nil: session.New would silently substitute a constant-false predicate")
	}

	// Assert -- every remaining field the interactive Deps literal sets today.
	fields := []struct {
		name  string
		isNil bool
	}{
		{"Harness", deps.Harness == nil},
		{"Store", deps.Store == nil},
		{"Clock", deps.Clock == nil},
		{"Interact", deps.Interact == nil},
		{"Debug", deps.Debug == nil},
		{"DispatchLog", deps.DispatchLog == nil},
		{"Routing", deps.Routing == nil},
		{"Manual", deps.Manual == nil},
		{"PreConsult", deps.PreConsult == nil},
		{"Approvals", deps.Approvals == nil},
	}
	for _, f := range fields {
		if f.isNil {
			t.Errorf("Deps.%s is nil: the seam must produce a session.Deps that sessFactory hands to session.New unchanged", f.name)
		}
	}

	// The process-scoped loggers must be the instances handed to the seam, not
	// fresh per-invocation ones -- a second log file per session is the same
	// class of defect as a second stop signal.
	if deps.Debug != in.Debug {
		t.Error("Deps.Debug is not the debug logger handed to the seam")
	}
	if deps.DispatchLog != in.DispatchLog {
		t.Error("Deps.DispatchLog is not the dispatch logger handed to the seam")
	}
	if deps.Interact != in.ProgramRef {
		t.Error("Deps.Interact is not the ProgramRef handed to the seam")
	}
}

// ---------------------------------------------------------------------------
// T1.4: every produced Deps observes the same process-scoped signal.
//
// The seam's Deps producer runs more than once per process -- eager placeholder
// construction, config-screen completion, exec-override retry, done-screen
// continue -- and every rebuilt session must observe the same flag. A signal
// constructed per invocation satisfies the single-production test above while
// still leaving every rebuilt session on its own orphan flag. Two productions
// is the smallest shape that distinguishes the two designs.
//
// The arming happens after both productions, so a design that snapshots the
// signal's value at construction time fails here too.
// ---------------------------------------------------------------------------

func TestInteractiveWiring_EveryProducedDeps_ObservesTheSameSignal(t *testing.T) {
	// Arrange
	signal := session.NewStopSignal()
	w := buildInteractiveWiring(testWiringInput(t, signal, resolvedTestIdentity(t)))
	if w.Options.StopSignal == nil {
		t.Fatal("seam produced tui.Options with a nil StopSignal")
	}

	// Two productions with different per-invocation inputs, as the real
	// eager-placeholder-then-config-screen sequence produces.
	first := w.NewDeps(testRunFolder(t, "20260101T000000Z-0007"), true, "", screens.ConfigSelection{})
	second := w.NewDeps(testRunFolder(t, "20260101T000000Z-0008"), false, "Orchestrator.md", fullyWiredConfig())

	if first.StopRequested == nil {
		t.Fatal("first produced Deps has a nil StopRequested")
	}
	if second.StopRequested == nil {
		t.Fatal("second produced Deps has a nil StopRequested")
	}
	if first.StopRequested() || second.StopRequested() {
		t.Fatal("a produced Deps reports StopRequested() true before any stop has been requested")
	}

	// Act -- arm once, through the seam-produced options value.
	w.Options.StopSignal.Request()

	// Assert -- both productions observe it.
	if !first.StopRequested() {
		t.Error("the first produced Deps did not observe the arming: the signal is not process-scoped")
	}
	if !second.StopRequested() {
		t.Error("the second produced Deps did not observe the arming: a session rebuilt after a config change or a retry would be left on an orphan flag")
	}
}

// ---------------------------------------------------------------------------
// The seam's non-stop responsibilities.
//
// buildInteractiveWiring absorbs a substantial amount of wiring that has
// nothing to do with the stop signal: artifact store path resolution from the
// per-invocation run folder, the unresolved-run-folder mint branch, and the
// raw-invoker extraction. All three are physically relocated by the extraction,
// which is when they are most likely to break; none of them is observable
// through a non-nil check on the produced Deps.
// ---------------------------------------------------------------------------

// TestInteractiveWiring_DepsStore_DerivesFromTheRunFolderArgument pins the
// per-invocation run folder to the store the produced Deps carries.
//
// Deps.Store is an opaque artifact.FileStore that exposes no path, so the
// derivation is asserted through the debug event the store builder emits for a
// non-run-scoped path: the event carries the exact path it was handed. A seam
// that ignored its runFolder parameter and built every store from the identity
// captured at construction time -- sending every rebuilt session's artifact I/O
// and COMPLETED marker to the first run's folder -- satisfies every non-nil
// check in this file but fails here.
func TestInteractiveWiring_DepsStore_DerivesFromTheRunFolderArgument(t *testing.T) {
	// Arrange -- a construction-time identity with a run-scoped folder, so the
	// only source of a non-run-scoped path is the per-invocation argument.
	in, rec := recordedInput(t, resolvedTestIdentity(t))
	w := buildInteractiveWiring(in)

	// An absolute but deliberately non-run-scoped folder: the store builder
	// records the path it was given rather than staying quiet.
	argFolder := filepath.Join(t.TempDir(), "not-a-run-scoped-folder")

	// Act
	deps := w.NewDeps(argFolder, true, "", screens.ConfigSelection{})

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
	deps := w.NewDeps("", true, "", screens.ConfigSelection{})

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
// production. Dropping in.ClaudePath sends every --claude-path invocation back
// to the harness's own default executable, because the adapter builder applies
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
			want:     sentinelClaudePath,
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
