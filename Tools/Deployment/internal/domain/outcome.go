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
}
