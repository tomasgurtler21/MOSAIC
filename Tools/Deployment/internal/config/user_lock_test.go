package config_test

// Tests for UserConfigStore.WithLock.
//
// Verified invariants:
//
// WithLock — fn is called when the lock is acquired:
//   - WithLock calls fn and returns its result when the lock is available.
//   - The lock file is created at <path>.lock alongside user-config.yaml.
//
// WithLock — timeout on contention:
//   - When another goroutine holds the lock, WithLock returns a non-nil error
//     after the timeout without calling fn.
//   - The error message mentions the lock file path.
//   - The timeout does not cause a panic or indefinite hang.
//
// WithLock — error from fn propagates:
//   - When fn returns an error, WithLock returns that error.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"mosaic-deploy/internal/config"
)

// TestUserConfigStore_WithLock_CallsFnAndReturnsNil verifies that WithLock calls fn
// and returns nil when the lock is available and fn returns nil.
func TestUserConfigStore_WithLock_CallsFnAndReturnsNil(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	called := false
	err := store.WithLock(func() error {
		called = true
		return nil
	})

	if err != nil {
		t.Errorf("WithLock returned error: %v; want nil when lock is available and fn returns nil", err)
	}
	if !called {
		t.Error("WithLock did not call fn; fn must be called when the lock is acquired")
	}
}

// TestUserConfigStore_WithLock_CreatesSidecarLockFile verifies that WithLock creates the
// sidecar lock file at <path>.lock alongside user-config.yaml.
func TestUserConfigStore_WithLock_CreatesSidecarLockFile(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	if err := store.WithLock(func() error { return nil }); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	lockPath := store.Path() + ".lock"
	if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
		t.Errorf("lock file does not exist at %s after WithLock; sidecar lock file must be created", lockPath)
	}
}

// TestUserConfigStore_WithLock_PropagatesFnError verifies that when fn returns an error,
// WithLock returns that error to the caller.
func TestUserConfigStore_WithLock_PropagatesFnError(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	sentinel := errors.New("fn error sentinel")
	err := store.WithLock(func() error { return sentinel })

	if !errors.Is(err, sentinel) {
		t.Errorf("WithLock returned %v; want error wrapping %v from fn", err, sentinel)
	}
}

// TestUserConfigStore_WithLock_TimeoutWhenLockHeld verifies that WithLock returns a
// non-nil error within a reasonable time when another process holds the lock, rather
// than panicking or blocking indefinitely.
//
// The test acquires the sidecar lock file directly using gofrs/flock to simulate another
// process, then calls store.WithLock, which must time out and return an error.
//
// This test takes approximately 3 seconds (the configured lock-timeout duration) by design;
// t.Parallel() is used so it does not serialize the entire config package test run.
func TestUserConfigStore_WithLock_TimeoutWhenLockHeld(t *testing.T) {
	t.Parallel()
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	lockPath := store.Path() + ".lock"

	// Ensure the lock file directory exists (WithLock normally does this, but we need
	// to create the file ourselves here to simulate an external holder).
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create lock dir: %v", err)
	}
	// Create the lock file so flock.New below can open it.
	f, createErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if createErr != nil {
		t.Fatalf("create lock file: %v", createErr)
	}
	f.Close()

	// Hold the lock in a background goroutine for longer than WithLock's timeout.
	holder := flock.New(lockPath)
	if ok, lockErr := holder.TryLock(); lockErr != nil || !ok {
		t.Fatalf("TryLock (holder): ok=%v err=%v; cannot acquire lock to simulate contention", ok, lockErr)
	}

	// release signals the goroutine to unlock and exit.
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-release
		_ = holder.Unlock()
	}()

	// WithLock must return an error after its timeout (~3s), not block indefinitely.
	start := time.Now()
	fnCalled := false
	err := store.WithLock(func() error {
		fnCalled = true
		return nil
	})
	elapsed := time.Since(start)

	// Release the holder so the goroutine can exit.
	close(release)
	wg.Wait()

	if err == nil {
		t.Error("WithLock returned nil when another process held the lock; want a non-nil timeout error")
	}
	if fnCalled {
		t.Error("WithLock called fn despite being unable to acquire the lock")
	}
	// The error message must mention the lock file path so notifyPersistFailure can display it.
	if err != nil && !strings.Contains(err.Error(), lockPath) {
		t.Errorf("WithLock error message = %q; want it to contain the lock file path %q", err.Error(), lockPath)
	}
	// Sanity: the timeout should not have taken much longer than the configured 3s timeout.
	if elapsed > 8*time.Second {
		t.Errorf("WithLock took %s; expected to return within ~3-4s of the configured timeout", elapsed)
	}
}

// TestUserConfigStore_WithLock_LoadSaveUnderLock verifies the common use-case:
// loading and saving the user config inside a WithLock call succeeds end-to-end.
func TestUserConfigStore_WithLock_LoadSaveUnderLock(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	err := store.WithLock(func() error {
		cfg, loadErr := store.Load()
		if loadErr != nil {
			return loadErr
		}
		return store.Save(cfg)
	})

	if err != nil {
		t.Errorf("WithLock Load+Save: %v; want nil", err)
	}
}
