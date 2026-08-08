package runstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-agent-test/internal/domain"
)

// store is the default Store implementation: a JSON document guarded by a
// lock file, committed via write-temp-then-rename so no reader ever observes
// a partial document.
type store struct {
	controlDir string
	clock      domain.Clock
}

func (s *store) statePath() string {
	return filepath.Join(s.controlDir, domain.StateFileName)
}

func (s *store) lockPath() string {
	return filepath.Join(s.controlDir, domain.LockFileName)
}

func (s *store) Initialize(state domain.RunState) error {
	if err := os.MkdirAll(s.controlDir, 0o755); err != nil {
		return fmt.Errorf("runstate: failed to create control dir %s: %w", s.controlDir, err)
	}

	f, err := os.OpenFile(s.statePath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("runstate: state document already exists at %s", s.statePath())
		}
		return fmt.Errorf("runstate: failed to create state document %s: %w", s.statePath(), err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(state); err != nil {
		return fmt.Errorf("runstate: failed to write state document %s: %w", s.statePath(), err)
	}
	return nil
}

func (s *store) Read() (domain.RunState, error) {
	return readState(s.statePath())
}

// readState reads and parses the state document at path, distinguishing
// absent, corrupt and unreadable as separate, errors.Is-matchable
// conditions. Conflating them makes every later diagnosis wrong: absent
// means setup never ran, corrupt means setup ran and something broke,
// unreadable means the document could not even be opened.
func readState(path string) (domain.RunState, error) {
	var data []byte
	err := retryTransientWindowsIO(func() error {
		var readErr error
		data, readErr = os.ReadFile(path)
		return readErr
	})
	if err != nil {
		if os.IsNotExist(err) {
			return domain.RunState{}, fmt.Errorf("%w: %s", ErrStateAbsent, path)
		}
		return domain.RunState{}, fmt.Errorf("%w: %s: %v", ErrStateUnreadable, path, err)
	}

	var state domain.RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return domain.RunState{}, fmt.Errorf("%w: %s: %v", ErrStateCorrupt, path, err)
	}
	return state, nil
}

// commitState writes state atomically: write to a temporary file, then
// rename it over the real path. No reader can ever observe a partial
// document, and a crash between the write and the rename leaves the
// previous state readable rather than a truncated one.
func commitState(path string, state domain.RunState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("runstate: failed to marshal state: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := retryTransientWindowsIO(func() error { return os.WriteFile(tmpPath, data, 0o644) }); err != nil {
		return fmt.Errorf("runstate: failed to write temp state document %s: %w", tmpPath, err)
	}
	if err := retryTransientWindowsIO(func() error { return os.Rename(tmpPath, path) }); err != nil {
		return fmt.Errorf("runstate: failed to commit state document %s: %w", path, err)
	}
	return nil
}

// Update performs the lock-guarded read-modify-write. mutate MUST NOT
// perform I/O: the guarded section is meant to be a read-modify-write of one
// small document, kept off any I/O other than that.
func (s *store) Update(mutate func(domain.RunState) (domain.RunState, error)) (UpdateResult, error) {
	reclaimed, priorHolder, err := s.acquireLock()
	if err != nil {
		return UpdateResult{}, err
	}
	defer s.releaseLock()

	current, err := readState(s.statePath())
	if err != nil {
		return UpdateResult{}, err
	}

	next, err := mutate(current)
	if err != nil {
		// mutate failed: commit nothing, and the deferred releaseLock still
		// runs, so a stuck lock never results from this path.
		return UpdateResult{}, err
	}

	if err := commitState(s.statePath(), next); err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{State: next, LockReclaimed: reclaimed, PriorHolder: priorHolder}, nil
}
