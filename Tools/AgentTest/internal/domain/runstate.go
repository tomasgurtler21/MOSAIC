package domain

import (
	"encoding/json"
	"time"
)

// RunIDPlaceholder is expanded to the run's identifier in every authored
// surface whose content may reference the run's identity:
//
//   - a seed file's declared path
//   - a seed file's content (inline or $ref-resolved)
//   - a stub side effect's declared create-file path
//   - a stub side effect's content (inline or $ref-resolved)
//   - a stub's response content (the JSON returned to the subject)
//   - the registry's generic response content
//
// A stub's response is test-author-authored fixture content simulating
// what a sub-agent would say. Expanding {run_id} in it resolves a
// placeholder the test author deliberately wrote so the same fixture
// works across different run IDs. This does not alter anything the
// subject generated -- only framework-controlled content.
const RunIDPlaceholder = "{run_id}"

// RunState is the document shared between the driver and every interceptor
// process. It is read-modify-written as a unit, which is why it is one JSON
// document rather than a directory of shards.
type RunState struct {
	SchemaVersion int    `json:"schema_version"`
	TestID        string `json:"test_id"`
	RunNumber     int    `json:"run_number"`

	// RunID is the run's identifier, persisted by the driver at setup so
	// every short-lived interceptor process can read it back. An interceptor
	// process has no other reach to it: it receives only its workspace,
	// control directory, harness and phase. This is the same reason TestID
	// and RunNumber live here rather than arriving as flags.
	RunID string `json:"run_id"`

	// SequenceCounter is the global 1-based invocation counter. It names
	// invocations; it is never used to match a stub.
	SequenceCounter int `json:"sequence_counter"`

	// CollaboratorCounters maps CollaboratorIdentity.Key() to the number of
	// invocations seen for that identity. The per-identity ordinal used for
	// matching is this value plus one.
	CollaboratorCounters map[string]int `json:"collaborator_counters"`

	// PendingStubs maps correlation token to the stub awaiting its echo check.
	PendingStubs map[string]PendingStub `json:"pending_stubs"`

	// InFlight maps correlation token to the invocation currently executing.
	InFlight map[string]InFlight `json:"in_flight"`

	// EarlyExitThreshold is the invocation count after which every further
	// call halts. Zero means no threshold.
	EarlyExitThreshold int  `json:"early_exit_threshold"`
	EarlyExitTriggered bool `json:"early_exit_triggered"`
	TurnLimit          int  `json:"turn_limit"`

	// UnclaimedDispatches is the FIFO queue of correlation tokens for
	// dispatches that have not yet been claimed by an agent-start event.
	// The agent-start phase pops from the head of this queue and binds the
	// popped token to the agent identifier the event carries.
	//
	// Nil and empty are equivalent: no unclaimed dispatches.
	UnclaimedDispatches []string `json:"unclaimed_dispatches,omitempty"`

	// AgentDispatch maps each agent identifier to the correlation token of
	// the dispatch it is executing, established by the agent-start phase.
	// The completion phase uses this map to correlate a completion event
	// (which carries an agent identifier but no dispatch identifier) back to
	// its originating dispatch.
	//
	// Nil and empty are equivalent: no agent bindings exist.
	AgentDispatch map[string]string `json:"agent_dispatch,omitempty"`
}

// PendingStub is a stub awaiting its echo check.
type PendingStub struct {
	Seq      int                  `json:"seq"`
	Identity CollaboratorIdentity `json:"identity"`
	Expected json.RawMessage      `json:"expected"`
}

// InFlight is an invocation currently executing.
type InFlight struct {
	Seq       int                  `json:"seq"`
	Identity  CollaboratorIdentity `json:"identity"`
	Group     string               `json:"group,omitempty"`
	StartedAt time.Time            `json:"started_at"`
}

// StateDelta is the mutation a decision implies. The pure core returns it;
// the imperative shell applies it inside the lock.
type StateDelta struct {
	SequenceIncrement      int
	CollaboratorIncrements map[string]int
	AddPending             map[string]PendingStub
	ResolvePending         []string
	MarkInFlight           map[string]InFlight
	ClearInFlight          []string
	SetEarlyExitTriggered  bool

	// EnqueueUnclaimed appends these correlation tokens to the tail of
	// UnclaimedDispatches. Applied by the pre-invocation phase so the
	// agent-start phase can pop them.
	EnqueueUnclaimed []string

	// DequeueUnclaimed removes this many entries from the head of
	// UnclaimedDispatches. Applied by the agent-start phase when it claims
	// a dispatch.
	DequeueUnclaimed int

	// BindAgent merges these agent-identifier-to-token mappings into
	// AgentDispatch. Applied by the agent-start phase to record which dispatch
	// each agent is executing.
	BindAgent map[string]string

	// ReleaseAgents removes these agent identifiers from AgentDispatch. Applied
	// by the completion phase after the association has been used so the
	// identifiers do not match a later dispatch erroneously.
	ReleaseAgents []string
}

// Apply is a pure state transition, so a test can assert on a delta without a
// filesystem and the shell has exactly one way to mutate state.
//
// Apply never mutates s: every map it touches is copied first, so the
// receiver remains valid for a caller that still holds it (e.g. a retried
// read-modify-write).
func (s RunState) Apply(d StateDelta) RunState {
	next := s

	next.CollaboratorCounters = cloneIntMap(s.CollaboratorCounters)
	next.PendingStubs = clonePendingStubMap(s.PendingStubs)
	next.InFlight = cloneInFlightMap(s.InFlight)
	next.AgentDispatch = cloneStringMap(s.AgentDispatch)

	next.SequenceCounter += d.SequenceIncrement

	for key, inc := range d.CollaboratorIncrements {
		if next.CollaboratorCounters == nil {
			next.CollaboratorCounters = map[string]int{}
		}
		next.CollaboratorCounters[key] += inc
	}

	for token, pending := range d.AddPending {
		if next.PendingStubs == nil {
			next.PendingStubs = map[string]PendingStub{}
		}
		next.PendingStubs[token] = pending
	}
	for _, token := range d.ResolvePending {
		delete(next.PendingStubs, token)
	}

	for token, inFlight := range d.MarkInFlight {
		if next.InFlight == nil {
			next.InFlight = map[string]InFlight{}
		}
		next.InFlight[token] = inFlight
	}
	for _, token := range d.ClearInFlight {
		delete(next.InFlight, token)
	}

	if d.SetEarlyExitTriggered {
		next.EarlyExitTriggered = true
	}

	if len(d.EnqueueUnclaimed) > 0 {
		next.UnclaimedDispatches = append(append([]string(nil), next.UnclaimedDispatches...), d.EnqueueUnclaimed...)
	}

	if d.DequeueUnclaimed > 0 {
		n := d.DequeueUnclaimed
		if n > len(next.UnclaimedDispatches) {
			n = len(next.UnclaimedDispatches)
		}
		next.UnclaimedDispatches = append([]string(nil), next.UnclaimedDispatches[n:]...)
	}

	for agentID, token := range d.BindAgent {
		if next.AgentDispatch == nil {
			next.AgentDispatch = map[string]string{}
		}
		next.AgentDispatch[agentID] = token
	}

	for _, agentID := range d.ReleaseAgents {
		delete(next.AgentDispatch, agentID)
	}

	return next
}

func cloneIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func clonePendingStubMap(m map[string]PendingStub) map[string]PendingStub {
	if m == nil {
		return nil
	}
	out := make(map[string]PendingStub, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneInFlightMap(m map[string]InFlight) map[string]InFlight {
	if m == nil {
		return nil
	}
	out := make(map[string]InFlight, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// NextOrdinal reports the per-identity ordinal the next call to id will
// carry.
func (s RunState) NextOrdinal(id CollaboratorIdentity) int {
	return s.CollaboratorCounters[id.Key()] + 1
}

// LockInfo records who holds the runstate lock, so liveness and staleness
// are decidable by another process.
type LockInfo struct {
	PID        int       `json:"pid"`
	Host       string    `json:"host"`
	AcquiredAt time.Time `json:"acquired_at"`
	ControlDir string    `json:"control_dir"`
	Nonce      string    `json:"nonce"` // distinguishes reclaim attempts
}
