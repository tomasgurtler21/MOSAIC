// Package runstate implements the crash-safe, lock-guarded run state
// document that is the only channel through which independent interceptor
// processes share state.
package runstate

import (
	"errors"
	"time"

	"mosaic-agent-test/internal/domain"
)

// Store is the crash-safe, lock-guarded run state document.
// Every mutation goes through Update; there is no unguarded write path.
type Store interface {
	// Initialize writes the first state document. It fails if one already
	// exists, so a second setup for the same run is a loud error.
	Initialize(s domain.RunState) error

	// Read returns the current state without taking the lock.
	// Returns ErrStateAbsent, ErrStateCorrupt or ErrStateUnreadable as
	// distinct, errors.Is-matchable conditions.
	Read() (domain.RunState, error)

	// Update performs a lock-guarded read-modify-write. mutate receives the
	// current state and returns the state to commit. The commit is atomic
	// (write-temp-then-rename), so no reader can observe a partial document
	// and an interrupted commit leaves the previous state readable.
	//
	// mutate MUST NOT perform I/O: the lock sits on the critical path of a
	// live agent turn and the guarded section is a read-modify-write of one
	// small document.
	//
	// When the lock was reclaimed from a dead or stale holder, the returned
	// UpdateResult reports it. Update does not itself write to the
	// invocation log; the caller appends the run-level record, because
	// runstate may not import invlog.
	Update(mutate func(domain.RunState) (domain.RunState, error)) (UpdateResult, error)
}

// UpdateResult is what Update reports about one lock-guarded mutation.
type UpdateResult struct {
	State         domain.RunState
	LockReclaimed bool
	PriorHolder   domain.LockInfo // populated only when LockReclaimed
}

// Lock protocol constants, fixed by design.
const (
	AcquireTimeout     = 5 * time.Second
	StalenessThreshold = 30 * time.Second
)

// Distinct read conditions. Conflating them makes every later diagnosis
// wrong: absent means setup never ran, corrupt means setup ran and
// something broke.
var (
	ErrStateAbsent     = errors.New("runstate: state document absent")
	ErrStateCorrupt    = errors.New("runstate: state document corrupt")
	ErrStateUnreadable = errors.New("runstate: state document unreadable")
	ErrLockTimeout     = errors.New("runstate: lock acquisition timed out")
	ErrLockLost        = errors.New("runstate: lock was reclaimed by another holder")
)

// NewStore constructs a Store rooted at controlDir, using clock for lock
// acquisition timing and staleness evaluation.
func NewStore(controlDir string, clock domain.Clock) Store {
	return &store{controlDir: controlDir, clock: clock}
}
