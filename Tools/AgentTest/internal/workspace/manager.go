// Package workspace owns the lifecycle of per-run sandboxes: an isolated
// directory tree that a run may only execute in when freshly created, never
// in an existing project.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"mosaic-agent-test/internal/domain"
)

// Manager owns the lifecycle of per-run sandboxes.
type Manager interface {
	// Create makes a fresh sandbox for the given run key. It refuses to
	// adopt an existing non-empty directory: a run may only execute in a
	// freshly created sandbox, never in a real project. That refusal is
	// what makes teardown a deletion instead of a restore protocol.
	Create(key domain.RunKey) (domain.Sandbox, error)

	// Locate returns the sandbox for a run key without creating one.
	Locate(key domain.RunKey) (domain.Sandbox, error)

	// Teardown removes the sandbox tree.
	Teardown(s domain.Sandbox) error

	// Reap removes sandboxes orphaned by crashed prior runs, using the same
	// holder-liveness and age evidence the lock protocol uses. A live
	// concurrent run's sandbox is never touched.
	Reap(policy ReapPolicy) ([]ReapedSandbox, error)
}

// ReapPolicy configures Reap.
type ReapPolicy struct {
	OlderThan time.Duration // default StalenessThreshold
	DryRun    bool
}

// ReapedSandbox describes one sandbox Reap removed (or would remove, under
// DryRun).
type ReapedSandbox struct {
	Key    domain.RunKey
	Root   string
	Reason string // "holder-dead" | "stale"
}

var (
	ErrSandboxExists     = errors.New("workspace: sandbox already exists")
	ErrDirectoryNotEmpty = errors.New("workspace: refusing to run in a non-empty existing directory")
	ErrSandboxNotFound   = errors.New("workspace: sandbox not found")
)

// defaultStalenessThreshold mirrors runstate.StalenessThreshold. It cannot
// import that constant: runstate is a consumer of workspace, and importing
// it back here would create a cycle.
const defaultStalenessThreshold = 30 * time.Second

// subjectDirName and controlDirName are the two children of a sandbox root.
// ControlDir must be a sibling of SubjectDir, never inside it, so a subject
// can never discover the control files through its own working directory.
const (
	subjectDirName = "subject"
	controlDirName = "control"
)

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeComponent(s string) string {
	sanitized := unsafePathChars.ReplaceAllString(s, "_")
	if sanitized == "" {
		return "_"
	}
	return sanitized
}

// manager is the default Manager implementation.
type manager struct {
	mu      sync.Mutex
	rootDir string
	clock   domain.Clock
	created map[domain.RunKey]domain.Sandbox
}

// NewManager constructs a Manager rooted at rootDir, using clock for
// staleness evaluation during reaping.
func NewManager(rootDir string, clock domain.Clock) Manager {
	return &manager{
		rootDir: rootDir,
		clock:   clock,
		created: make(map[domain.RunKey]domain.Sandbox),
	}
}

func (m *manager) sandboxFor(key domain.RunKey) domain.Sandbox {
	name := fmt.Sprintf("%s-%s-%d", sanitizeComponent(key.RunID), sanitizeComponent(key.TestID), key.RunNumber)
	root := filepath.Join(m.rootDir, name)
	return domain.Sandbox{
		Key:        key,
		Root:       root,
		SubjectDir: filepath.Join(root, subjectDirName),
		ControlDir: filepath.Join(root, controlDirName),
	}
}

func (m *manager) Create(key domain.RunKey) (domain.Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.created[key]; ok {
		return domain.Sandbox{}, ErrSandboxExists
	}

	sb := m.sandboxFor(key)
	empty, err := dirEmptyOrAbsent(sb.Root)
	if err != nil {
		return domain.Sandbox{}, fmt.Errorf("workspace: failed to inspect %s: %w", sb.Root, err)
	}
	if !empty {
		return domain.Sandbox{}, ErrDirectoryNotEmpty
	}

	if err := os.MkdirAll(sb.SubjectDir, 0o755); err != nil {
		return domain.Sandbox{}, fmt.Errorf("workspace: failed to create subject dir %s: %w", sb.SubjectDir, err)
	}
	if err := os.MkdirAll(sb.ControlDir, 0o755); err != nil {
		return domain.Sandbox{}, fmt.Errorf("workspace: failed to create control dir %s: %w", sb.ControlDir, err)
	}

	m.created[key] = sb
	return sb, nil
}

func dirEmptyOrAbsent(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

func (m *manager) Locate(key domain.RunKey) (domain.Sandbox, error) {
	sb := m.sandboxFor(key)
	if _, err := os.Stat(sb.SubjectDir); err != nil {
		return domain.Sandbox{}, ErrSandboxNotFound
	}
	if _, err := os.Stat(sb.ControlDir); err != nil {
		return domain.Sandbox{}, ErrSandboxNotFound
	}
	return sb, nil
}

func (m *manager) Teardown(s domain.Sandbox) error {
	// os.RemoveAll is already a no-op (nil error) when the path does not
	// exist, which is what makes Teardown safely callable twice: it runs on
	// every exit path, including a panic recovery.
	if err := os.RemoveAll(s.Root); err != nil {
		return fmt.Errorf("workspace: failed to remove sandbox %s: %w", s.Root, err)
	}
	return nil
}

func (m *manager) Reap(policy ReapPolicy) ([]ReapedSandbox, error) {
	olderThan := policy.OlderThan
	if olderThan == 0 {
		olderThan = defaultStalenessThreshold
	}

	entries, err := os.ReadDir(m.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("workspace: failed to list %s: %w", m.rootDir, err)
	}

	now := m.clock.Now()
	var reaped []ReapedSandbox
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root := filepath.Join(m.rootDir, entry.Name())
		lockPath := filepath.Join(root, controlDirName, domain.LockFileName)

		data, err := os.ReadFile(lockPath)
		if err != nil {
			// No lock evidence for this sandbox: nothing to judge it by, so
			// leave it alone rather than guess.
			continue
		}
		var info domain.LockInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}

		var reason string
		switch {
		case !domain.ProcessAlive(info.PID):
			reason = "holder-dead"
		case now.Sub(info.AcquiredAt) > olderThan:
			reason = "stale"
		default:
			continue // live and fresh; leave it alone
		}

		if !policy.DryRun {
			if err := os.RemoveAll(root); err != nil {
				return reaped, fmt.Errorf("workspace: failed to reap %s: %w", root, err)
			}
		}
		reaped = append(reaped, ReapedSandbox{Root: root, Reason: reason})
	}
	return reaped, nil
}
