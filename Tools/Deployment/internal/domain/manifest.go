package domain

import "time"

// VersionStamp carries the source version fields for one deployed artifact.
// It is supplied per-item by the caller (typically the app layer) so the executor
// can record current source versions in ManifestEntry, enabling the planner to
// detect staleness on subsequent runs.
type VersionStamp struct {
	Version           string // catalog version of the artifact (agents, skills, hooks)
	TransformVersion  string // transform engine version (agents only)
	InjectionsVersion string // injections version (agents only)
}

// Manifest is the deployment tool's record of every artifact it has written to a workspace.
// It is the authoritative source for staleness detection and conflict classification.
type Manifest struct {
	SchemaVersion string
	HarnessID     string
	UpdatedAt     time.Time
	Entries       []ManifestEntry // sorted by TargetPath
}

// ManifestEntry records one deployed artifact: its identity, the path it was written to,
// the version stamps at the time of deployment, and a content hash for conflict detection.
type ManifestEntry struct {
	Ref               ArtifactRef
	TargetPath        string    // relative to the deployment root
	Version           string    // agents, skills, hook bundles
	TransformVersion  string    // agents only
	InjectionsVersion string    // agents only
	ContentHash       string    // "sha256:<hex>" over the exact deployed bytes (CD-11)
	DeployedAt        time.Time
}

// Lookup finds the manifest entry for one artifact. The second return value is false when
// no entry for the given ref exists.
func (m Manifest) Lookup(ref ArtifactRef) (ManifestEntry, bool) {
	for _, e := range m.Entries {
		if e.Ref == ref {
			return e, true
		}
	}
	return ManifestEntry{}, false
}
