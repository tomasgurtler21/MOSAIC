package plan

import "mosaic-deploy/internal/domain"

// AgentStaleness compares the three agent version fields independently and returns one delta
// per mismatching field, in the fixed order version, transform_version, injections_version.
// An empty result means not stale; ActionUnchanged is the appropriate action.
//
// The stamps parameter carries the version values that would be written on a fresh deploy:
//   - stamps.Version is the agent's current source version
//   - stamps.TransformVersion is the harness's current transform version
//   - stamps.InjectionsVersion is the harness's current injections version
//
// The agent parameter is accepted for future extension (for example, key-based log
// attribution or per-agent version overrides) but is not used by the current
// implementation. It is explicitly blanked with _ to signal this intent.
func AgentStaleness(entry domain.ManifestEntry, _ domain.Agent, stamps domain.VersionStamps) []domain.VersionDelta {
	var deltas []domain.VersionDelta

	if entry.Version != stamps.Version {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "version",
			Deployed: entry.Version,
			Source:   stamps.Version,
		})
	}
	if entry.TransformVersion != stamps.TransformVersion {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "transform_version",
			Deployed: entry.TransformVersion,
			Source:   stamps.TransformVersion,
		})
	}
	if entry.InjectionsVersion != stamps.InjectionsVersion {
		deltas = append(deltas, domain.VersionDelta{
			Field:    "injections_version",
			Deployed: entry.InjectionsVersion,
			Source:   stamps.InjectionsVersion,
		})
	}

	return deltas
}

// SkillStaleness compares the single version field of a skill against the manifest entry.
// Returns at most one delta. An empty result means not stale.
func SkillStaleness(entry domain.ManifestEntry, skill domain.Skill) []domain.VersionDelta {
	if entry.Version != skill.Version {
		return []domain.VersionDelta{{
			Field:    "version",
			Deployed: entry.Version,
			Source:   skill.Version,
		}}
	}
	return nil
}

// HookStaleness compares the single version field of a hook bundle against the manifest
// entry. Returns at most one delta. The bundle version covers every file in the bundle.
func HookStaleness(entry domain.ManifestEntry, bundle domain.HookBundle) []domain.VersionDelta {
	if entry.Version != bundle.Version {
		return []domain.VersionDelta{{
			Field:    "version",
			Deployed: entry.Version,
			Source:   bundle.Version,
		}}
	}
	return nil
}
