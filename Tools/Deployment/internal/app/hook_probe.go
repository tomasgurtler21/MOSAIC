package app

// hook_probe.go holds the layout-aware hook presence probe and the post-pass that applies it
// to a probe map. Hook files are written directly into the harness hooks directory (flat, or
// nested under lib/ for some harnesses), never into a <hooks dir>/<key> subdirectory, so
// presence is decided from the files of the harness's hook variant.

import (
	"os"
	"path/filepath"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// probeHookPlanPresence reports the on-disk state of one hook bundle as deployed by the
// harness described by hp. Present is true when at least one hp.Files entry exists as a regular
// file at <workspace>/<hp.TargetDir>/<TargetName>; an unsupported plan, a plan without files,
// and a plan whose files are all missing are absent.
//
// ContentHash is manifest.Hash(nil) when present, matching the executor's hook manifest
// convention, and every version field stays empty: the manifest entry is the only version
// source for hooks. It never returns an error; any I/O failure yields absent.
func probeHookPlanPresence(workspace string, hp domain.HookPlan) domain.DeployedArtifactState {
	if !hp.Supported {
		return domain.DeployedArtifactState{}
	}
	for _, f := range hp.Files {
		info, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(hp.TargetDir), filepath.FromSlash(f.TargetName)))
		if err == nil && info.Mode().IsRegular() {
			return domain.DeployedArtifactState{Present: true, ContentHash: manifest.Hash(nil)}
		}
	}
	return domain.DeployedArtifactState{}
}

// hookPlansByTargetPath resolves the HookPlan of every bundle in hooks and keys it by the
// bundle's planned hook target path. Bundles with no planned path or whose HookPlan cannot be
// resolved are omitted.
func hookPlansByTargetPath(
	module domain.HarnessModule,
	hooks []domain.HookBundle,
	paths plan.PlannedPaths,
	scope domain.Scope,
) map[string]domain.HookPlan {
	result := make(map[string]domain.HookPlan, len(hooks))
	for _, h := range hooks {
		targetPath, ok := paths.Path(domain.ArtifactRef{Kind: domain.ArtifactHook, Key: h.Key})
		if !ok {
			continue
		}
		hp, err := module.HookPlan(domain.HookPlanRequest{Bundle: h, Scope: scope})
		if err != nil {
			continue
		}
		result[targetPath] = hp
	}
	return result
}

// applyHookPresence is the hook post-pass: it overwrites the hook entries of state with the
// layout-aware presence probe and leaves every other entry untouched.
func applyHookPresence(
	workspace string,
	state map[string]domain.DeployedArtifactState,
	hookPlans map[string]domain.HookPlan,
) {
	if state == nil {
		return
	}
	for targetPath, hp := range hookPlans {
		state[targetPath] = probeHookPlanPresence(workspace, hp)
	}
}
