// Package runner owns one attempt of one test: setup, execution under
// supervision, snapshot and teardown that always happens.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mosaic-agent-test/internal/concurrency"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/orchstate"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/protocolcheck"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
	"mosaic-agent-test/internal/workspace"
)

// protocolVersion is the Communication Protocol version evidence-building
// checks messages against. Named rather than threaded through as a setting:
// no test definition declares one, and protocolcheck's rules do not yet vary
// by version.
const protocolVersion protocolcheck.Version = "1.10"

// Deps are the collaborators one attempt needs. Every one is a port or a
// pure package: this package spawns nothing directly and names no harness.
type Deps struct {
	Workspaces workspace.Manager
	Adapter    domain.HarnessAdapter
	Launcher   domain.SubjectLauncher
	Fixtures   fixtures.Resolver
	Effects    sideeffects.Applier
	Cost       domain.CostProvider
	Clock      domain.Clock
	Progress   domain.ProgressSink

	// SelfPath and LoggerBundleDir are passed through to the adapter's
	// provision request; the runner does not interpret either.
	SelfPath        string
	LoggerBundleDir string

	// InterpreterCmd is the interpreter resolved by the adapter's
	// CheckEnvironment during preflight and carried here by the composition
	// root. The runner does not interpret it; it populates
	// ProvisionRequest.InterpreterCmd verbatim.
	//
	// Empty is legal and means "this adapter runs no interpreter". It is
	// never a default, a fallback or a literal: no interpreter name appears
	// anywhere in this package.
	InterpreterCmd string

	// Deploy renders the subject and each declared stub collaborator into the
	// sandbox. It is a port like every other field here: the runner spawns
	// nothing itself and names no harness, and it passes the harness id
	// through from the adapter without interpreting it.
	Deploy domain.AgentDeployer
}

// Request is what one attempt of one test needs to run.
type Request struct {
	Key      domain.RunKey
	Test     preflight.ResolvedTest
	Settings domain.RunSettings

	// Retention is the run-level sandbox retention policy, decided by the
	// frontend and passed in. The runner never reads it from the
	// environment.
	Retention domain.RetentionPolicy
}

// Run executes one attempt end to end and tears down on every exit path,
// including a panic inside execution and a setup that failed partway.
//
// It returns evidence on every path on which an attempt began. The error
// return is reserved for a fault that prevented the attempt from starting at
// all; teardown has still run when it is returned.
func Run(ctx context.Context, d Deps, req Request) (evidence domain.RunEvidence, runErr error) {
	start := d.Clock.Now()

	var (
		sb           domain.Sandbox
		ledger       SetupLedger
		sandboxKnown bool
	)

	defer func() {
		if r := recover(); r != nil {
			if sandboxKnown {
				_, _ = Teardown(d, ledger, AttemptOutcome{Policy: req.Retention, Failed: true})
			}
			evidence = domain.RunEvidence{}
			runErr = fmt.Errorf("runner: recovered panic: %v", r)
		}
	}()

	var setupErr error
	sb, ledger, setupErr = setup(ctx, d, req)
	if ledger.SandboxRoot != "" {
		sandboxKnown = true
	}
	if setupErr != nil {
		if sandboxKnown {
			_, _ = Teardown(d, ledger, AttemptOutcome{Policy: req.Retention, Failed: true})
		}
		return domain.RunEvidence{}, setupErr
	}

	subject := subjectWithRunIDPrelude(req.Test.Definition.Subject, req.Key.RunID)

	plan, planErr := d.Adapter.SpawnPlan(ctx, subject, ledger.Provisioning)
	if planErr != nil {
		_, _ = Teardown(d, ledger, AttemptOutcome{Policy: req.Retention, Failed: true})
		return domain.RunEvidence{}, fmt.Errorf("runner: obtaining spawn plan: %w", planErr)
	}

	res, launchErr := superviseExecution(ctx, d, sb, plan, req.Settings)

	snap := TakeSnapshot(d, sb, res)
	// Carry the subject version captured at render time into the snapshot, so
	// BuildEvidence can place it in RunEvidence. TakeSnapshot has no access to
	// the ledger, so the copy happens here where both are in scope.
	snap.SubjectVersion = ledger.SubjectVersion

	costReport, costErr := d.Cost.Cost(ctx, domain.CostQuery{LogRoot: snap.LogRoot, RunID: req.Key.RunID})
	if costErr != nil {
		costReport = domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: %v", costErr),
		}
	}

	retention, _ := Teardown(d, ledger, AttemptOutcome{Policy: req.Retention, Failed: launchErr != nil})

	dur := d.Clock.Now().Sub(start)
	evidence = BuildEvidence(req, snap, costReport, dur)
	if retention.Retained {
		evidence.RetainedSandboxPath = retention.Path
	}

	if launchErr != nil {
		return evidence, launchErr
	}
	return evidence, nil
}

// setup creates the sandbox and populates it, appending to the ledger before
// each thing it creates is used, so a failure between two steps still leaves
// the ledger describing what exists.
func setup(ctx context.Context, d Deps, req Request) (domain.Sandbox, SetupLedger, error) {
	var ledger SetupLedger

	// 1. Create the sandbox.
	sb, err := d.Workspaces.Create(req.Key)
	if err != nil {
		return domain.Sandbox{}, ledger, fmt.Errorf("runner: creating sandbox: %w", err)
	}
	ledger.SandboxRoot = sb.Root

	// 2. Provision the subject and each declared stub collaborator into the
	// sandbox through the deployment port. The path taken depends on the
	// definition's provisioning path:
	//
	//  - Catalogue path: one Deploy call provisions the subject and every
	//    workflow-referenced stub agent in one shot. The definition path is
	//    derived from what Deploy reported; the subject version is recorded
	//    as empty because Deploy reports no version.
	//
	//  - Stub-agents path: the existing per-agent Render loop is unchanged.
	//
	// In both cases each result is appended to the ledger before the next
	// step begins, so a partial failure still leaves the ledger describing
	// exactly what exists and teardown can remove exactly that.
	switch req.Test.Definition.ProvisioningPath() {
	case domain.ProvisioningCatalog:
		if d.Deploy == nil {
			// Not supplied means not checked; skip the deploy step gracefully.
			break
		}
		result, deployErr := d.Deploy.Deploy(ctx, domain.DeployRequest{
			HarnessID:     d.Adapter.ID(),
			WorkspaceRoot: sb.SubjectDir,
			Workflows:     req.Test.Definition.Subject.Workflows,
			TierModels:    buildTierModelMap(req.Test.Definition.Subject),
		})
		if deployErr != nil {
			return sb, ledger, fmt.Errorf("runner: deploying catalogue path: %w", deployErr)
		}
		// Record all deployed agent paths before the next step so teardown
		// has an accurate ledger even after a later Provision failure.
		for _, agent := range result.Agents {
			ledger.Provisioning.Files = append(ledger.Provisioning.Files, agent.DestinationPath)
		}
		ledger.Provisioning.Dirs = append(ledger.Provisioning.Dirs, result.CreatedDirectories...)

		// The subject version is empty on the catalogue path: Deploy reports
		// no source version and we must not reconstruct one.
		ledger.SubjectVersion = ""

		// Derive the subject's definition path from the DeployedAgent entry
		// whose Key matches the declared catalogue agent key. No positional
		// assumption; key-based matching is the contract.
		found := false
		for _, agent := range result.Agents {
			if agent.Key != req.Test.Definition.Subject.CatalogAgentKey {
				continue
			}
			rel, relErr := filepath.Rel(sb.SubjectDir, agent.DestinationPath)
			if relErr != nil {
				return sb, ledger, fmt.Errorf("runner: subject destination %q is not relative to sandbox %q: %w",
					agent.DestinationPath, sb.SubjectDir, relErr)
			}
			req.Test.Definition.Subject.DefinitionPath = rel
			found = true
			break
		}
		if !found {
			return sb, ledger, fmt.Errorf("runner: deploy result contains no agent matching subject key %q",
				req.Test.Definition.Subject.CatalogAgentKey)
		}

	default:
		// Stub-agents path (or neither declared): the existing per-agent
		// Render loop, unchanged from before this stage.
		if d.Deploy != nil && req.Test.Definition.Subject.CatalogAgentKey != "" {
			result, renderErr := d.Deploy.Render(ctx, domain.RenderAgentRequest{
				CatalogAgentKey: req.Test.Definition.Subject.CatalogAgentKey,
				HarnessID:       d.Adapter.ID(),
				WorkspaceRoot:   sb.SubjectDir,
				Model:           req.Test.Definition.Subject.Model,
				Workflows:       req.Test.Definition.Subject.Workflows,
			})
			if renderErr != nil {
				return sb, ledger, fmt.Errorf("runner: rendering subject %q: %w",
					req.Test.Definition.Subject.CatalogAgentKey, renderErr)
			}
			// Record before using so teardown sees the path even if a later step fails.
			ledger.Provisioning.Files = append(ledger.Provisioning.Files, result.DestinationPath)
			ledger.Provisioning.Dirs = append(ledger.Provisioning.Dirs, result.CreatedDirectories...)

			// Capture the subject's source version at render time.
			ledger.SubjectVersion = result.SourceVersion

			// Resolve the subject's spawn-time definition path from the reported
			// destination.
			rel, relErr := filepath.Rel(sb.SubjectDir, result.DestinationPath)
			if relErr != nil {
				return sb, ledger, fmt.Errorf("runner: subject destination %q is not relative to sandbox %q: %w",
					result.DestinationPath, sb.SubjectDir, relErr)
			}
			req.Test.Definition.Subject.DefinitionPath = rel
		}

		for _, stub := range req.Test.Definition.StubAgents {
			if d.Deploy == nil {
				break
			}
			result, renderErr := d.Deploy.Render(ctx, domain.RenderAgentRequest{
				SourcePath:    stub.SourcePath,
				HarnessID:     d.Adapter.ID(),
				WorkspaceRoot: sb.SubjectDir,
			})
			if renderErr != nil {
				return sb, ledger, fmt.Errorf("runner: rendering stub %q: %w",
					stub.Identity.AgentIdentity, renderErr)
			}
			// Record before moving to the next stub.
			ledger.Provisioning.Files = append(ledger.Provisioning.Files, result.DestinationPath)
			ledger.Provisioning.Dirs = append(ledger.Provisioning.Dirs, result.CreatedDirectories...)
		}
	}

	// 3. Seed declared files, resolving $ref through the fixture resolver,
	// with {run_id} expanded in the declared path.
	for _, sf := range req.Test.Definition.SeedFiles {
		rel, err := seedFile(d, sb, req.Key, sf)
		if err != nil {
			return sb, ledger, fmt.Errorf("runner: seeding file %q: %w", sf.Path, err)
		}
		ledger.SeededFiles = append(ledger.SeededFiles, rel)
	}

	// 4. Write the active stub registry and the parallel-group document into
	// the control directory.
	if err := writeControlDocument(sb.ActiveRegistryPath(), req.Test.Registry); err != nil {
		return sb, ledger, fmt.Errorf("runner: writing active registry: %w", err)
	}
	ledger.ControlFiles = append(ledger.ControlFiles, domain.ActiveRegistryFileName)

	if err := writeControlDocument(sb.ParallelGroupsPath(), req.Test.Definition.ParallelGroups); err != nil {
		return sb, ledger, fmt.Errorf("runner: writing parallel groups: %w", err)
	}
	ledger.ControlFiles = append(ledger.ControlFiles, domain.ParallelGroupsFileName)

	// 5. Initialize run state with the declared early-exit threshold and
	// turn limit.
	store := runstate.NewStore(sb.ControlDir, d.Clock)
	state := domain.RunState{
		TestID:    req.Test.Definition.ID,
		RunNumber: req.Key.RunNumber,
		RunID:     req.Key.RunID,
	}
	if req.Settings.StopAfterInvocations != nil {
		state.EarlyExitThreshold = *req.Settings.StopAfterInvocations
	}
	if req.Settings.TurnLimit != nil {
		state.TurnLimit = *req.Settings.TurnLimit
	}
	if err := store.Initialize(state); err != nil {
		return sb, ledger, fmt.Errorf("runner: initializing run state: %w", err)
	}
	ledger.ControlFiles = append(ledger.ControlFiles, domain.StateFileName)

	// 6. Drive the adapter's provisioning.
	provReq := domain.ProvisionRequest{
		Sandbox:         sb,
		Subject:         subjectWithRunIDPrelude(req.Test.Definition.Subject, req.Key.RunID),
		LoggerBundleDir: d.LoggerBundleDir,
		InterceptorPath: d.SelfPath,
		InterceptorArgs: []string{domain.InterceptorSubcommand},
		InterpreterCmd:  d.InterpreterCmd,
	}
	provisioning, err := d.Adapter.Provision(ctx, provReq)
	if err != nil {
		return sb, ledger, fmt.Errorf("runner: provisioning: %w", err)
	}
	// Merge the adapter's Provisioning into the ledger rather than replacing
	// it, so rendered paths recorded before Provision are preserved. Files
	// the deploy tool wrote are invisible to the adapter; if they were not
	// recorded before this merge they would be lost and survive Deprovision.
	ledger.Provisioning.Files = append(ledger.Provisioning.Files, provisioning.Files...)
	ledger.Provisioning.Dirs = append(ledger.Provisioning.Dirs, provisioning.Dirs...)
	ledger.Provisioning.Sandbox = provisioning.Sandbox
	ledger.Provisioning.ScopeFindings = provisioning.ScopeFindings
	ledger.Provisioning.Sensitive = provisioning.Sensitive

	_ = SaveSetupLedger(sb.SetupLedgerPath(), ledger)

	return sb, ledger, nil
}

// buildTierModelMap constructs the two-entry tier-model map the catalogue path
// sends on both the setup Deploy and the preflight dry-run Deploy. Both entries
// are always present:
//
//   - TierTestSubject → Subject.Model
//   - TierTestStub    → Subject.StubModel, falling back to Subject.Model when
//     StubModel is empty
//
// The fallback is deliberate: an absent StubModel yields a working run that
// costs more (the expensive subject model is used for every stub too), rather
// than a broken run. An empty Subject.Model propagates as empty for both tiers.
func buildTierModelMap(subject domain.SubjectUnderTest) map[string]string {
	stubModel := subject.StubModel
	if stubModel == "" {
		stubModel = subject.Model
	}
	return map[string]string{
		domain.TierTestSubject: subject.Model,
		domain.TierTestStub:    stubModel,
	}
}

// subjectWithRunIDPrelude returns a copy of subject whose opening message
// has domain.RunIDPrelude(runID) prepended, so the logger bundle's
// extraction recovers the run id from the prompt the same way real MOSAIC
// orchestration runs carry it. subject itself, and therefore the caller's
// Request, is never mutated.
func subjectWithRunIDPrelude(subject domain.SubjectUnderTest, runID string) domain.SubjectUnderTest {
	out := subject
	out.OpeningMessage = domain.RunIDPrelude(runID) + subject.OpeningMessage
	return out
}

// seedFile resolves and writes one declared seed file beneath the subject
// directory, returning its path relative to the subject directory.
func seedFile(d Deps, sb domain.Sandbox, key domain.RunKey, sf domain.SeedFile) (string, error) {
	var content []byte
	if sf.Ref != "" {
		resolved, err := d.Fixtures.Resolve(sf.Ref)
		if err != nil {
			return "", err
		}
		content = resolved
	} else {
		content = []byte(sf.Content)
	}

	expandedPath := strings.ReplaceAll(sf.Path, domain.RunIDPlaceholder, key.RunID)
	rel, full, err := resolveSubjectPath(sb.SubjectDir, expandedPath)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("creating directory for %q: %w", sf.Path, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return "", fmt.Errorf("writing %q: %w", sf.Path, err)
	}
	return rel, nil
}

// resolveSubjectPath resolves path against the subject directory and refuses
// anything that would escape it.
func resolveSubjectPath(subjectDir, path string) (rel string, full string, err error) {
	absSubject, err := filepath.Abs(subjectDir)
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	full = filepath.Join(absSubject, cleaned)

	rel, err = filepath.Rel(absSubject, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q escapes the subject directory", path)
	}
	return rel, full, nil
}

// writeControlDocument marshals v as indented JSON and writes it to path,
// creating the destination directory if needed.
func writeControlDocument(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SetupLedger records exactly what setup created, so teardown removes
// exactly that and nothing else.
type SetupLedger struct {
	SandboxRoot  string
	Provisioning domain.Provisioning
	SeededFiles  []string // relative to the subject dir, in creation order
	ControlFiles []string // relative to the control dir, in creation order
	Effects      domain.EffectLedger

	// SubjectVersion is the subject's declared source version, captured from
	// the deployment port's result at render time. Carried here so Run can
	// copy it into the Snapshot and ultimately into BuildEvidence without
	// threading it through as an additional return value.
	SubjectVersion string
}

// SaveSetupLedger persists l at path.
func SaveSetupLedger(path string, l SetupLedger) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("runner: marshaling setup ledger: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("runner: creating setup ledger directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("runner: writing setup ledger %q: %w", path, err)
	}
	return nil
}

// LoadSetupLedger reads a previously persisted setup ledger.
func LoadSetupLedger(path string) (SetupLedger, error) {
	var l SetupLedger
	data, err := os.ReadFile(path)
	if err != nil {
		return l, fmt.Errorf("runner: reading setup ledger %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return SetupLedger{}, fmt.Errorf("runner: parsing setup ledger %q: %w", path, err)
	}
	return l, nil
}

// RetentionOutcome tells Run's caller what happened to the sandbox, so the
// report can name a path a user can actually open.
type RetentionOutcome struct {
	Retained bool
	// Path is the retained sandbox root; empty when Retained is false.
	Path string
	// Scrubbed lists the sensitive paths removed before retention, so a test
	// asserts on the scrub rather than on its absence.
	Scrubbed []string
}

// AttemptOutcome is what the caller knows and Teardown cannot observe: which
// policy this attempt runs under, and whether the attempt failed. It is a
// parameter rather than a field on SetupLedger, because SetupLedger is
// persisted to disk at the end of setup, before any outcome exists — a
// Retention or AttemptFailed field on it would be written to disk
// permanently stale, inside precisely the retained sandbox a diagnosis
// reads. Run supplies AttemptOutcome{Policy: req.Retention, Failed: ...} at
// each of its four Teardown call sites.
type AttemptOutcome struct {
	// Policy is req.Retention, carried unchanged from the frontend. Teardown
	// never reads a policy from the environment or from disk.
	Policy domain.RetentionPolicy

	// Failed is true exactly when the attempt failed or never started: the
	// panic-recovery exit, the setup-failure exit, the spawn-plan-failure
	// exit, and a normal exit whose supervision returned an error. It is
	// false only on a normal exit that produced evidence without error.
	Failed bool
}

// Teardown honours o.Policy on every exit path, including panic recovery:
//
//	RetainNever     → Deprovision, then delete the sandbox root. Today's behaviour.
//	RetainAlways    → Skip Deprovision, scrub Provisioning.Sensitive, keep the root.
//	RetainOnFailure → Behaves as RetainAlways when o.Failed; as RetainNever otherwise.
//
// Deprovision is skipped whenever the sandbox is retained: deprovisioning
// removes the provisioned configuration, which is precisely the artifact a
// diagnosis needs. Scrubbing is unconditional whenever a sandbox is left on
// disk: a retained sandbox never contains live harness credential material.
func Teardown(d Deps, l SetupLedger, o AttemptOutcome) (RetentionOutcome, error) {
	if shouldRetain(o) {
		scrubbed, scrubErr := scrubSensitive(l.Provisioning.Sensitive)
		if scrubErr != nil {
			return RetentionOutcome{}, fmt.Errorf("runner: teardown: scrubbing retained sandbox: %w", scrubErr)
		}
		return RetentionOutcome{Retained: true, Path: l.SandboxRoot, Scrubbed: scrubbed}, nil
	}

	var deprovisionErr error
	if d.Adapter != nil {
		deprovisionErr = d.Adapter.Deprovision(context.Background(), l.Provisioning)
	}

	var teardownErr error
	if l.SandboxRoot != "" && d.Workspaces != nil {
		teardownErr = d.Workspaces.Teardown(domain.Sandbox{Root: l.SandboxRoot})
	}

	switch {
	case deprovisionErr != nil && teardownErr != nil:
		return RetentionOutcome{}, fmt.Errorf("runner: teardown: deprovision failed: %v; sandbox removal failed: %v", deprovisionErr, teardownErr)
	case deprovisionErr != nil:
		return RetentionOutcome{}, fmt.Errorf("runner: teardown: deprovision failed: %w", deprovisionErr)
	case teardownErr != nil:
		return RetentionOutcome{}, fmt.Errorf("runner: teardown: %w", teardownErr)
	default:
		return RetentionOutcome{}, nil
	}
}

// shouldRetain resolves o into a retain/remove decision: RetainAlways always
// retains, RetainOnFailure retains exactly when the attempt failed, and
// RetainNever (and any unrecognised policy) never retains.
func shouldRetain(o AttemptOutcome) bool {
	switch o.Policy {
	case domain.RetainAlways:
		return true
	case domain.RetainOnFailure:
		return o.Failed
	default:
		return false
	}
}

// scrubSensitive removes every path in paths, so a retained sandbox never
// contains live harness credential material, and returns the paths it
// actually removed (skipping empty entries) for the caller to report.
func scrubSensitive(paths []string) ([]string, error) {
	var scrubbed []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return scrubbed, fmt.Errorf("removing %q: %w", p, err)
		}
		scrubbed = append(scrubbed, p)
	}
	return scrubbed, nil
}

// SentinelPollInterval is how often the supervisor checks for the
// early-exit sentinel.
const SentinelPollInterval = 250 * time.Millisecond

// Termination is why execution ended, decided by the supervisor rather than
// inferred afterwards from the envelope.
type Termination struct {
	Disposition domain.RunDisposition
	At          time.Time
	Detail      string
}

// launchOutcome carries one call to the launcher's result off its own
// goroutine, recovering a panic into an error rather than letting it escape
// and crash the process.
type launchOutcome struct {
	res domain.SubjectResult
	err error
}

// superviseExecution starts the subject by handing the adapter's plan to the
// launcher port and supervises it for the three terminating conditions: the
// early-exit sentinel appearing, the declared timeout elapsing, and the
// harness reporting its own turn limit. The supervisor's own decision — the
// sentinel was observed — wins over whatever the launcher's decoded result
// reports, because a cancellation caused by the sentinel would otherwise
// decode as a timeout.
func superviseExecution(ctx context.Context, d Deps, sb domain.Sandbox, plan domain.SpawnPlan, settings domain.RunSettings) (domain.SubjectResult, error) {
	// Fill in the sentinel path the supervisor itself watches, when the
	// adapter left it unstated. An adapter that declared one explicitly is
	// left untouched.
	if plan.EarlyExitSentinel == "" {
		plan.EarlyExitSentinel = sb.EarlyExitSentinelPath()
	}

	base := ctx
	if settings.Timeout != nil {
		var cancelTimeout context.CancelFunc
		base, cancelTimeout = context.WithTimeout(ctx, *settings.Timeout)
		defer cancelTimeout()
	}
	launchCtx, cancelSentinel := context.WithCancel(base)
	defer cancelSentinel()

	sentinelHit := make(chan struct{})
	stopPoll := make(chan struct{})
	go watchSentinel(plan.EarlyExitSentinel, sentinelHit, stopPoll)

	resultCh := make(chan launchOutcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- launchOutcome{
					res: domain.SubjectResult{Disposition: domain.DispositionSpawnFailed},
					err: fmt.Errorf("runner: recovered panic during execution: %v", r),
				}
			}
		}()
		res, err := d.Launcher.Launch(launchCtx, plan)
		resultCh <- launchOutcome{res: res, err: err}
	}()

	var out launchOutcome
	sentinelWon := false
	select {
	case <-sentinelHit:
		sentinelWon = true
		cancelSentinel()
		out = <-resultCh
	case out = <-resultCh:
	}
	close(stopPoll)

	switch {
	case sentinelWon:
		out.res.Disposition = domain.DispositionEarlyExit
		out.err = nil
	case settings.Timeout != nil && errors.Is(base.Err(), context.DeadlineExceeded):
		// The launcher's own returned error can never be trusted for this:
		// domain.SubjectLauncher's contract (a subject that started and was
		// then cancelled still yields a result rather than an error) means a
		// conforming launcher swallows the cancellation into its decoded
		// result and returns a nil error on this path — internal/launch.Launcher
		// does exactly that. base is the context this supervisor itself put
		// the declared timeout on, so its own Err() is the one signal that
		// survives the launcher's decoding: DeadlineExceeded here can only
		// mean base's deadline fired, since the sentinel path above cancels
		// launchCtx directly and never reaches base.
		out.res.Disposition = domain.DispositionTimedOut
		out.err = nil
	}

	return out.res, out.err
}

// watchSentinel polls path for existence every SentinelPollInterval, closing
// hit the first time it finds one. It stops polling as soon as stop closes.
// A poll rather than a watch, because file-change notification semantics
// differ across platforms and a missed notification would hang the run past
// its usefulness.
func watchSentinel(path string, hit chan<- struct{}, stop <-chan struct{}) {
	if path == "" {
		return
	}
	ticker := time.NewTicker(SentinelPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				close(hit)
				return
			}
		}
	}
}

// Snapshot is everything the verdict engine will need, captured while it
// still exists.
type Snapshot struct {
	Files                []string // subject-dir-relative listing, sorted
	Orchestration        domain.OrchestrationState
	OrchestrationProblem string
	Records              []domain.LogRecord
	LogReport            invlog.ReadReport
	SubjectResult        domain.SubjectResult
	LogRoot              string // captured so cost can be read after teardown

	// LogsProduced reports whether LogRoot held any log records, populated
	// from the actual log tree by TakeSnapshot via logRootHasFiles.
	LogsProduced bool

	// SubjectVersion is the subject's declared source version, captured from
	// the deployment port's result during setup and copied here from the
	// SetupLedger so BuildEvidence can carry it into RunEvidence without
	// re-deriving it. Empty when the source declared no version.
	SubjectVersion string
}

// TakeSnapshot captures everything the verdict engine will need, before
// anything is removed.
func TakeSnapshot(d Deps, s domain.Sandbox, res domain.SubjectResult) Snapshot {
	snap := Snapshot{
		Files:         listSubjectFiles(s.SubjectDir),
		SubjectResult: res,
		LogRoot:       s.LogRoot(),
		LogsProduced:  logRootHasFiles(s.LogRoot()),
	}

	records, report, err := invlog.NewLog(s.InvocationLogPath()).Read()
	if err == nil {
		snap.Records = records
		snap.LogReport = report
	}

	orchState, orchErr := orchstate.ParseFile(orchestrationDocPath(s))
	if orchErr != nil {
		snap.OrchestrationProblem = orchErr.Error()
	} else {
		snap.Orchestration = orchState
	}

	return snap
}

// orchestrationDocPath returns the location of the subject's own
// orchestration document, following the Orchestration-{run_id}/Orchestration.md
// convention seed files and real MOSAIC runs both use.
func orchestrationDocPath(s domain.Sandbox) string {
	return filepath.Join(s.SubjectDir, "Orchestration-"+s.Key.RunID, "Orchestration.md")
}

// logRootHasFiles reports whether root contains at least one file, so a run
// that started but produced no logs at all is distinguishable from one that
// did. A missing root counts as no files, not an error: a run whose subject
// never wrote anything under OrchestrationLogs never creates the directory.
func logRootHasFiles(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if found {
			return filepath.SkipAll
		}
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		found = true
		return nil
	})
	return found
}

// listSubjectFiles returns every file beneath subjectDir, relative to it,
// sorted.
func listSubjectFiles(subjectDir string) []string {
	var files []string
	_ = filepath.WalkDir(subjectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(subjectDir, path)
		if relErr != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files
}

// BuildEvidence assembles the verdict engine's input from a snapshot plus
// the derived measurements. Pure, so the assembly step is testable and a
// stored snapshot can be re-evaluated without re-spawning an agent.
func BuildEvidence(req Request, snap Snapshot, cost domain.CostReport, dur time.Duration) domain.RunEvidence {
	peaks, concurrencyReport := concurrency.Peaks(snap.Records, req.Test.Definition.ParallelGroups)

	return domain.RunEvidence{
		Definition:           req.Test.Definition,
		Settings:             req.Settings,
		Key:                  req.Key,
		Records:              snap.Records,
		SubjectResult:        snap.SubjectResult,
		Orchestration:        snap.Orchestration,
		OrchestrationProblem: snap.OrchestrationProblem,
		SnapshotFiles:        snap.Files,

		ProtocolViolations:        collaboratorProtocolViolations(snap.Records),
		SubjectProtocolViolations: subjectProtocolViolations(req.Test.Definition.Subject, snap.SubjectResult),
		PeakConcurrency:           peaks,
		ConcurrencyProblems: domain.ConcurrencyProblems{
			UnterminatedSeqs: concurrencyReport.UnterminatedSeqs,
			Ungrouped:        concurrencyReport.Ungrouped,
		},

		Cost:     cost,
		Duration: dur,

		LogRoot:      snap.LogRoot,
		LogsProduced: snap.LogsProduced,

		SubjectVersion: snap.SubjectVersion,
	}
}

// collaboratorProtocolViolations checks every collaborator response recorded
// in records — the text a stub or a rewrite-prompt echo actually produced —
// against the Communication Protocol, tallied by class. Each response's
// context comes from the invocation it answers, correlated by sequence
// number, per the contract runner is the one component positioned to
// resolve: it is the only package holding both sides of every message pair.
func collaboratorProtocolViolations(records []domain.LogRecord) map[domain.ViolationClassKey]int {
	invocations := make(map[int]domain.TaskMessage, len(records))
	for _, rec := range records {
		if rec.Kind == domain.RecordStart && rec.Message != nil {
			invocations[rec.Seq] = *rec.Message
		}
	}

	counts := map[domain.ViolationClassKey]int{}
	for _, rec := range records {
		if rec.Kind != domain.RecordEnd || rec.Echo == nil || rec.Echo.Observed == "" {
			continue
		}

		ctx := protocolcheck.UnknownRequest
		if inv, ok := invocations[rec.Seq]; ok {
			ctx = protocolcheck.ResponseContextFor(inv)
		}

		result := protocolcheck.CheckResponse(rec.Echo.Observed, protocolVersion, ctx)
		for class, n := range result.CountByClass() {
			counts[domain.ViolationClassKey(class)] += n
		}
	}
	return counts
}

// subjectProtocolViolations checks the subject's own final protocol message
// against the Communication Protocol. The response context comes from the
// subject's declared opening message when it itself parses as a protocol
// invocation; a free-text opening message yields an unknown request rather
// than manufacturing a violation from a fact the opening message never
// stated.
func subjectProtocolViolations(subject domain.SubjectUnderTest, res domain.SubjectResult) map[domain.ViolationClassKey]int {
	if res.ProtocolMessage == "" {
		return map[domain.ViolationClassKey]int{}
	}

	ctx := protocolcheck.UnknownRequest
	if protocolcheck.CheckInvocation(subject.OpeningMessage, protocolVersion).Parsed {
		if inv, ok := parseTaskMessage(subject.OpeningMessage); ok {
			ctx = protocolcheck.ResponseContextFor(inv)
		}
	}

	result := protocolcheck.CheckResponse(res.ProtocolMessage, protocolVersion, ctx)
	counts := map[domain.ViolationClassKey]int{}
	for class, n := range result.CountByClass() {
		counts[domain.ViolationClassKey(class)] += n
	}
	return counts
}

// parseTaskMessage best-effort decodes the first JSON object in raw as a
// domain.TaskMessage, mirroring protocolcheck's own bare-message extraction
// so a caller that already confirmed raw parses as a protocol invocation can
// recover the fields it needs.
func parseTaskMessage(raw string) (domain.TaskMessage, bool) {
	idx := strings.IndexByte(raw, '{')
	if idx < 0 {
		return domain.TaskMessage{}, false
	}
	dec := json.NewDecoder(strings.NewReader(raw[idx:]))
	var tm domain.TaskMessage
	if err := dec.Decode(&tm); err != nil {
		return domain.TaskMessage{}, false
	}
	tm.Raw = raw
	tm.Extraction = domain.ExtractionParsed
	return tm, true
}
