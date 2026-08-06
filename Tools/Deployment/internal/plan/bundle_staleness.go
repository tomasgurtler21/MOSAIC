package plan

import "mosaic-deploy/internal/domain"

// BundleDeltaField is the VersionDelta.Field name used for bundle staleness. It is distinct
// from every frontmatter stamp field name, from WorkflowDeltaFieldPrefix, and from
// ProtocolDeltaField, so the executor's resolveVersionStamp switch cannot mistake it for a
// frontmatter stamp.
const BundleDeltaField = "bundle_version"

// BundleDrift records the difference between a deployed file's bundle version and the
// bundle's current version. The zero value means no drift.
type BundleDrift struct {
	Deployed string // from the deployed file's `bundle_version` frontmatter; "" when absent
	Source   string // the bundle's current bundle_version
	Applies  bool   // false when this agent's role receives no bundle block
}

// BundleStaleness compares a deployed artifact's bundle version against the bundle's.
// applies is false for a role the bundle declares no block for (the orchestrator today);
// such an agent is never stale for a bundle reason and never carries the stamp.
//
// Implementation body added in I6.7.
func BundleStaleness(deployed domain.DeployedArtifactState, sourceVersion string, applies bool) BundleDrift {
	// Implementation stub — body added in I6.7.
	return BundleDrift{
		Deployed: deployed.BundleVersion,
		Source:   sourceVersion,
		Applies:  applies,
	}
}

// Stale reports whether the deployed and source bundle versions differ. Always false when
// Applies is false (the agent's role receives no bundle block).
func (d BundleDrift) Stale() bool {
	if !d.Applies {
		return false
	}
	return d.Deployed != d.Source
}

// Delta returns the VersionDelta for the drift; ok is false when not stale.
// The returned delta's Field is always BundleDeltaField.
func (d BundleDrift) Delta() (domain.VersionDelta, bool) {
	if !d.Stale() {
		return domain.VersionDelta{}, false
	}
	return domain.VersionDelta{
		Field:    BundleDeltaField,
		Deployed: d.Deployed,
		Source:   d.Source,
	}, true
}

// Reason returns a one-line human-readable string describing the bundle staleness.
// Returns "" when not stale.
//
// Format examples:
//
//	"bundle version changed (1.0.0 -> 1.1.0)"
//	"deployed file carries no bundle version stamp (source 1.0.0)"
func (d BundleDrift) Reason() string {
	if !d.Stale() {
		return ""
	}
	if d.Deployed == "" {
		return "deployed file carries no bundle version stamp (source " + d.Source + ")"
	}
	return "bundle version changed (" + d.Deployed + " -> " + d.Source + ")"
}
