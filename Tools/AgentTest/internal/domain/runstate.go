package domain

import (
	"encoding/json"
	"time"
)

// RunIDPlaceholder is expanded to the run's identifier in any authored path
// that may need to name a directory which only exists once the run identity
// is known (e.g. an orchestration document under Orchestration-{run_id}/).
//
// It is expanded in two authored surfaces and no others:
//   - a seed file's declared path
//   - a stub side effect's declared create-file path
//
// It is NOT expanded in stub response content. A stub's response is returned
// to the subject verbatim; the subject carries its own run identity and any
// substitution there would corrupt the reply the test is measuring.
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
