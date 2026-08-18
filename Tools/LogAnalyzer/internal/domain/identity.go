package domain

import "regexp"

// Harness names the adapter that emitted an event. Unrecognised values are
// preserved verbatim rather than rejected.
type Harness string

const (
	HarnessClaudeCode Harness = "claude-code"
	HarnessOpenCode   Harness = "opencode"
	HarnessVSCodeGHCP Harness = "vscode-ghcp"
)

// IsRecognised reports whether the harness value is one of the known adapters.
// Unrecognised harness values are tolerated (not rejected) by the decoder.
func (h Harness) IsRecognised() bool {
	return h == HarnessClaudeCode || h == HarnessOpenCode || h == HarnessVSCodeGHCP
}

// ModelID is a model identifier as it appears in the logs and as it keys the
// pricing table. Harness is never part of this key.
type ModelID string

// IsEmpty reports whether the model identifier is the empty string.
func (m ModelID) IsEmpty() bool { return m == "" }

// AgentInstanceID is the "{AgentName}#{Number}" instance identifier.
type AgentInstanceID string

// RunKind distinguishes a named run from the unattributable degradation bucket.
type RunKind uint8

const (
	RunNamed RunKind = iota
	RunUnattributable
)

// UnknownRunDirName is the on-disk folder name of the degradation bucket.
const UnknownRunDirName = "unknown-run"

// RunRef identifies a run source. The unattributable bucket is a distinct kind,
// never a run id, so downstream code cannot accidentally fold it into run totals.
type RunRef struct {
	Kind RunKind
	ID   string // set only when Kind == RunNamed
}

// NamedRun constructs a RunRef for a named run.
func NamedRun(id string) RunRef { return RunRef{Kind: RunNamed, ID: id} }

// UnattributableRun constructs the unattributable bucket RunRef.
func UnattributableRun() RunRef { return RunRef{Kind: RunUnattributable} }

// IsUnattributable reports whether the run is the unattributable bucket.
func (r RunRef) IsUnattributable() bool { return r.Kind == RunUnattributable }

// Label returns the ID for named runs, or "unattributable" for the bucket.
func (r RunRef) Label() string {
	if r.Kind == RunNamed {
		return r.ID
	}
	return "unattributable"
}

// runIDPattern matches {YYYYMMDD}T{HHMMSS}Z-{4-hex}.
var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$`)

// IsRunID reports whether s matches the {YYYYMMDD}T{HHMMSS}Z-{4-hex} run-id format.
func IsRunID(s string) bool { return runIDPattern.MatchString(s) }

// ActorKind distinguishes the orchestrator's own cost from a subagent instance's.
type ActorKind uint8

const (
	ActorOrchestrator  ActorKind = iota
	ActorAgentInstance
)

// ActorRef identifies whose cost a figure belongs to within a run.
type ActorRef struct {
	Kind     ActorKind
	Instance AgentInstanceID // set only when Kind == ActorAgentInstance
	Type     string          // agent_type from invocation_start, may be empty
}

// Orchestrator constructs an ActorRef for the orchestrator.
func Orchestrator() ActorRef { return ActorRef{Kind: ActorOrchestrator} }

// AgentInstance constructs an ActorRef for an agent instance.
func AgentInstance(id AgentInstanceID) ActorRef {
	return ActorRef{Kind: ActorAgentInstance, Instance: id}
}

// Label returns a human-readable label for the actor.
func (a ActorRef) Label() string {
	if a.Kind == ActorOrchestrator {
		return "orchestrator"
	}
	return string(a.Instance)
}
