package interceptor_test

// Coverage: T12.7/AC12.9 — many interceptor *processes* against one
// workspace lose no counter increments, produce a log with no interleaved
// or truncated records, and each receive a coherent reply. Goroutines in one
// process do not exercise the cross-process file locking this component
// exists for (the specific defect the first-generation implementation had),
// so this test drives real OS processes: this test binary re-executes
// itself as a one-shot interceptor when GO_WANT_HELPER_PROCESS=1 is set,
// mirroring internal/launch's own helper-process pattern.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// systemClock is the real wall clock, used only by the helper subprocess:
// unlike every other test in this package, the helper is a genuinely
// separate process and cannot share a *fakeClock with its parent.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runHelperInterceptor()
		return
	}
	os.Exit(m.Run())
}

// runHelperInterceptor turns this test binary into a one-shot interceptor
// process: it reads a native pre-invocation payload from stdin, drives
// interceptor.Run against the real, file-backed collaborators rooted at the
// control/subject directories named by environment variables, writes the
// native reply to stdout, and exits with Run's own exit code.
func runHelperInterceptor() {
	controlDir := os.Getenv("ICPT_CONTROL_DIR")
	subjectDir := os.Getenv("ICPT_SUBJECT_DIR")
	testID := os.Getenv("ICPT_TEST_ID")
	runNumber, _ := strconv.Atoi(os.Getenv("ICPT_RUN_NUMBER"))

	store := runstate.NewStore(controlDir, systemClock{})
	log := invlog.NewLog(filepath.Join(controlDir, domain.InvocationLogName))
	resolver, err := fixtures.NewResolver(controlDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: NewResolver: %v\n", err)
		os.Exit(2)
	}
	applier := sideeffects.NewApplier(resolver)
	adapter := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{SupportsDirectSubstitution: true}})

	cfg := interceptor.Config{
		ControlDir: controlDir,
		SubjectDir: subjectDir,
		Phase:      domain.PhasePre,
		Adapter:    adapter,
		State:      store,
		Log:        log,
		Effects:    applier,
		Clock:      systemClock{},
		Registry:   domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
		TestID:     testID,
		RunNumber:  runNumber,
		In:         os.Stdin,
		Out:        os.Stdout,
		Diag:       os.Stderr,
	}

	os.Exit(interceptor.Run(context.Background(), cfg))
}

func TestRun_ConcurrentProcesses_AgainstOneWorkspace_LoseNoIncrementsAndProduceAWellFormedLog(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Skip("this test itself must not run inside the helper-process re-exec")
	}

	root := t.TempDir()
	subjectDir := filepath.Join(root, "subject")
	controlDir := filepath.Join(root, "control")
	if err := os.MkdirAll(subjectDir, 0o755); err != nil {
		t.Fatalf("mkdir subject dir: %v", err)
	}
	if err := os.MkdirAll(controlDir, 0o755); err != nil {
		t.Fatalf("mkdir control dir: %v", err)
	}

	const testID = "concurrency-example"
	store := runstate.NewStore(controlDir, systemClock{})
	if err := store.Initialize(domain.RunState{
		SchemaVersion:        1,
		TestID:               testID,
		RunNumber:            1,
		CollaboratorCounters: map[string]int{},
		PendingStubs:         map[string]domain.PendingStub{},
		InFlight:             map[string]domain.InFlight{},
	}); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}

	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			token := fmt.Sprintf("corr-%d", i)
			payload, err := json.Marshal(wireCall{
				Phase: "pre", Tool: id.ToolName, Agent: id.AgentIdentity,
				AgentInstanceID: fmt.Sprintf("researcher#%d", i), TaskDescription: "do work",
				Token: token,
			})
			if err != nil {
				errs <- fmt.Errorf("process %d: marshal: %w", i, err)
				return
			}

			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				"ICPT_CONTROL_DIR="+controlDir,
				"ICPT_SUBJECT_DIR="+subjectDir,
				"ICPT_TEST_ID="+testID,
				"ICPT_RUN_NUMBER=1",
			)
			cmd.Stdin = bytes.NewReader(payload)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				errs <- fmt.Errorf("process %d: exec failed: %v (stderr: %s)", i, err, stderr.String())
				return
			}

			var reply wireReply
			if err := json.Unmarshal(stdout.Bytes(), &reply); err != nil {
				errs <- fmt.Errorf("process %d: reply is not valid JSON: %v (stdout: %s)", i, err, stdout.String())
				return
			}
			if reply.Kind != "passthrough" {
				errs <- fmt.Errorf("process %d: reply Kind = %q, want %q", i, reply.Kind, "passthrough")
				return
			}
			if reply.Token != token {
				errs <- fmt.Errorf("process %d: reply Token = %q, want %q", i, reply.Token, token)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	final, err := store.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if final.SequenceCounter != n {
		t.Fatalf("SequenceCounter = %d, want %d — a lost increment means concurrent processes were not mutually excluded", final.SequenceCounter, n)
	}
	if final.CollaboratorCounters[id.Key()] != n {
		t.Fatalf("CollaboratorCounters[%q] = %d, want %d", id.Key(), final.CollaboratorCounters[id.Key()], n)
	}

	log := invlog.NewLog(filepath.Join(controlDir, domain.InvocationLogName))
	records, report, err := log.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if report.TrailingPartialRecord {
		t.Error("log has a trailing partial record — a concurrent append interleaved or truncated a record")
	}
	if len(report.MalformedLines) > 0 {
		t.Errorf("log has malformed lines: %v — a concurrent append interleaved or truncated a record", report.MalformedLines)
	}

	seqSeen := map[int]bool{}
	starts := 0
	for _, r := range records {
		if r.Kind != domain.RecordStart {
			continue
		}
		starts++
		if seqSeen[r.Seq] {
			t.Errorf("duplicate Seq %d among start records — a counter increment was lost", r.Seq)
		}
		seqSeen[r.Seq] = true
	}
	if starts != n {
		t.Fatalf("expected %d start records, got %d", n, starts)
	}
	for i := 1; i <= n; i++ {
		if !seqSeen[i] {
			t.Errorf("missing start record for Seq %d", i)
		}
	}
}
