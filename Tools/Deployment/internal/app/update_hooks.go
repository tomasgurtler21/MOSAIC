package app

// update_hooks.go holds the hook-specific steps of the update flow: discovery of the hook
// bundles already deployed in the workspace, and filtering of the reviewed plan and the
// executor request down to the hooks the update actually writes.

import (
	"mosaic-deploy/internal/domain"
)

// deployedHookIDs returns the keys of the Catalog hook bundles that are already deployed for
// the harness: those with at least one file of the harness's hook variant on disk. It reads
// the files directly because it runs before the artifact set (and so the probe map) exists.
// Harnesses whose HookPlan is unsupported (runtime-provisioned, descriptor-only) never yield
// a deployed hook, whatever the manifest records.
func (s *service) deployedHookIDs(module domain.HarnessModule, workspace string, scope domain.Scope) []string {
	var ids []string
	for _, h := range s.deps.Catalog.Hooks() {
		hp, err := module.HookPlan(domain.HookPlanRequest{Bundle: h, Scope: scope})
		if err != nil {
			continue
		}
		if probeHookPlanPresence(workspace, hp).Present {
			ids = append(ids, h.Key)
		}
	}
	return ids
}

// writtenHookBundles returns the hooks the update will write: hook items planned as an
// update, or as a conflict whose decision is overwrite or backup. Unchanged hooks and hooks
// whose conflict was skipped are excluded.
func writtenHookBundles(
	hooks []domain.HookBundle,
	items []domain.PlanItem,
	conflicts map[string]domain.ConflictDecision,
) []domain.HookBundle {
	writtenKeys := make(map[string]bool, len(hooks))
	for _, item := range items {
		if item.Ref.Kind != domain.ArtifactHook {
			continue
		}
		switch item.Action {
		case domain.ActionUpdate, domain.ActionCreate:
			writtenKeys[item.Ref.Key] = true
		case domain.ActionConflict:
			d := conflicts[item.TargetPath]
			if d == domain.DecisionOverwrite || d == domain.DecisionBackupThenOverwrite {
				writtenKeys[item.Ref.Key] = true
			}
		}
	}
	var written []domain.HookBundle
	for _, h := range hooks {
		if writtenKeys[h.Key] {
			written = append(written, h)
		}
	}
	return written
}

// dropUnwrittenHookRegistrations removes, from the plan, the registration gaps and
// registration steps of every hook in hooks that is not in written.
func dropUnwrittenHookRegistrations(
	p *domain.Plan,
	module domain.HarnessModule,
	hooks, written []domain.HookBundle,
	scope domain.Scope,
) {
	writtenKeys := make(map[string]bool, len(written))
	for _, h := range written {
		writtenKeys[h.Key] = true
	}
	dropped := map[string]bool{}
	for _, h := range hooks {
		if writtenKeys[h.Key] {
			continue
		}
		hp, err := module.HookPlan(domain.HookPlanRequest{Bundle: h, Scope: scope})
		if err != nil {
			continue
		}
		for _, step := range hp.Registration {
			dropped[step.ID] = true
		}
	}
	if len(dropped) == 0 {
		return
	}
	gaps := make([]domain.Gap, 0, len(p.Gaps))
	for _, g := range p.Gaps {
		if g.Kind == domain.GapHookRegistration && dropped[g.Subject] {
			continue
		}
		gaps = append(gaps, g)
	}
	p.Gaps = gaps
	var steps []domain.RegistrationStep
	for _, step := range p.Registrations {
		if !dropped[step.ID] {
			steps = append(steps, step)
		}
	}
	p.Registrations = steps
}
