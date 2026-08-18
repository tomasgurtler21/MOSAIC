package runstate

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"mosaic-agent-test/internal/domain"
)

const (
	initialBackoff = 5 * time.Millisecond
	maxBackoff     = 100 * time.Millisecond
)

var reclaimCounter atomic.Int64

// newNonce returns a value unique enough to name a transient reclaim-claim
// file; it distinguishes concurrent reclaim attempts from one another, not
// from an adversary.
func newNonce() string {
	return time.Now().UTC().Format("20060102T150405.000000000") + "-" +
		strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(reclaimCounter.Add(1), 10)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// acquireLock acquires the run-state lock, polling with backoff up to
// AcquireTimeout. If the current holder is verifiably dead or the lock has
// aged past StalenessThreshold, it is reclaimed via an atomic
// rename-with-ownership-check: the same primitive the state commit uses, so
// simultaneous reclaimers cannot both win.
func (s *store) acquireLock() (reclaimed bool, priorHolder domain.LockInfo, err error) {
	deadline := s.clock.Now().Add(AcquireTimeout)
	backoff := initialBackoff

	for {
		own := domain.LockInfo{
			PID:        os.Getpid(),
			Host:       hostname(),
			AcquiredAt: s.clock.Now(),
			ControlDir: s.controlDir,
			Nonce:      newNonce(),
		}

		if createErr := createLockFileExclusive(s.lockPath(), own); createErr == nil {
			return false, domain.LockInfo{}, nil
		} else if !os.IsExist(createErr) {
			return false, domain.LockInfo{}, createErr
		}

		existing, readErr := readLockFile(s.lockPath())
		if readErr == nil && isReclaimable(existing, s.clock.Now()) {
			won, reclaimErr := s.tryReclaim(own)
			if reclaimErr == nil && won {
				return true, existing, nil
			}
		}
		// A read error (e.g. the holder released the lock between our
		// failed create and this read) or a lost reclaim race are not
		// fatal — just retry from the top.

		if !s.clock.Now().Before(deadline) {
			return false, domain.LockInfo{}, ErrLockTimeout
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

func (s *store) releaseLock() {
	_ = retryTransientWindowsIO(func() error { return os.Remove(s.lockPath()) })
}

// tryReclaim wins exactly-one-of-many by racing to rename the existing lock
// file away: rename of a given source path succeeds for at most one caller,
// since the source no longer exists for anyone after the first winner. The
// winner then attempts to recreate the lock file with its own identity;
// losing that second race (to some other, unrelated fresh acquisition)
// simply means going back to contending normally.
func (s *store) tryReclaim(own domain.LockInfo) (won bool, err error) {
	claimPath := s.lockPath() + ".reclaim." + own.Nonce
	// Deliberately not retried: this rename is the exactly-one-winner check
	// itself. Blindly retrying it after a transient failure would risk
	// renaming away a lock file that changed identity in the meantime (e.g.
	// a legitimate new holder that acquired after our read), which would
	// silently break mutual exclusion. Any failure here — "someone already
	// renamed it away", "the holder refreshed it", or a transient
	// Windows contention error — is treated uniformly as "not our win this
	// iteration"; the caller's loop re-reads and re-evaluates from scratch.
	if renameErr := os.Rename(s.lockPath(), claimPath); renameErr != nil {
		return false, nil
	}
	_ = os.Remove(claimPath) // best-effort cleanup of the renamed-away artifact

	if createErr := createLockFileExclusive(s.lockPath(), own); createErr != nil {
		// We won the removal race but lost the immediate re-acquire race.
		return false, nil
	}
	return true, nil
}

const (
	transientIORetries = 500
	transientIODelay   = 1 * time.Millisecond
)

// retryTransientWindowsIO retries op a bounded number of times when it fails
// with one of Windows' transient file-contention errors:
//
//   - ERROR_SHARING_VIOLATION: Go's os package opens files on Windows
//     without FILE_SHARE_DELETE, so a concurrent, briefly-open os.ReadFile of
//     the same file (e.g. another contender polling lock liveness) can
//     transiently block a delete, rename, or open-for-write of it.
//   - ERROR_ACCESS_DENIED: Windows can report this, instead of "not found",
//     for a moment while it finishes tearing down a just-deleted or
//     just-renamed-away file before a new create at the same path is let
//     through.
//
// Both windows are a handful of microseconds under heavy concurrency; a
// short bounded retry clears them without changing any protocol semantics.
// On non-Windows platforms neither condition exists, so op runs exactly
// once.
func retryTransientWindowsIO(op func() error) error {
	if runtime.GOOS != "windows" {
		return op()
	}
	var err error
	for i := 0; i < transientIORetries; i++ {
		err = op()
		if err == nil || !isTransientWindowsIOError(err) {
			return err
		}
		time.Sleep(transientIODelay)
	}
	return err
}

// ERROR_SHARING_VIOLATION and ERROR_ACCESS_DENIED, per the Windows System
// Error Codes reference.
const (
	errnoSharingViolation = 32
	errnoAccessDenied     = 5
)

func isTransientWindowsIOError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == errnoSharingViolation || errno == errnoAccessDenied
	}
	return false
}

func createLockFileExclusive(path string, info domain.LockInfo) error {
	var f *os.File
	openErr := retryTransientWindowsIO(func() error {
		var err error
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		return err
	})
	if openErr != nil {
		return openErr
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(info)
}

func readLockFile(path string) (domain.LockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.LockInfo{}, err
	}
	var info domain.LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return domain.LockInfo{}, err
	}
	return info, nil
}

// isReclaimable reports whether a lock may be taken from its current
// holder: the holder is verifiably dead, or the lock has aged past
// StalenessThreshold. Staleness is a standalone reclamation condition, not
// merely a proxy for liveness.
func isReclaimable(info domain.LockInfo, now time.Time) bool {
	return !domain.ProcessAlive(info.PID) || now.Sub(info.AcquiredAt) >= StalenessThreshold
}
