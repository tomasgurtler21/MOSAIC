package domain

// SourceKind classifies what a path turned out to be.
type SourceKind uint8

const (
	SourceNotFound  SourceKind = iota // nothing at the probed/supplied location
	SourceLogsRoot                    // a tree of run folders
	SourceSingleRun                   // the path is itself one run folder
	SourceUnusable                    // exists but is not a logs tree
)

// Source describes a resolved (or unresolved) log source. It is never an error.
type Source struct {
	Kind   SourceKind
	Path   string
	Reason string // human-readable, set only when Kind == SourceUnusable
}

// IsUsable reports whether the source Kind is LogsRoot or SingleRun.
func (s Source) IsUsable() bool {
	return s.Kind == SourceLogsRoot || s.Kind == SourceSingleRun
}

// AgentEntry is one agent-instance folder discovered by structure alone.
type AgentEntry struct {
	Dir string
	// InstanceHint is derived from the sanitized folder name. It is a HINT only:
	// where it disagrees with the agent_instance_id carried in the events,
	// the event field wins.
	InstanceHint AgentInstanceID
	EventFile    string // "" when the folder contains no event file
}

// RunEntry is one run folder (or the unattributable bucket) discovered by structure.
type RunEntry struct {
	Run              RunRef
	Dir              string
	OrchestratorFile string   // "" when 00_orchestrator_events.jsonl is absent
	Agents           []AgentEntry // sorted by Dir
}

// Inventory is the complete structural picture of a source. No file contents
// were read to produce it.
type Inventory struct {
	Source         Source
	Runs           []RunEntry // named runs only, sorted by run id
	Unattributable *RunEntry  // nil when no unknown-run/ folder exists
}

// IsEmpty reports the explicit no-data outcome: a usable source containing
// neither runs nor an unattributable bucket. Distinct from SourceNotFound.
func (i Inventory) IsEmpty() bool {
	return len(i.Runs) == 0 && i.Unattributable == nil
}

// StreamKind names which event stream a file is.
type StreamKind uint8

const (
	StreamOrchestrator  StreamKind = iota
	StreamAgentInstance
)

// StreamRef is the provenance the reader/app attaches to every event handed to
// the analysis core. It is how the pure core knows which run and which actor a
// stream belongs to without performing any I/O of its own.
type StreamRef struct {
	Run          RunRef
	Kind         StreamKind
	InstanceHint AgentInstanceID // set only for StreamAgentInstance
	Path         string
}
