package domain

import "path/filepath"

// RunKey names one attempt of one test.
type RunKey struct {
	RunID     string // the run identity the driver authors before spawning
	TestName  string // the human-readable display name of the test
	RunNumber int    // 1-based repetition
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

	// DiagnosticLogName is the filename of the per-run diagnostic log written
	// by the file-backed sink. It lives in ControlDir so the run's retention
	// policy governs it uniformly alongside the other control files.
	DiagnosticLogName = "diag.log"

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

// LogsRoot returns the OrchestrationLogs directory, the parent of all
// per-run log folders including the unknown-run fallback bucket. It is
// the sibling-bucket detection anchor: the unknown-run fallback bucket
// sits at LogsRoot()/unknown-run, not inside the per-run folder.
func (s Sandbox) LogsRoot() string {
	return filepath.Join(s.SubjectDir, "OrchestrationLogs")
}

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

// DiagnosticLogPath returns the path of the per-run diagnostic log file.
// The file lives in ControlDir so the run's retention policy governs it
// uniformly alongside the other control files.
func (s Sandbox) DiagnosticLogPath() string {
	return filepath.Join(s.ControlDir, DiagnosticLogName)
}

// DeployLogDirName is the directory name within ControlDir that holds the
// deployment tool's per-run log files. It is a directory rather than a single
// file because the deployment tool writes two files inside it (latest.log and
// history.log) — so the override targets the directory, not any individual file.
const DeployLogDirName = "deploy-logs"

// DeployLogDir returns the directory this run's deployment tool invocation
// should write its logs to.
//
// It lives under ControlDir so the run's retention policy governs the deploy
// logs uniformly alongside all other per-run evidence (state, lock, invocation
// log, diagnostic log). Placing them here is intentional: these logs are
// per-run evidence, and coupling their lifetime to the sandbox prevents them
// from accumulating in the shared repository root across concurrent runs.
func (s Sandbox) DeployLogDir() string {
	return filepath.Join(s.ControlDir, DeployLogDirName)
}
