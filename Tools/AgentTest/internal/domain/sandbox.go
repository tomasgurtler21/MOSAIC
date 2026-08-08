package domain

import "path/filepath"

// RunKey names one attempt of one test.
type RunKey struct {
	RunID     string // the run identity the driver authors before spawning
	TestID    string
	RunNumber int // 1-based repetition
}

// Sandbox is the isolated tree for one run attempt.
//
// The layout separates the directory the subject runs in from the directory
// the tool keeps its control files in. This is deliberate and load-bearing:
// the subject must not be able to tell it is being exercised, and a control
// file sitting in its working directory is discoverable by any tool it has.
// The interceptor receives ControlDir explicitly as an argument and never
// infers it from a process working directory, because a harness may launch a
// hook command from anywhere.
type Sandbox struct {
	Key RunKey

	// Root is the sandbox tree. Teardown deletes it whole.
	Root string

	// SubjectDir is the subject's working directory. It contains only things
	// a real project would contain: the harness configuration, agent
	// definitions, seeded artifacts, and the logs the MOSAIC logger writes.
	SubjectDir string

	// ControlDir is a sibling of SubjectDir, never inside it. It holds the
	// run state document, its lock, the invocation log, the side-effect
	// ledger and the early-exit sentinel.
	ControlDir string
}

// Fixed names within ControlDir.
const (
	StateFileName         = "state.json"
	LockFileName          = "state.lock"
	InvocationLogName     = "invocations.jsonl"
	LedgerFileName        = "effects.json"
	EarlyExitSentinelName = "early-exit"

	// The active stub registry, resolved and token-expanded by setup, read by
	// every interceptor process.
	ActiveRegistryFileName = "registry.json"
	// The test's declared parallel groups, so an interceptor can tag a record
	// with its group without loading the whole test definition.
	ParallelGroupsFileName = "groups.json"
	// The setup ledger, persisted so a later process can tear down a sandbox
	// whose driver died.
	SetupLedgerFileName = "setup.json"
)

// LogRoot returns the directory the MOSAIC logger bundle writes into,
// beneath the subject directory, from which cost is read.
func (s Sandbox) LogRoot() string {
	return filepath.Join(s.SubjectDir, "OrchestrationLogs", s.Key.RunID)
}

// StatePath returns the run state document's path.
func (s Sandbox) StatePath() string {
	return filepath.Join(s.ControlDir, StateFileName)
}

// LockPath returns the run state lock's path.
func (s Sandbox) LockPath() string {
	return filepath.Join(s.ControlDir, LockFileName)
}

// InvocationLogPath returns the invocation log's path.
func (s Sandbox) InvocationLogPath() string {
	return filepath.Join(s.ControlDir, InvocationLogName)
}

// LedgerPath returns the side-effect ledger's path.
func (s Sandbox) LedgerPath() string {
	return filepath.Join(s.ControlDir, LedgerFileName)
}

// EarlyExitSentinelPath returns the early-exit sentinel's path.
func (s Sandbox) EarlyExitSentinelPath() string {
	return filepath.Join(s.ControlDir, EarlyExitSentinelName)
}

// ActiveRegistryPath returns the active stub registry document's path.
func (s Sandbox) ActiveRegistryPath() string {
	return filepath.Join(s.ControlDir, ActiveRegistryFileName)
}

// ParallelGroupsPath returns the declared parallel groups document's path.
func (s Sandbox) ParallelGroupsPath() string {
	return filepath.Join(s.ControlDir, ParallelGroupsFileName)
}

// SetupLedgerPath returns the setup ledger's path.
func (s Sandbox) SetupLedgerPath() string {
	return filepath.Join(s.ControlDir, SetupLedgerFileName)
}
