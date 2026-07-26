package logging

import (
	"time"

	"mosaic-deploy/internal/domain"
)

// Logger records deployment run events to two log sinks. All methods return nothing; write
// failures are accumulated in Degraded and surfaced in the run summary rather than propagated,
// so logging can never abort or alter a deployment (CD-10, AC15.4).
type Logger interface {
	// StartRun begins a new run log entry in both sinks. The latest sink is truncated first;
	// the history sink is appended. Called once per run, before any Event or Action call.
	StartRun(RunContext)
	// Event records a diagnostic event within the current run.
	Event(Event)
	// Action records the outcome of one deployment item, including update attribution deltas.
	Action(domain.ActionRecord)
	// EndRun closes the current run record with its outcome.
	EndRun(domain.Outcome)
	// Paths returns the absolute paths of the two sink files.
	Paths() domain.LogPaths
	// Degraded returns any non-fatal write failures accumulated during this run. A non-empty
	// slice is included in RunSummary.LogDegraded and shown to the user at run end.
	Degraded() []error
}

// RunContext carries the run-level fields recorded once per run in both sinks.
type RunContext struct {
	RunID          string
	StartedAt      time.Time
	Mode           domain.RunMode
	Harness        domain.HarnessRef
	External       *domain.ExternalModuleInfo // non-nil when an external harness module ran
	WorkspacePath  string
	DeploymentRoot string
	Fallback       domain.FallbackTier
}

// Level is the severity of a log event.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event is one structured log record within a run.
type Event struct {
	Time    time.Time
	Level   Level
	Kind    string // "plan", "transform", "write", "backup", "skip", "registration", "fallback"
	Subject string
	Message string
	// Changes names the version fields that drove an update action and their before/after
	// values. Non-empty only for update events (AC15.2).
	Changes []domain.VersionDelta
	Fields  map[string]string
}
