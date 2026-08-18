package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-deploy/internal/domain"
)

// resolveDeploymentRoot determines the deployment root by probing writability with
// the first Create or Update plan item. If the workspace probe fails, the MOSAIC-root
// fallback is tried, then the OS-temp fallback. The probe error (workspace write failure)
// is returned so callers can record it as a partial failure even when fallback succeeds.
// For DryRun or plans with no writable items, the workspace is used without probing.
func resolveDeploymentRoot(req ExecRequest) (root string, fallback domain.FallbackTier, probeErr error, err error) {
	if req.DryRun {
		return req.Plan.WorkspacePath, domain.FallbackNone, nil, nil
	}

	probeItem, hasProbe := firstProbeItem(req.Plan.Items)
	if !hasProbe {
		// No Create/Update items; use workspace as the deployment root.
		return req.Plan.WorkspacePath, domain.FallbackNone, nil, nil
	}

	content, err := req.Content(probeItem)
	if err != nil {
		return "", "", nil, fmt.Errorf("content for probe item %q: %w", probeItem.Ref.Key, err)
	}

	// Tier 1: workspace.
	workspaceDest := filepath.Join(req.Plan.WorkspacePath, probeItem.TargetPath)
	if writeErr := mkdirAndWrite(workspaceDest, content); writeErr == nil {
		return req.Plan.WorkspacePath, domain.FallbackNone, nil, nil
	} else {
		probeErr = writeErr
	}

	// Tier 2: MOSAIC-root fallback.
	mosaicFallback := mosaicFallbackRoot(req.MosaicRoot, req.Plan.WorkspacePath)
	mosaicDest := filepath.Join(mosaicFallback, probeItem.TargetPath)
	if writeErr := mkdirAndWrite(mosaicDest, content); writeErr == nil {
		return mosaicFallback, domain.FallbackMosaicRoot, probeErr, nil
	}

	// Tier 3: OS-temp fallback.
	tempFallback := tempFallbackRoot(req.Plan.WorkspacePath)
	tempDest := filepath.Join(tempFallback, probeItem.TargetPath)
	if writeErr := mkdirAndWrite(tempDest, content); writeErr == nil {
		return tempFallback, domain.FallbackTemp, probeErr, nil
	}

	return "", "", nil, ErrNoWritableLocation
}

// firstProbeItem returns the first ActionCreate or ActionUpdate item from the plan that
// is suitable for a writability probe (i.e. has content that can be fetched and written).
// Conflict and unchanged items are not used for probing.
// Hook items are also excluded: their SourcePath is a bundle directory that the catalog
// never registers as a file, so calling req.Content on them always returns a catalog error.
// Hook deployment is handled separately by deployHooks and must not be used to probe the
// workspace write path.
func firstProbeItem(items []domain.PlanItem) (domain.PlanItem, bool) {
	for _, item := range items {
		if item.Action == domain.ActionCreate || item.Action == domain.ActionUpdate {
			if item.Ref.Kind != domain.ArtifactHook {
				return item, true
			}
		}
	}
	return domain.PlanItem{}, false
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
