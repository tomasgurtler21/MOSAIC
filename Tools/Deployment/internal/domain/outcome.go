package domain

// FallbackTier records which deployment location was ultimately used when the primary location
// was not writable.
type FallbackTier string

const (
	FallbackNone       FallbackTier = "workspace"
	FallbackMosaicRoot FallbackTier = "mosaic-root"
	FallbackTemp       FallbackTier = "temp"
)

// Outcome is the high-level result of a deployment run.
type Outcome string

const (
	OutcomeSuccess           Outcome = "success"
	OutcomeCompletedWithGaps Outcome = "completed-with-skips"
	OutcomeFailed            Outcome = "failed"
)

// ActionTaken records what the executor actually did for one artifact.
type ActionTaken string

const (
	TakenCreated   ActionTaken = "created"
	TakenUpdated   ActionTaken = "updated"
	TakenUnchanged ActionTaken = "unchanged"
	TakenSkipped   ActionTaken = "skipped"
	TakenBackedUp  ActionTaken = "backed-up-and-overwritten"
	TakenFailed    ActionTaken = "failed"
)

// ActionRecord is what actually happened to one file. It is the shared currency between the
// executor, the logger, and the summary renderer (CD-9).
type ActionRecord struct {
	Ref        ArtifactRef
	TargetPath string // absolute, as written
	Taken      ActionTaken
	Stale      []VersionDelta
	BackupPath string
	Err        string // non-empty only for TakenFailed

	// SourceVersion is the source artifact's declared version (frontmatter `version`).
	// Populated for agent artifacts when the source declares one; empty otherwise.
	// This is distinct from Stale, which records staleness deltas, not the declared version.
	SourceVersion string
}

// ExternalModuleInfo carries identity information about the external harness process, for logging
// and for the run summary.
type ExternalModuleInfo struct {
	HarnessID       string
	ExecutablePath  string
	ProtocolVersion string
}

// LogPaths holds the two log file paths a Logger always writes.
type LogPaths struct {
	Latest  string
	History string
}

// OwnedKeyDifference names one MOSAIC-owned frontmatter key whose decoded canonical
// value in the deployed file differs from the value being written in this run. Only
// populated for artifacts whose origin the plan layer could not confirm. The difference
// asserts nothing about who changed what; it reports only what was found.
type OwnedKeyDifference struct {
	Key             string // the frontmatter key
	Deployed        string // prior on-disk value (decoded canonical); "" when DeployedPresent is false
	Incoming        string // value this run writes; "" when IncomingPresent is false
	Reason          string // human-readable explanation
	DeployedPresent bool   // false when the key was absent from the deployed file
	IncomingPresent bool   // false when the key is absent from the incoming form
}

// RunSummary is the single structure both frontends render and the only value the app returns.
// It is also the JSON document emitted by cli --output json.
type RunSummary struct {
	Mode           RunMode
	Harness        HarnessRef
	WorkspacePath  string
	DeploymentRoot string
	Fallback       FallbackTier
	External       *ExternalModuleInfo
	Actions        []ActionRecord
	Todos          []TodoItem
	TodoFilePath   string
	Logs           LogPaths
	LogDegraded    []string // non-fatal logging failures
	Outcome        Outcome

	// OwnedKeyDifferences accumulates every owned-key difference emitted during this run.
	// Non-empty only when one or more artifacts were conflict-classified and had parseable
	// deployed bytes. Empty on every ordinary run and when all conflicts are resolved by skip.
	OwnedKeyDifferences []OwnedKeyDifference
}
