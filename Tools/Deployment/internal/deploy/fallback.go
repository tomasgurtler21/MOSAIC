package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-deploy/internal/domain"
)

// ContentError records a content-render failure for one plan item during the
// writability probe. The item was skipped as a probe candidate but must be
// surfaced to the user as a failed/skipped item.
type ContentError struct {
	Item domain.PlanItem
	Err  error
}

// resolveDeploymentRoot determines the deployment root by probing writability with
// probe-eligible Create or Update plan items. If the first eligible item's content
// cannot be rendered, the next eligible item is tried. If the workspace probe fails,
// the MOSAIC-root fallback is tried, then the OS-temp fallback. The probe error
// (workspace write failure) is returned so callers can record it as a partial failure
// even when fallback succeeds. For DryRun or plans with no writable items, the
// workspace is used without probing.
//
// contentErrors lists items whose Content callback failed; they were skipped as probe
// candidates but must be surfaced to the user. usedItem is the item whose content was
// actually used for the writability probe. When all items fail, usedItem is the zero value.
//
// journal, when non-nil, is recorded for the workspace probe path immediately before the
// probe write. Recording before the write ensures that when atomic reversal deletes or
// restores the file, it correctly treats the probe as a new file (existed=false) rather
// than a pre-existing one. The journal is passed through from the executor so the probe
// write participates in the same reversal scope as the rest of the run.
func resolveDeploymentRoot(req ExecRequest, journal *writeJournal) (root string, fallback domain.FallbackTier, probeErr error, contentErrors []ContentError, usedItem domain.PlanItem, err error) {
	if req.DryRun {
		return req.Plan.WorkspacePath, domain.FallbackNone, nil, nil, domain.PlanItem{}, nil
	}

	eligible := probeEligibleItems(req.Plan.Items)
	if len(eligible) == 0 {
		// No Create/Update items; use workspace as the deployment root.
		return req.Plan.WorkspacePath, domain.FallbackNone, nil, nil, domain.PlanItem{}, nil
	}

	// Iterate through eligible items, skipping those whose content rendering fails.
	var probeContent []byte
	found := false
	for _, item := range eligible {
		content, cerr := req.Content(item)
		if cerr != nil {
			contentErrors = append(contentErrors, ContentError{Item: item, Err: cerr})
			continue
		}
		probeContent = content
		usedItem = item
		found = true
		break
	}

	// If every eligible item failed content rendering, return a distinct error (not
	// ErrNoWritableLocation, which signals a filesystem problem, not a content problem).
	if !found {
		return "", "", nil, contentErrors, domain.PlanItem{}, fmt.Errorf("content rendering failed for all probe-eligible items")
	}

	// Tier 1: workspace. Journal the probe path before writing so atomic reversal can
	// correctly identify the file as newly created (existed=false) and delete it on rollback.
	workspaceDest := filepath.Join(req.Plan.WorkspacePath, usedItem.TargetPath)
	if journal != nil {
		_ = journal.record(workspaceDest)
	}
	if writeErr := mkdirAndWrite(workspaceDest, probeContent); writeErr == nil {
		return req.Plan.WorkspacePath, domain.FallbackNone, nil, contentErrors, usedItem, nil
	} else {
		probeErr = writeErr
	}

	// Tier 2: MOSAIC-root fallback.
	mosaicFallback := mosaicFallbackRoot(req.MosaicRoot, req.Plan.WorkspacePath)
	mosaicDest := filepath.Join(mosaicFallback, usedItem.TargetPath)
	if writeErr := mkdirAndWrite(mosaicDest, probeContent); writeErr == nil {
		return mosaicFallback, domain.FallbackMosaicRoot, probeErr, contentErrors, usedItem, nil
	}

	// Tier 3: OS-temp fallback.
	tempFallback := tempFallbackRoot(req.Plan.WorkspacePath)
	tempDest := filepath.Join(tempFallback, usedItem.TargetPath)
	if writeErr := mkdirAndWrite(tempDest, probeContent); writeErr == nil {
		return tempFallback, domain.FallbackTemp, probeErr, contentErrors, usedItem, nil
	}

	return "", "", nil, contentErrors, domain.PlanItem{}, ErrNoWritableLocation
}

// probeEligibleItems returns all plan items eligible for use as a writability probe,
// in plan order. Eligible items have ActionCreate or ActionUpdate and are not hooks.
// Hook items are excluded because their SourcePath is a bundle directory that the
// catalog never registers as a file, so calling req.Content on them always returns
// a catalog error. Returns nil when no items are eligible.
func probeEligibleItems(items []domain.PlanItem) []domain.PlanItem {
	var eligible []domain.PlanItem
	for _, item := range items {
		if item.Action == domain.ActionCreate || item.Action == domain.ActionUpdate {
			if item.Ref.Kind != domain.ArtifactHook {
				eligible = append(eligible, item)
			}
		}
	}
	return eligible
}

// firstProbeItem returns the first probe-eligible item, or false if none exist.
// Retained for callers that need to identify the first candidate without iterating
// (e.g. the atomic-mode journal pre-record, which now delegates to resolveDeploymentRoot).
func firstProbeItem(items []domain.PlanItem) (domain.PlanItem, bool) {
	eligible := probeEligibleItems(items)
	if len(eligible) == 0 {
		return domain.PlanItem{}, false
	}
	return eligible[0], true
}

// mkdirAndWrite creates parent directories and writes content to path.
func mkdirAndWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// mosaicFallbackRoot returns the MOSAIC-root fallback deployment directory:
// <mosaicRoot>/MosaicDeploy/fallback/<workspace-slug>
func mosaicFallbackRoot(mosaicRoot, workspacePath string) string {
	return filepath.Join(mosaicRoot, "MosaicDeploy", "fallback", workspaceSlug(workspacePath))
}

// tempFallbackRoot returns the OS-temp fallback deployment directory:
// <os.TempDir()>/mosaic-deploy/<workspace-slug>
func tempFallbackRoot(workspacePath string) string {
	return filepath.Join(os.TempDir(), "mosaic-deploy", workspaceSlug(workspacePath))
}

// workspaceSlug derives a filesystem-safe directory name from a workspace path.
func workspaceSlug(workspacePath string) string {
	base := filepath.Base(workspacePath)
	var sb strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	s := sb.String()
	if s == "" || s == "." || s == "-" {
		s = "workspace"
	}
	return s
}
