package interceptor_test

// Shared fixtures for this package's tests: a fake clock (mirroring
// internal/runstate's own test helper), the fake adapter's private wire
// shape re-declared here (internal/harness/fake speaks its native language
// only to its own tests and to whatever composes it — a shell test drives
// the adapter the same way a real harness would, by exchanging native
// bytes, so it owns its own copy of that shape rather than reaching into the
// adapter package's internals), and the harness assembly every test in this
// package builds its Config from.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/fake"
	"mosaic-agent-test/internal/interceptor"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
)

// --- fake wall clock, mirroring internal/runstate/fakeclock_test.go -------

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// startTicking fast-forwards the clock in the background so a real-time
// bounded test can exercise the 5s acquisition timeout without actually
// waiting that long. Call the returned func to stop it.
func (c *fakeClock) startTicking(step time.Duration) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.Advance(step)
			}
		}
	}()
	return func() { close(stop) }
}

// --- the fake adapter's own private wire shape, re-declared for tests -----

type wireCall struct {
	Phase           string `json:"phase"`
	Tool            string `json:"tool"`
	Agent           string `json:"agent"`
	AgentInstanceID string `json:"agent_instance_id"`
	TaskDescription string `json:"task_description"`
	Token           string `json:"token"`
}

type wireReply struct {
	Kind   string          `json:"kind"`
	Body   json.RawMessage `json:"body,omitempty"`
	Prompt string          `json:"prompt,omitempty"`
	Token  string          `json:"token"`
}

func encodePre(t *testing.T, id domain.CollaboratorIdentity, token, instanceID, taskDesc string) []byte {
	t.Helper()
	b, err := json.Marshal(wireCall{
		Phase: "pre", Tool: id.ToolName, Agent: id.AgentIdentity,
		AgentInstanceID: instanceID, TaskDescription: taskDesc, Token: token,
	})
	if err != nil {
		t.Fatalf("encodePre: marshal failed: %v", err)
	}
	return b
}

func encodePost(t *testing.T, id domain.CollaboratorIdentity, token string) []byte {
	t.Helper()
	b, err := json.Marshal(wireCall{Phase: "post", Tool: id.ToolName, Agent: id.AgentIdentity, Token: token})
	if err != nil {
		t.Fatalf("encodePost: marshal failed: %v", err)
	}
	return b
}

func decodeReply(t *testing.T, native []byte) wireReply {
	t.Helper()
	var r wireReply
	if err := json.Unmarshal(native, &r); err != nil {
		t.Fatalf("decodeReply: reply is not valid JSON: %v (native: %s)", err, native)
	}
	return r
}

// --- sandbox + Config assembly ---------------------------------------------

func newSandboxDirs(t *testing.T) (subjectDir, controlDir string) {
	t.Helper()
	root := t.TempDir()
	subjectDir = filepath.Join(root, "subject")
	controlDir = filepath.Join(root, "control")
	if err := os.MkdirAll(subjectDir, 0o755); err != nil {
		t.Fatalf("mkdir subject dir: %v", err)
	}
	if err := os.MkdirAll(controlDir, 0o755); err != nil {
		t.Fatalf("mkdir control dir: %v", err)
	}
	return subjectDir, controlDir
}

// baseState is a freshly initialized run state with every map field
// populated, matching what a real run's setup step would write.
func baseState() domain.RunState {
	return domain.RunState{
		SchemaVersion:        1,
		TestID:               "stage12-example",
		RunNumber:            1,
		CollaboratorCounters: map[string]int{},
		PendingStubs:         map[string]domain.PendingStub{},
		InFlight:             map[string]domain.InFlight{},
	}
}

// testHarness is everything newHarness assembled for one test, so a test
// can reach past the Config into the store/log/adapter directly to assert
// on committed state, appended records and adapter-recorded invocations.
type testHarness struct {
	Config     interceptor.Config
	Store      runstate.Store
	Log        invlog.Log
	Adapter    *fake.Adapter
	SubjectDir string
	ControlDir string
	Clock      *fakeClock
}

// newHarness assembles a Config wired to real, file-backed collaborators
// (runstate.Store, invlog.Log, sideeffects.Applier) and the scripted
// internal/harness/fake adapter, so a test drives the shell end to end
// without a real harness or a real LLM.
func newHarness(t *testing.T, state domain.RunState, registry domain.StubRegistry, caps domain.HarnessCapabilities, script map[string][]fake.Turn) testHarness {
	t.Helper()

	subjectDir, controlDir := newSandboxDirs(t)
	clock := newFakeClock()

	store := runstate.NewStore(controlDir, clock)
	if err := store.Initialize(state); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	log := invlog.NewLog(filepath.Join(controlDir, domain.InvocationLogName))

	resolver, err := fixtures.NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned unexpected error: %v", err)
	}
	applier := sideeffects.NewApplier(resolver)

	adapter := fake.New(fake.Options{Capabilities: caps, Script: script})

	cfg := interceptor.Config{
		ControlDir: controlDir,
		SubjectDir: subjectDir,
		Adapter:    adapter,
		State:      store,
		Log:        log,
		Effects:    applier,
		Clock:      clock,
		Registry:   registry,
		TestID:     state.TestID,
		RunNumber:  state.RunNumber,
	}

	return testHarness{
		Config:     cfg,
		Store:      store,
		Log:        log,
		Adapter:    adapter,
		SubjectDir: subjectDir,
		ControlDir: controlDir,
		Clock:      clock,
	}
}

// run drives interceptor.Run with native fed through cfg.In and captures
// what it wrote to Out and Diag, so a test asserts on plain byte slices
// rather than juggling io.Reader/io.Writer plumbing itself.
func run(t *testing.T, cfg interceptor.Config, phase domain.InterceptionPhase, native []byte) (reply []byte, exitCode int, diag []byte) {
	t.Helper()
	var out, diagBuf bytes.Buffer
	cfg.Phase = phase
	cfg.In = bytes.NewReader(native)
	cfg.Out = &out
	cfg.Diag = &diagBuf
	exitCode = interceptor.Run(context.Background(), cfg)
	return out.Bytes(), exitCode, diagBuf.Bytes()
}

func readState(t *testing.T, store runstate.Store) domain.RunState {
	t.Helper()
	s, err := store.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	return s
}

// readLog reads the invocation log and fails the test if reading it
// tolerated any anomaly a healthy run must never produce (a trailing
// partial record or a malformed interior line), since this package's own
// appends are the only writer in these tests.
func readLog(t *testing.T, log invlog.Log) []domain.LogRecord {
	t.Helper()
	records, report, err := log.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if report.TrailingPartialRecord {
		t.Errorf("log has a trailing partial record")
	}
	if len(report.MalformedLines) > 0 {
		t.Errorf("log has malformed lines: %v", report.MalformedLines)
	}
	return records
}

func writeLockFile(t *testing.T, dir string, info domain.LockInfo) {
	t.Helper()
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal lock info: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, domain.LockFileName), b, 0o644); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}
}
