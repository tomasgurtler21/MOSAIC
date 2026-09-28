package lockprotocol

// Race and edge-case tests for the joiner path: concurrent callers, m4 robustness,
// and the four interleaving scenarios from the design (Race a/b/c and AC8.4).

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T8.1(f): joiner verifies own lock file still exists after acquisition
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_OwnLockFileDeletedAfterAcquisition_StartsFresh verifies
// the m4 robustness check: after acquiring its own run lock, the joiner
// verifies the lock file still exists on disk. If another process deleted
// the file between creation and locking, the joiner detects this and returns
// errStartFresh.
//
// This scenario cannot occur on Windows (locked files cannot be deleted), so
// the test is skipped on that platform.
func TestSetupAsJoiner_OwnLockFileDeletedAfterAcquisition_StartsFresh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot unlink a locked file on Windows; this scenario does not occur on this platform")
	}

	// Arrange: full backup state.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	joinerLockPath := backup.RunLockPath(backupDir, "run-joiner")

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.afterJoinerLockAcquired = func() {
		// Delete the joiner's lock file while the lock is held (Unix only).
		// The OS retains the inode (lock is still held via the open FD), but
		// os.Stat on the path will return not-found.
		os.Remove(joinerLockPath) //nolint:errcheck
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: joiner detected its lock file was deleted and returned errStartFresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh when joiner's lock file was deleted after acquisition; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(d): manifest-wait: .restoring becomes held -> enters restoring-poll
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_ManifestWait_RestoringBecomesHeld_EntersRestoringPoll verifies
// the teardown-interleaving behavior: when .restoring becomes held during the
// manifest-wait loop (indicating last-out teardown), the joiner enters the
// .restoring re-poll loop. Once .restoring is released and the backup dir is gone,
// the joiner returns errStartFresh.
func TestSetupAsJoiner_ManifestWait_RestoringBecomesHeld_EntersRestoringPoll(t *testing.T) {
	// Arrange: backup dir exists, no manifest (joiner is in manifest-wait).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})
	var once sync.Once
	joiner.onManifestPollTick = func() {
		once.Do(func() {
			go func() {
				h, lockErr := filelock.Lock(restoringPath)
				if lockErr != nil {
					return
				}
				close(restoringHeld)
				time.Sleep(60 * time.Millisecond)
				os.RemoveAll(backupDir) // teardown: backup dir gone while .restoring held
				h.Unlock()              //nolint:errcheck
			}()
			<-restoringHeld
		})
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: .restoring re-poll detected backup dir gone -> start fresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh after .restoring held during manifest wait and backup dir disappeared; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(a): Race (b) -- joiner acquires lock, then last-out holds .restoring
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RaceB_RestoringHeldAfterLockAcquisition_StartsFresh
// verifies Race (b) interleaving: the joiner acquires its own run lock, then
// a concurrent last-out run acquires .restoring and begins teardown. The
// joiner's post-lock re-verification detects .restoring as held, releases its
// own lock, enters the .restoring re-poll loop, and returns errStartFresh when
// the backup directory disappears.
func TestSetupAsJoiner_RaceB_RestoringHeldAfterLockAcquisition_StartsFresh(t *testing.T) {
	// Arrange: full backup state.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})

	joiner.afterJoinerLockAcquired = func() {
		// Last-out run acquires .restoring and tears down while joiner is about
		// to perform post-lock re-verification.
		go func() {
			h, lockErr := filelock.Lock(restoringPath)
			if lockErr != nil {
				return
			}
			close(restoringHeld)
			time.Sleep(60 * time.Millisecond)
			os.RemoveAll(backupDir)
			h.Unlock() //nolint:errcheck
		}()
		<-restoringHeld
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert
	if !errors.Is(err, errStartFresh) {
		t.Errorf("Race (b): expected errStartFresh after last-out teardown during post-lock re-verify; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(b): Race (a) partial backup -- no manifest, joiner times out
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RaceA_PartialBackupNoManifest_TimesOut verifies that when
// a creator crashes after creating the backup directory but before writing the
// manifest, a concurrent joiner's manifest-wait poll eventually times out with
// errPollTimeout. (Recovery of the partial backup is handled by RecoveryCheck
// at startup, tested in Stage 9. This test verifies the joiner side.)
func TestSetupAsJoiner_RaceA_PartialBackupNoManifest_TimesOut(t *testing.T) {
	// Arrange: backup dir exists (creator mkdir'd) but no manifest (creator crashed).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollInterval = 5 * time.Millisecond
	joiner.pollTimeout = 20 * time.Millisecond

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: manifest never appeared; joiner timed out rather than blocking forever.
	if !errors.Is(err, errPollTimeout) {
		t.Errorf("Race (a) partial backup: expected errPollTimeout; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(c): Race (c) -- joiner waits for creator to finish transforms
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RaceC_WaitsForCreatorToFinishTransforms verifies Race (c)
// interleaving: a joiner that arrives while the creator has written the manifest
// but not yet .setup-complete (transforms still in progress) blocks until the
// creator writes .setup-complete, then proceeds.
func TestSetupAsJoiner_RaceC_WaitsForCreatorToFinishTransforms(t *testing.T) {
	// Arrange: backup dir with manifest, transforms complete, no .setup-complete.
	base := t.TempDir()
	agentsDir, backupDir := newTestBackupStateWithManifestOnly(t, base)
	rules := transform.TransformationsFor("opencode")
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: backup.TransformInPlace: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 500 * time.Millisecond

	// Simulate creator writing .setup-complete after finishing transforms.
	go func() {
		time.Sleep(30 * time.Millisecond)
		if writeErr := backup.WriteSetupComplete(backupDir); writeErr != nil {
			t.Logf("background backup.WriteSetupComplete: %v", writeErr)
		}
	}()

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert
	if err != nil {
		t.Errorf("Race (c): expected nil after waiting for creator to write .setup-complete; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(d): Race (a) live concurrent start -- exactly one caller wins os.Mkdir
// ---------------------------------------------------------------------------

// TestNewBackupDir_ConcurrentCallers_ExactlyOneWinsCreation verifies that
// when multiple runs call NewBackupDir concurrently, exactly one receives
// alreadyExisted=false (the winner that created the directory) and all others
// receive alreadyExisted=true (joiners). This is the concurrency invariant
// that drives the creator/joiner dispatch.
func TestNewBackupDir_ConcurrentCallers_ExactlyOneWinsCreation(t *testing.T) {
	const numCallers = 6

	// Arrange: fresh agents dir, no backup dir yet.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)

	type result struct {
		alreadyExisted bool
		err            error
	}
	results := make([]result, numCallers)
	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(numCallers)
	for i := range numCallers {
		go func(idx int) {
			defer wg.Done()
			<-start
			_, existed, err := backup.NewBackupDir(agentsDir)
			results[idx] = result{alreadyExisted: existed, err: err}
		}(i)
	}

	// Act: release all goroutines simultaneously.
	close(start)
	wg.Wait()

	// Assert: all calls succeeded.
	for i, r := range results {
		if r.err != nil {
			t.Errorf("caller %d: backup.NewBackupDir error: %v", i, r.err)
		}
	}

	// Assert: exactly one got alreadyExisted=false (the winner).
	var winners int
	for _, r := range results {
		if !r.alreadyExisted {
			winners++
		}
	}
	if winners != 1 {
		t.Errorf("expected exactly 1 caller to win os.Mkdir (alreadyExisted=false), got %d", winners)
	}
}

// ---------------------------------------------------------------------------
// AC8.4 gap: setup-complete-wait + .restoring-held -> joiner enters restoring-poll
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_SetupCompleteWait_RestoringBecomesHeld_StartsFresh verifies
// the teardown-interleaving behavior during setup-complete-wait: when .restoring
// becomes held during the setup-complete-wait loop (indicating a last-out
// teardown is in progress), the joiner exits the setup-complete-wait loop,
// enters the .restoring re-poll loop, and returns errStartFresh when the backup
// directory disappears.
//
// This test covers the fourth combination required by AC8.4:
// setup-complete-wait + .restoring-held. The other three combinations are
// covered by existing tests.
func TestSetupAsJoiner_SetupCompleteWait_RestoringBecomesHeld_StartsFresh(t *testing.T) {
	// Arrange: backup dir with manifest and transformed agents, no .setup-complete.
	// Joiner is in setup-complete-wait loop.
	base := t.TempDir()
	agentsDir, backupDir := newTestBackupStateWithManifestOnly(t, base)
	rules := transform.TransformationsFor("opencode")
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: backup.TransformInPlace: %v", err)
	}
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})
	var once sync.Once
	joiner.onSetupCompletePollTick = func() {
		once.Do(func() {
			go func() {
				h, lockErr := filelock.Lock(restoringPath)
				if lockErr != nil {
					return
				}
				close(restoringHeld)
				time.Sleep(60 * time.Millisecond)
				os.RemoveAll(backupDir) // teardown: backup dir gone while .restoring held
				h.Unlock()              //nolint:errcheck
			}()
			<-restoringHeld
		})
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: .restoring re-poll detected backup dir gone -> start fresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh after .restoring held during setup-complete wait"+
			" and backup dir disappeared; got %v", err)
	}
}
