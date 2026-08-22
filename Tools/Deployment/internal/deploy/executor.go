package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/todo"
)

// entryKey is the composite key used by buildEntryMap and buildManifest.
// Keying by (Ref, Target) instead of Ref alone ensures each file in a
// multi-file skill retains its own manifest entry without silent overwrite.
type entryKey struct {
	Ref    domain.ArtifactRef
	Target string // normalised target path (forward slashes)
}

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
	// TodoItems provides the set of pre-collected gaps (from the transform and plan phases) to
	// write into the checklist file. The executor calls it once, at checklist-write time, after
	// every per-item Content callback has run — so gaps forwarded into the collector *during*
	// Execute are included. The executor appends its own gaps to the returned items before
	// rendering.
	//
	// The provider must be read-only with respect to the caller's collector: it returns items,
	// it does not expose mutation. Callers pass their collector's items accessor directly
	// (for example todo.Collector.Items).
	//
	// A nil TodoItems is valid and means "no pre-collected items"; only executor-generated gaps
	// are then rendered.
	TodoItems func() []domain.TodoItem
	// TodoMeta holds the run-level metadata embedded in the MOSAIC-DEPLOYMENT-TODO.md header.
	TodoMeta todo.Meta
	// DryRun disables all filesystem writes when true; the result still reports what would
	// have happened.
	DryRun bool
	// Atomic opts this run into all-or-nothing execution. When false (the default and the
	// behaviour of every pre-existing caller), a mid-plan failure is non-fatal: files already
	// written stay on disk, ExecResult.Partial carries the first error, and the manifest is
	// saved so it reflects real on-disk state.
	//
	// When true, every filesystem write the executor performs is journaled before it happens.
	// On any failure the run reverses itself: files this run created are deleted, files this
	// run overwrote are restored to their pre-run bytes, and the manifest and the deployment
	// checklist file are left exactly as they were before the run started.
	//
	// Atomic has no effect when DryRun is true (no writes occur) or when a fallback tier is
	// used (the workspace was unwritable, which is not a mid-plan stop).
	Atomic bool
}

// RevertFailure names one journal entry the reversal could not apply.
type RevertFailure struct {
	// Path is the absolute filesystem path that could not be restored or deleted.
	Path string
	// Err is the underlying filesystem error.
	Err error
	// Restore is true when the entry described a file that existed before the run and could
	// not be rewritten; false when it described a file the run created and could not be
	// deleted. The distinction matters to the user: a failed restore means lost content, a
	// failed delete means a stray file.
	Restore bool
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
	// Reverted is true when this run was executed in Atomic mode, hit a failure, and reversed
	// its writes. Partial is always non-nil when Reverted is true and carries the original
	// triggering error, so pre-existing consumers that only read Partial still see a failure.
	// Reverted is the field that distinguishes "failed and rolled back" from "failed partway
	// and disk reflects that".
	//
	// CALLER INVARIANT: a result with Reverted true must never be passed to buildSummary. A
	// summary enumerates deployed artifacts, and after a reversal none of them exist, so the
	// summary would describe files that are not on disk. Callers must branch on Reverted
	// before summarising.
	Reverted bool
	// RevertFailures lists the journal entries the reversal itself could not apply. It is
	// empty on a clean reversal. A non-empty slice means the workspace is in an intermediate
	// state and the listed paths need manual recovery; the reversal always attempts every
	// entry rather than aborting at the first error.
	RevertFailures []RevertFailure
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

	// Initialise the write journal for atomic mode. The probe item's path is pre-recorded
	// after resolveDeploymentRoot returns (using the item that was actually written), because
	// the multi-item probe iteration may use a different item than the first eligible one when
	// the first item's content rendering fails.
	//
	// Atomic has no effect during DryRun (no writes occur) or when a fallback tier is selected
	// (the workspace was unwritable — a fallback run is not a mid-plan stop). The journal is
	// discarded after resolveDeploymentRoot if a fallback is chosen.
	var journal *writeJournal
	if req.Atomic && !req.DryRun {
		journal = &writeJournal{}
	}

	// Resolve the deployment root once before any content write (CD-12, AC17.2).
	// The journal is passed through so resolveDeploymentRoot can record the probe path
	// before writing it, ensuring atomic reversal correctly sees the probe file as newly
	// created (existed=false) and deletes it on rollback rather than restoring it.
	// probeErr is non-nil when the workspace probe failed and a fallback was triggered;
	// it is carried as the first partial error and the per-item Err in fallback mode.
	// contentErrors lists items whose Content callback failed during probe iteration.
	deployRoot, fallback, probeErr, contentErrors, _, err := resolveDeploymentRoot(req, journal)
	if err != nil {
		return ExecResult{}, err
	}

	// Atomic mode is active only when writing to the workspace (FallbackNone). A fallback
	// run writes to an alternate location because the workspace was unwritable; that is not a
	// mid-plan stop and keeps today's non-atomic semantics. DryRun performs no writes.
	atomicMode := req.Atomic && !req.DryRun && fallback == domain.FallbackNone
	if !atomicMode {
		journal = nil // only workspace writes are covered by atomic reversal
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

	// Surface content-render failures from the probe phase as TakenFailed action records.
	// Build a set of their TargetPaths to exclude them from the main execution loop so they
	// are not double-processed.
	contentErrorPaths := make(map[string]bool, len(contentErrors))
	for _, ce := range contentErrors {
		contentErrorPaths[ce.Item.TargetPath] = true
		ar := domain.ActionRecord{
			Ref:        ce.Item.Ref,
			TargetPath: filepath.Join(deployRoot, ce.Item.TargetPath),
			Taken:      domain.TakenFailed,
			Err:        fmt.Sprintf("content render failed during probe: %s", ce.Err),
		}
		e.log.Action(ar)
		actions = append(actions, ar)
	}

	for _, item := range req.Plan.Items {
		// Skip items that already have a TakenFailed record from the probe phase; executing
		// them again would produce a duplicate action record for the same TargetPath.
		if contentErrorPaths[item.TargetPath] {
			continue
		}
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
			ar, entry = e.executeItem(item, req, deployRoot, prior, journal)
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
			normTarget := strings.ReplaceAll(item.TargetPath, "\\", "/")
			entryMap[entryKey{Ref: item.Ref, Target: normTarget}] = *entry
		}

		// In atomic mode, stop at the first failure. Continuing to write more items only
		// enlarges the journal and widens the window for a reversal failure; the action records
		// gathered so far are kept for diagnostics and are documented as describing writes that
		// no longer exist on disk after the reversal.
		if atomicMode && partialErr != nil {
			break
		}
	}

	// In atomic mode, reverse all journaled writes on any failure. Hook deployment, manifest
	// save, and checklist write are all skipped when reverting — the run is all-or-nothing.
	if atomicMode && partialErr != nil {
		revertFailures := journal.revert()
		outcome := computeOutcome(actions, partialErr, fallback)
		e.log.EndRun(outcome)
		return ExecResult{
			DeploymentRoot: deployRoot,
			Fallback:       fallback,
			Actions:        actions,
			Manifest:       prior, // return the prior manifest; nothing was saved to disk
			TodoFilePath:   "",    // no checklist was written on a reverted run
			Partial:        partialErr,
			Reverted:       true,
			RevertFailures: revertFailures,
		}, nil
	}

	// Deploy hook bundles and apply registration steps. The journal is passed through so that
	// hook file writes are covered by atomic reversal: if the manifest save below then fails,
	// the hook files written here are reversed along with the plan item files.
	hookGaps := e.deployHooks(deployRoot, req.Plan.WorkspacePath, req.Hooks, req.DryRun, journal)
	for _, g := range hookGaps {
		addGap(g)
	}

	// Build the manifest from what is actually on disk (entryMap reflects reality).
	finalManifest := buildManifest(prior, entryMap, req.Plan)

	// Persist the manifest and the TODO file (skipped for DryRun).
	todoFilePath := ""
	if !req.DryRun {
		// In atomic mode, record the manifest path in the journal before saving it. The
		// manifest store uses os.WriteFile (O_WRONLY|O_CREATE|O_TRUNC), which truncates the
		// file before writing. A write failure after truncation begins leaves the manifest
		// empty or corrupt on disk. Recording the path here ensures journal.revert() can
		// restore the pre-run bytes from the snapshot taken now, returning the manifest to
		// exactly the state it was in before this run started.
		if atomicMode {
			manifestPath := filepath.Join(req.Plan.WorkspacePath, manifest.Dir, manifest.FileName)
			if recErr := journal.record(manifestPath); recErr != nil {
				partialErr = fmt.Errorf("atomic journal record for manifest failed: %s", recErr)
				revertFailures := journal.revert()
				outcome := computeOutcome(actions, partialErr, fallback)
				e.log.EndRun(outcome)
				return ExecResult{
					DeploymentRoot: deployRoot,
					Fallback:       fallback,
					Actions:        actions,
					Manifest:       prior,
					TodoFilePath:   "",
					Partial:        partialErr,
					Reverted:       true,
					RevertFailures: revertFailures,
				}, nil
			}
		}

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

		// In atomic mode, a manifest save failure is a triggering failure. All writes made so
		// far — plan item files and hook files — are reversed. The TODO checklist is never
		// written on a reverted run (we return here before reaching the write below). The
		// manifest is left as it was before the run: the save failed, so nothing was durably
		// changed on disk (or the write was incomplete and the file is stale, which is why the
		// prior manifest is returned rather than the draft finalManifest).
		if atomicMode && partialErr != nil {
			revertFailures := journal.revert()
			outcome := computeOutcome(actions, partialErr, fallback)
			e.log.EndRun(outcome)
			return ExecResult{
				DeploymentRoot: deployRoot,
				Fallback:       fallback,
				Actions:        actions,
				Manifest:       prior,
				TodoFilePath:   "",
				Partial:        partialErr,
				Reverted:       true,
				RevertFailures: revertFailures,
			}, nil
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
//
// journal is non-nil in atomic mode. When non-nil, the path of every write is recorded in
// the journal immediately before the write so the executor can reverse the run on failure.
func (e *executor) executeItem(
	item domain.PlanItem,
	req ExecRequest,
	deployRoot string,
	prior domain.Manifest,
	journal *writeJournal,
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
		if journal != nil {
			if recErr := journal.record(destPath); recErr != nil {
				ar.Taken = domain.TakenFailed
				ar.Err = fmt.Sprintf("atomic journal record failed: %s", recErr)
				return ar, nil
			}
		}
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
		return e.executeConflict(item, ar, decision, req, deployRoot, prior, journal)
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
//
// journal is non-nil in atomic mode. When non-nil, the target path is recorded before any
// overwrite so the executor can restore the original content on reversal. The backup copy
// written by DecisionBackupThenOverwrite is deliberately not journaled: backup files under
// .mosaic/backups/ survive reversal by design (deleting a user's backup is the more
// dangerous choice).
func (e *executor) executeConflict(
	item domain.PlanItem,
	ar domain.ActionRecord,
	decision domain.ConflictDecision,
	req ExecRequest,
	deployRoot string,
	prior domain.Manifest,
	journal *writeJournal,
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
		if journal != nil {
			if recErr := journal.record(destPath); recErr != nil {
				ar.Taken = domain.TakenFailed
				ar.Err = fmt.Sprintf("atomic journal record failed: %s", recErr)
				return ar, nil
			}
		}
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
		// Journal the target path before backup and overwrite. The backup file written to
		// .mosaic/backups/ is deliberately not journaled: backup copies survive reversal by
		// design (deleting a user's own backup is the more dangerous choice).
		srcPath := filepath.Join(deployRoot, item.TargetPath)
		if journal != nil {
			if recErr := journal.record(srcPath); recErr != nil {
				ar.Taken = domain.TakenFailed
				ar.Err = fmt.Sprintf("atomic journal record failed: %s", recErr)
				return ar, nil
			}
		}
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
		HarnessVersion:                stamp.HarnessVersion,
		InjectionsVersion:             stamp.InjectionsVersion,
		OrchestratorInjectionsVersion: stamp.OrchestratorInjectionsVersion,
		ToolMappingsVersion:           stamp.ToolMappingsVersion,
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
			case "harness_version":
				stamp.HarnessVersion = delta.Source
			case "injections_version":
				stamp.InjectionsVersion = delta.Source
			case "orchestrator_injections_version":
				stamp.OrchestratorInjectionsVersion = delta.Source
			case "tool_mappings_version":
				stamp.ToolMappingsVersion = delta.Source
			}
		}
	}
	return stamp
}

// buildEntryMap constructs a mutable map of manifest entries keyed by the composite
// (ArtifactRef, normalised TargetPath). The executor modifies this map during execution
// and converts it back to a Manifest via buildManifest.
//
// Target paths are normalised (backslashes to forward slashes) to match LookupAt semantics.
// Using a composite key ensures each file in a multi-file skill retains its own manifest
// entry and is not silently overwritten by a later file with the same ArtifactRef.
func buildEntryMap(m domain.Manifest) map[entryKey]domain.ManifestEntry {
	out := make(map[entryKey]domain.ManifestEntry, len(m.Entries))
	for _, e := range m.Entries {
		normTarget := strings.ReplaceAll(e.TargetPath, "\\", "/")
		out[entryKey{Ref: e.Ref, Target: normTarget}] = e
	}
	return out
}

// buildManifest produces the final domain.Manifest from the entry map, sorted by TargetPath.
func buildManifest(prior domain.Manifest, entryMap map[entryKey]domain.ManifestEntry, plan domain.Plan) domain.Manifest {
	harnessID := plan.Harness.ID
	if harnessID == "" {
		harnessID = prior.HarnessID
	}
	m := domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     harnessID,
		UpdatedAt:     time.Now(),
	}

	keys := make([]entryKey, 0, len(entryMap))
	for k := range entryMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return entryMap[keys[i]].TargetPath < entryMap[keys[j]].TargetPath
	})
	for _, k := range keys {
		m.Entries = append(m.Entries, entryMap[k])
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
// It combines pre-collected items from req.TodoItems (called once here, after all Content
// callbacks have run) with executor-generated gaps.
func (e *executor) writeTodoFile(req ExecRequest, execGaps []domain.Gap) (string, error) {
	collector := todo.NewCollector()

	if req.TodoItems != nil {
		for _, item := range req.TodoItems() {
			collector.Add(item)
		}
	}
	for _, g := range execGaps {
		collector.AddGap(g)
	}

	content := todo.RenderMarkdown(collector.Groups(), req.TodoMeta)

	path := filepath.Join(req.Plan.WorkspacePath, todo.FileNameAt(req.TodoMeta.GeneratedAt))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
