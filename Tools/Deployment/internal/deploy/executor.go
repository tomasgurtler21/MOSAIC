package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/todo"
)

// ErrUndecidedConflict is returned when a plan item classified as ActionConflict has no
// corresponding entry in ExecRequest.Conflicts. This is a programming error in the caller:
// the app layer must collect all conflict decisions before calling Execute.
var ErrUndecidedConflict = errors.New("locally-modified item has no decision")

// ErrNoWritableLocation is returned when all three fallback tiers (workspace, MOSAIC-root,
// OS temp) are unwritable and no deployment location can be established.
var ErrNoWritableLocation = errors.New("no writable deployment location found")

// Executor applies a domain.Plan to disk. It resolves the deployment root once before any
// content write (CD-12, AC17.2), manages backups for locally-modified files, performs hook
// registration steps, writes the manifest after every action, and delegates every gap and
// declined action to the todo.Collector.
type Executor interface {
	Execute(ctx context.Context, req ExecRequest) (ExecResult, error)
}

// ExecRequest is the input to Executor.Execute.
type ExecRequest struct {
	Plan domain.Plan
	// MosaicRoot is the absolute path of the MOSAIC repository. It supplies both the second
	// fallback tier (<MosaicRoot>/MosaicDeploy/fallback/) and the log path.
	MosaicRoot string
	// Content produces the bytes for one plan item. Backed by transform.Apply via the app
	// layer; the executor never calls the transform engine directly (testability boundary).
	Content func(domain.PlanItem) ([]byte, error)
	// Conflicts holds the user's per-file decision, keyed by PlanItem.TargetPath. A missing
	// key for any ActionConflict item is a programming error: Execute returns ErrUndecidedConflict.
	Conflicts map[string]domain.ConflictDecision
	// VersionStamps carries the source version fields for each plan item, keyed by
	// PlanItem.TargetPath. These are written into ManifestEntry so the planner can detect
	// staleness on subsequent runs. For ActionUpdate items, stale field values are also
	// derived from PlanItem.Stale; VersionStamps fills in non-stale fields and ActionCreate
	// items. A nil map is valid: version fields will be empty for items not in the map.
	VersionStamps map[string]domain.VersionStamp
	// Hooks carries the resolved hook plans from the app layer (one per active hook bundle).
	Hooks []domain.HookPlan
	// Todo is the set of pre-collected gaps (from transform and plan phases) to write into
	// the checklist file. The executor appends its own gaps before rendering.
	Todo []domain.TodoItem
	// TodoMeta holds the run-level metadata embedded in the MOSAIC-DEPLOYMENT-TODO.md header.
	TodoMeta todo.Meta
	// DryRun disables all filesystem writes when true; the result still reports what would
	// have happened.
	DryRun bool
}

// ExecResult is the outcome of Executor.Execute.
type ExecResult struct {
	// DeploymentRoot is the absolute path of the directory used for all content writes.
	// It is set even when Partial is non-nil.
	DeploymentRoot string
	// Fallback records which tier of the fallback chain was ultimately used.
	Fallback domain.FallbackTier
	// Actions is the ordered record of every deployment action taken or declined.
	Actions []domain.ActionRecord
	// Manifest is the state written to <workspace>/.mosaic/manifest.yaml after the run.
	// It reflects actual on-disk state, including any partial failure (AC17.4).
	Manifest domain.Manifest
	// TodoFilePath is the absolute path of the written MOSAIC-DEPLOYMENT-TODO.md file.
	TodoFilePath string
	// Partial is non-nil when the run stopped mid-plan due to a write failure. The manifest
	// and Actions still reflect what is actually on disk.
	Partial error
}

// NewExecutor constructs the production Executor.
func NewExecutor(store manifest.Store, log logging.Logger, todos todo.Collector) Executor {
	return &executor{store: store, log: log, todos: todos}
}

type executor struct {
	store manifest.Store
	log   logging.Logger
	todos todo.Collector
}

// Execute applies req.Plan to disk according to the deployment contracts.
//
// Fallback semantics: when a fallback tier is used, items are physically written to the
// fallback deployment root so the plan is complete in one location (AC17.2). However,
// each per-item action record reflects the workspace write outcome (TakenFailed) because
// the workspace — the user's intended target — was not writable. No manifest entries are
// produced for fallback writes; the manifest tracks only what reached the workspace.
func (e *executor) Execute(ctx context.Context, req ExecRequest) (ExecResult, error) {
	// Validate that every ActionConflict item has a decision. A missing decision is a
	// programming error in the caller that must be surfaced immediately.
	for _, item := range req.Plan.Items {
		if item.Action == domain.ActionConflict {
			if _, ok := req.Conflicts[item.TargetPath]; !ok {
				return ExecResult{}, ErrUndecidedConflict
			}
		}
	}

	// Load the existing manifest so prior entries can be preserved for unchanged and
	// skipped items (AC17.4). An I/O error (e.g. permission denied) is logged as a warning
	// and the run continues as if no manifest exists — the Snapshot.State field distinguishes
	// the specific condition.
	snap, loadErr := e.store.Load(req.Plan.WorkspacePath)
	if loadErr != nil {
		e.log.Event(logging.Event{
			Time:    time.Now(),
			Level:   logging.LevelWarn,
			Kind:    "manifest",
			Subject: req.Plan.WorkspacePath,
			Message: fmt.Sprintf("could not load manifest (treating workspace as new): %s", loadErr),
		})
	}
	prior := snap.Manifest

	// Resolve the deployment root once before any content write (CD-12, AC17.2).
	// probeErr is non-nil when the workspace probe failed and a fallback was triggered;
	// it is carried as the first partial error and the per-item Err in fallback mode.
	deployRoot, fallback, probeErr, err := resolveDeploymentRoot(req)
	if err != nil {
		return ExecResult{}, err
	}

	// Log the run start after the deployment root is known.
	e.log.StartRun(logging.RunContext{
		StartedAt:      time.Now(),
		Mode:           req.Plan.Mode,
		Harness:        req.Plan.Harness,
		WorkspacePath:  req.Plan.WorkspacePath,
		DeploymentRoot: deployRoot,
		Fallback:       fallback,
	})

	// Accumulate executor-generated gaps for the TODO file and the injected collector.
	var execGaps []domain.Gap
	addGap := func(g domain.Gap) {
		e.todos.AddGap(g)
		execGaps = append(execGaps, g)
	}

	// Emit a fallback-location gap when deployment did not land in the workspace.
	if fallback != domain.FallbackNone {
		addGap(domain.Gap{
			Kind:    domain.GapFallbackLocation,
			Subject: deployRoot,
			Detail:  fmt.Sprintf("files deployed to fallback location %q (workspace was not writable)", deployRoot),
		})
	}

	// Process every plan item, recording the outcome of each action.
	// partialErr is non-nil when a content-write (not the workspace probe) fails mid-plan.
	// The probe error is tracked separately in probeErr and does not set Partial — a complete
	// fallback deployment is not a mid-plan stop.
	var actions []domain.ActionRecord
	entryMap := buildEntryMap(prior) // carries prior entries; updated for successful workspace writes
	var partialErr error

	for _, item := range req.Plan.Items {
		var ar domain.ActionRecord
		var entry *domain.ManifestEntry

		if req.DryRun {
			ar = simulateAction(item, req)
		} else if fallback != domain.FallbackNone {
			// Fallback mode: write to the fallback tier for physical completeness, but
			// record TakenFailed because the workspace (intended target) was unwritable.
			ar, entry = e.executeFallbackItem(item, req, deployRoot, probeErr)
		} else {
			// Normal mode: write to the workspace deployment root.
			ar, entry = e.executeItem(item, req, deployRoot, prior)
			if ar.Err != "" && partialErr == nil {
				partialErr = fmt.Errorf("%s", ar.Err)
			}
		}

		// A skipped conflict file is reported to the TODO collector.
		if ar.Taken == domain.TakenSkipped {
			addGap(domain.Gap{
				Kind:    domain.GapSkippedFile,
				Subject: item.Ref.Key,
				Detail:  "file was locally modified and the user chose to skip it",
			})
		}

		e.log.Action(ar)
		actions = append(actions, ar)

		if entry != nil {
			entryMap[item.Ref] = *entry
		}
	}

	// Deploy hook bundles and apply registration steps.
	hookGaps := e.deployHooks(deployRoot, req.Plan.WorkspacePath, req.Hooks, req.DryRun)
	for _, g := range hookGaps {
		addGap(g)
	}

	// Build the manifest from what is actually on disk (entryMap reflects reality).
	finalManifest := buildManifest(prior, entryMap, req.Plan)

	// Persist the manifest and the TODO file (skipped for DryRun).
	todoFilePath := ""
	if !req.DryRun {
		if saveErr := e.store.Save(req.Plan.WorkspacePath, finalManifest); saveErr != nil {
			e.log.Event(logging.Event{
				Time:    time.Now(),
				Level:   logging.LevelError,
				Kind:    "manifest",
				Subject: req.Plan.WorkspacePath,
				Message: fmt.Sprintf("failed to save manifest: %s", saveErr),
			})
			if partialErr == nil {
				partialErr = saveErr
			}
		}
		var todoErr error
		todoFilePath, todoErr = e.writeTodoFile(req, execGaps)
		if todoErr != nil {
			e.log.Event(logging.Event{
				Time:    time.Now(),
				Level:   logging.LevelWarn,
				Kind:    "todo",
				Subject: req.Plan.WorkspacePath,
				Message: fmt.Sprintf("failed to write TODO file: %s", todoErr),
			})
		}
	}

	outcome := computeOutcome(actions, partialErr, fallback)
	e.log.EndRun(outcome)

	return ExecResult{
		DeploymentRoot: deployRoot,
		Fallback:       fallback,
		Actions:        actions,
		Manifest:       finalManifest,
		TodoFilePath:   todoFilePath,
		Partial:        partialErr,
	}, nil
}

// executeItem carries out one plan item in normal (workspace) mode and returns its action
// record and optional manifest entry. A nil manifest entry means "do not alter the entry
// for this item" — the caller leaves the prior entry intact in entryMap.
func (e *executor) executeItem(
	item domain.PlanItem,
	req ExecRequest,
	deployRoot string,
	prior domain.Manifest,
) (domain.ActionRecord, *domain.ManifestEntry) {
	ar := domain.ActionRecord{
		Ref:        item.Ref,
		TargetPath: filepath.Join(deployRoot, item.TargetPath),
		Stale:      item.Stale,
	}

	switch item.Action {
	case domain.ActionUnchanged:
		ar.Taken = domain.TakenUnchanged
		// Prior entry is preserved via entryMap; return nil to signal no update.
		return ar, nil

	case domain.ActionCreate, domain.ActionUpdate:
		// Hook items are multi-file bundles: their SourcePath is the bundle directory, not a
		// single file registered in the catalog. Calling req.Content on a hook item always
		// returns a catalog error. Hook bundle files are written by deployHooks (which runs
		// after this loop); hook plan items exist only for action recording and manifest
		// tracking. Skip the content/write path entirely for hook items.
		if item.Ref.Kind == domain.ArtifactHook {
			if item.Action == domain.ActionCreate {
				ar.Taken = domain.TakenCreated
			} else {
				ar.Taken = domain.TakenUpdated
			}
			stamp := resolveVersionStamp(item, req.VersionStamps)
			entry := newManifestEntry(item, nil, stamp)
			return ar, &entry
		}

		content, err := req.Content(item)
		if err != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = err.Error()
			return ar, nil
		}
		destPath := filepath.Join(deployRoot, item.TargetPath)
		if writeErr := mkdirAndWrite(destPath, content); writeErr != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = writeErr.Error()
			return ar, nil
		}
		if item.Action == domain.ActionCreate {
			ar.Taken = domain.TakenCreated
		} else {
			ar.Taken = domain.TakenUpdated
		}
		stamp := resolveVersionStamp(item, req.VersionStamps)
		entry := newManifestEntry(item, content, stamp)
		return ar, &entry

	case domain.ActionConflict:
		decision := req.Conflicts[item.TargetPath]
		return e.executeConflict(item, ar, decision, req, deployRoot, prior)
	}

	ar.Taken = domain.TakenFailed
	ar.Err = fmt.Sprintf("unrecognised plan action %q", item.Action)
	return ar, nil
}

// executeFallbackItem handles one plan item when a fallback tier is in use. Items are
// physically written to deployRoot so the deployment is complete in one location (AC17.2),
// but action records show TakenFailed because the workspace was not writable. No manifest
// entries are produced for fallback writes.
func (e *executor) executeFallbackItem(
	item domain.PlanItem,
	req ExecRequest,
	deployRoot string,
	probeErr error,
) (domain.ActionRecord, *domain.ManifestEntry) {
	ar := domain.ActionRecord{
		Ref:        item.Ref,
		TargetPath: filepath.Join(deployRoot, item.TargetPath),
		Stale:      item.Stale,
	}

	switch item.Action {
	case domain.ActionUnchanged:
		ar.Taken = domain.TakenUnchanged
		return ar, nil

	case domain.ActionCreate, domain.ActionUpdate:
		// Hook items must not go through the content path: their SourcePath is a bundle
		// directory that the catalog never registers, so req.Content would return an error.
		// In fallback mode all items are TakenFailed anyway (the workspace was unwritable),
		// so skip the content call entirely for hook items.
		if item.Ref.Kind != domain.ArtifactHook {
			content, err := req.Content(item)
			if err == nil {
				// Write to the fallback tier so the plan is physically complete there.
				_ = mkdirAndWrite(filepath.Join(deployRoot, item.TargetPath), content)
			}
		}
		ar.Taken = domain.TakenFailed
		if probeErr != nil {
			ar.Err = probeErr.Error()
		} else {
			ar.Err = "workspace deployment path was not writable"
		}
		return ar, nil

	case domain.ActionConflict:
		ar.Taken = domain.TakenFailed
		if probeErr != nil {
			ar.Err = probeErr.Error()
		} else {
			ar.Err = "workspace deployment path was not writable"
		}
		return ar, nil
	}

	ar.Taken = domain.TakenFailed
	ar.Err = fmt.Sprintf("unrecognised plan action %q in fallback mode", item.Action)
	return ar, nil
}

// executeConflict handles the three conflict decisions for a locally-modified file.
func (e *executor) executeConflict(
	item domain.PlanItem,
	ar domain.ActionRecord,
	decision domain.ConflictDecision,
	req ExecRequest,
	deployRoot string,
	prior domain.Manifest,
) (domain.ActionRecord, *domain.ManifestEntry) {
	switch decision {
	case domain.DecisionSkip:
		ar.Taken = domain.TakenSkipped
		// Return the prior entry unchanged so entryMap preserves it.
		if entry, ok := prior.Lookup(item.Ref); ok {
			return ar, &entry
		}
		return ar, nil

	case domain.DecisionOverwrite:
		// Hook items are multi-file bundles: their SourcePath is the bundle directory, not a
		// single catalog-registered file. Skip the content/write path entirely; bundle files
		// are managed by deployHooks. Produce the action record and manifest entry directly.
		if item.Ref.Kind == domain.ArtifactHook {
			ar.Taken = domain.TakenUpdated
			stamp := resolveVersionStamp(item, req.VersionStamps)
			entry := newManifestEntry(item, nil, stamp)
			return ar, &entry
		}
		content, err := req.Content(item)
		if err != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = err.Error()
			return ar, nil
		}
		destPath := filepath.Join(deployRoot, item.TargetPath)
		if writeErr := mkdirAndWrite(destPath, content); writeErr != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = writeErr.Error()
			return ar, nil
		}
		ar.Taken = domain.TakenUpdated
		stamp := resolveVersionStamp(item, req.VersionStamps)
		entry := newManifestEntry(item, content, stamp)
		return ar, &entry

	case domain.DecisionBackupThenOverwrite:
		// Hook items are multi-file bundles whose TargetPath is a directory, not a single
		// file. Backing up or overwriting a directory via the single-file path is not
		// meaningful here; deployHooks manages the actual bundle files. Skip the backup,
		// content fetch and file write; produce the action record and manifest entry directly.
		if item.Ref.Kind == domain.ArtifactHook {
			ar.Taken = domain.TakenBackedUp
			stamp := resolveVersionStamp(item, req.VersionStamps)
			entry := newManifestEntry(item, nil, stamp)
			return ar, &entry
		}
		// Backup the current on-disk file before overwriting.
		srcPath := filepath.Join(deployRoot, item.TargetPath)
		backupPath, err := createBackup(req.Plan.WorkspacePath, item.TargetPath, srcPath)
		if err != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = fmt.Sprintf("backup failed: %s", err)
			return ar, nil
		}
		ar.BackupPath = backupPath

		content, err := req.Content(item)
		if err != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = err.Error()
			return ar, nil
		}
		destPath := filepath.Join(deployRoot, item.TargetPath)
		if writeErr := mkdirAndWrite(destPath, content); writeErr != nil {
			ar.Taken = domain.TakenFailed
			ar.Err = writeErr.Error()
			return ar, nil
		}
		ar.Taken = domain.TakenBackedUp
		stamp := resolveVersionStamp(item, req.VersionStamps)
		entry := newManifestEntry(item, content, stamp)
		return ar, &entry
	}

	ar.Taken = domain.TakenFailed
	ar.Err = fmt.Sprintf("unrecognised conflict decision %q", decision)
	return ar, nil
}

// simulateAction builds an ActionRecord for a DryRun, mapping each plan action to its
// expected outcome without performing any filesystem write.
func simulateAction(item domain.PlanItem, req ExecRequest) domain.ActionRecord {
	ar := domain.ActionRecord{
		Ref:        item.Ref,
		TargetPath: item.TargetPath,
		Stale:      item.Stale,
	}
	switch item.Action {
	case domain.ActionCreate:
		ar.Taken = domain.TakenCreated
	case domain.ActionUpdate:
		ar.Taken = domain.TakenUpdated
	case domain.ActionUnchanged:
		ar.Taken = domain.TakenUnchanged
	case domain.ActionConflict:
		decision := req.Conflicts[item.TargetPath]
		switch decision {
		case domain.DecisionSkip:
			ar.Taken = domain.TakenSkipped
		case domain.DecisionOverwrite:
			ar.Taken = domain.TakenUpdated
		case domain.DecisionBackupThenOverwrite:
			ar.Taken = domain.TakenBackedUp
		default:
			ar.Taken = domain.TakenSkipped
		}
	default:
		ar.Taken = domain.TakenSkipped
	}
	return ar
}

// newManifestEntry builds a ManifestEntry for a successfully deployed item.
// stamp carries the source version fields for this deployment so the planner can detect
// staleness on subsequent runs.
func newManifestEntry(item domain.PlanItem, content []byte, stamp domain.VersionStamp) domain.ManifestEntry {
	return domain.ManifestEntry{
		Ref:                           item.Ref,
		TargetPath:                    item.TargetPath,
		Version:                       stamp.Version,
		TransformVersion:              stamp.TransformVersion,
		InjectionsVersion:             stamp.InjectionsVersion,
		OrchestratorInjectionsVersion: stamp.OrchestratorInjectionsVersion,
		ContentHash:                   manifest.Hash(content),
		DeployedAt:                    time.Now(),
	}
}

// resolveVersionStamp derives the version stamp for one plan item.
// For ActionUpdate items, stale field deltas supply authoritative source values — they override
// the corresponding VersionStamps map entries. For all other actions, stamps come entirely from
// the VersionStamps map (a nil map or absent key yields zero-value stamps).
func resolveVersionStamp(item domain.PlanItem, stamps map[string]domain.VersionStamp) domain.VersionStamp {
	stamp := stamps[item.TargetPath] // zero value when stamps is nil or key absent
	// ActionUpdate: item.Stale lists every changed field with its new source value. These are
	// authoritative for the stale fields; non-stale fields retain their value from the map.
	if item.Action == domain.ActionUpdate {
		for _, delta := range item.Stale {
			switch delta.Field {
			case "version":
				stamp.Version = delta.Source
			case "transform_version":
				stamp.TransformVersion = delta.Source
			case "injections_version":
				stamp.InjectionsVersion = delta.Source
			case "orchestrator_injections_version":
				stamp.OrchestratorInjectionsVersion = delta.Source
			}
		}
	}
	return stamp
}

// buildEntryMap constructs a mutable map of manifest entries keyed by ArtifactRef.
// The executor modifies this map during execution and converts it back to a Manifest.
func buildEntryMap(m domain.Manifest) map[domain.ArtifactRef]domain.ManifestEntry {
	out := make(map[domain.ArtifactRef]domain.ManifestEntry, len(m.Entries))
	for _, e := range m.Entries {
		out[e.Ref] = e
	}
	return out
}

// buildManifest produces the final domain.Manifest from the entry map, sorted by TargetPath.
func buildManifest(prior domain.Manifest, entryMap map[domain.ArtifactRef]domain.ManifestEntry, plan domain.Plan) domain.Manifest {
	harnessID := plan.Harness.ID
	if harnessID == "" {
		harnessID = prior.HarnessID
	}
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     harnessID,
		UpdatedAt:     time.Now(),
	}

	refs := make([]domain.ArtifactRef, 0, len(entryMap))
	for ref := range entryMap {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		return entryMap[refs[i]].TargetPath < entryMap[refs[j]].TargetPath
	})
	for _, ref := range refs {
		m.Entries = append(m.Entries, entryMap[ref])
	}
	return m
}

// computeOutcome derives the run outcome from the action records, any partial content-write
// error, and the fallback tier used.
//
// A complete fallback deployment (fallback != FallbackNone, partialErr == nil) is reported
// as OutcomeCompletedWithGaps because a GapFallbackLocation was already emitted; the workspace
// — the user's intended target — was not written. OutcomeFailed is only returned when content
// writes (not the workspace probe) failed mid-plan.
func computeOutcome(actions []domain.ActionRecord, partialErr error, fallback domain.FallbackTier) domain.Outcome {
	if partialErr != nil {
		return domain.OutcomeFailed
	}
	if fallback != domain.FallbackNone {
		return domain.OutcomeCompletedWithGaps
	}
	for _, a := range actions {
		if a.Taken == domain.TakenSkipped {
			return domain.OutcomeCompletedWithGaps
		}
	}
	return domain.OutcomeSuccess
}

// writeTodoFile renders MOSAIC-DEPLOYMENT-TODO.md and writes it to the workspace root.
// It combines pre-collected items from req.Todo with executor-generated gaps.
func (e *executor) writeTodoFile(req ExecRequest, execGaps []domain.Gap) (string, error) {
	collector := todo.NewCollector()

	for _, item := range req.Todo {
		collector.Add(item)
	}
	for _, g := range execGaps {
		collector.AddGap(g)
	}

	content := todo.RenderMarkdown(collector.Groups(), req.TodoMeta)

	path := filepath.Join(req.Plan.WorkspacePath, todo.FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
