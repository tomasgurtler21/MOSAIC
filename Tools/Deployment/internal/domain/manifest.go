package domain

import (
	"strings"
	"time"
)

// VersionStamp carries the source version fields for one deployed artifact.
// It is supplied per-item by the caller (typically the app layer) so the executor
// can record current source versions in ManifestEntry, enabling the planner to
// detect staleness on subsequent runs.
type VersionStamp struct {
	Version                       string // catalog version of the artifact (agents, skills, hooks)
	HarnessVersion                string // transform engine version (agents only); written as mosaic_harness_version
	InjectionsVersion             string // role-selected injection version (agents only)
	OrchestratorInjectionsVersion string // reserved; no longer populated after Stage 3
	ToolMappingsVersion           string // hash of the effective tool-destination mapping set; agents only; empty when no config mappings
}

// Manifest is the deployment tool's bookkeeping record for every artifact it has written to
// a workspace. It records harness identity, known-entry listing, the content hash for each
// entry (used for local-modification conflict detection), and absent/corrupt-state detection.
// It is not the source of truth for version comparison — deployed file versions are read
// directly from the deployed files by the app layer (DeployedArtifactState).
type Manifest struct {
	SchemaVersion string
	HarnessID     string
	UpdatedAt     time.Time
	Entries       []ManifestEntry // sorted by TargetPath
}

// ManifestEntry records one deployed artifact: its identity, the path it was written to,
// the version stamps at the time of deployment, and a content hash for conflict detection.
type ManifestEntry struct {
	Ref                           ArtifactRef
	TargetPath                    string    // relative to the deployment root
	Version                       string    // agents, skills, hook bundles
	HarnessVersion                string    // agents only; written as harness_version in manifest YAML
	InjectionsVersion             string    // agents only; role-selected injection version
	OrchestratorInjectionsVersion string    // reserved; no longer populated after Stage 3
	ToolMappingsVersion           string    // agents only; hash of the effective tool-destination mapping set at deploy time
	ContentHash                   string    // "sha256:<hex>" over the exact deployed bytes (CD-11)
	DeployedAt                    time.Time
}

// Lookup finds the manifest entry for one artifact. The second return value is false when
// no entry for the given ref exists. Callers outside the planner (for example the executor's
// entry map) use this method; see LookupAt for the target-path-aware variant used by the
// planner's classifiers.
func (m Manifest) Lookup(ref ArtifactRef) (ManifestEntry, bool) {
	for _, e := range m.Entries {
		if e.Ref == ref {
			return e, true
		}
	}
	return ManifestEntry{}, false
}

// LookupAt finds the manifest entry for one artifact recorded at the given target path.
// It returns false when no entry matches both the ref and the path — including the case
// where an entry exists for the same Kind+Key but was deployed under a different harness
// (and therefore a different target path).
//
// Paths are compared after normalising backslashes to forward slashes; the comparison is
// otherwise exact and case-sensitive.
func (m Manifest) LookupAt(ref ArtifactRef, targetPath string) (ManifestEntry, bool) {
	normTarget := strings.ReplaceAll(targetPath, "\\", "/")
	for _, e := range m.Entries {
		if e.Ref == ref && strings.ReplaceAll(e.TargetPath, "\\", "/") == normTarget {
			return e, true
		}
	}
	return ManifestEntry{}, false
}
