package integration_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// update is set by -update to regenerate golden files rather than compare against them.
// Usage: go test -run TestIntegration_... -update
var update = flag.Bool("update", false, "regenerate golden files")

// ---- fixed-time Clock ----

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ---- always-approved ApprovalReader ----

// alwaysApprovedReader approves every artifact unconditionally. Integration
// tests exercise routing and harness dispatch, not HITL gate enforcement; this
// reader prevents the HITL check from redispatching agents whose output files
// do not exist on disk (which is the normal state in integration tests).
type alwaysApprovedReader struct{}

func (alwaysApprovedReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return domain.ApprovalTrue
}

// ---- no-op Interaction ----

type noopInteraction struct{}

func (n *noopInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (n *noopInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---- test helpers ----

// sessionTestdataDir is the path to the session package's testdata directory,
// which contains reusable orchestrator fixture files.
const sessionTestdataDir = "../../testdata/session"

// goldenDir is the path to the integration golden file directory.
const goldenDir = "../../testdata/integration/golden"

// copyFile copies src to dst in the given temp directory with the given name.
func copyFile(t *testing.T, dir, dstName, srcPath string) string {
	t.Helper()
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("copyFile: read %q: %v", srcPath, err)
	}
	dst := filepath.Join(dir, dstName)
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copyFile: write %q: %v", dst, err)
	}
	return dst
}

// writeAgentFile creates a minimal agent definition file for the given ID.
func writeAgentFile(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, agentID+".md")
	if err := os.WriteFile(path, []byte("# Agent: "+agentID+"\n"), 0600); err != nil {
		t.Fatalf("writeAgentFile(%q): %v", agentID, err)
	}
}

// writeOrchFile writes an orchestrator file with an inline workflow section.
func writeOrchFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeOrchFile: %v", err)
	}
	return path
}

// newSession builds a session with a file-based artifact store and scripted components.
// Approvals is wired to alwaysApprovedReader so that HITL checks on workflows
// with HITL=true rows do not redispatch agents for non-existent approval files.
func newSession(
	f *harness.MockAdapter,
	artifactPath string,
) session.Session {
	store := artifact.NewFileStore(artifactPath)
	return session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Approvals: alwaysApprovedReader{},
	})
}

// requireRunStatus asserts that the RunOutcome has the expected status.
func requireRunStatus(t *testing.T, got domain.RunOutcome, err error, want domain.RunStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != want {
		t.Errorf("want Status=%q, got %q (message: %q)", want, got.Status, got.Message)
	}
}

// requireRefused asserts that the outcome is RunRefused with a nil error.
func requireRefused(t *testing.T, got domain.RunOutcome, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error for refusal, got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused, got %q (message: %q)", got.Status, got.Message)
	}
	return got.Message
}

// requireStartFailed asserts that the outcome is RunStartFailed with a nil
// error and a non-empty message.
func requireStartFailed(t *testing.T, got domain.RunOutcome, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error for a start failure, got %v", err)
	}
	if got.Status != domain.RunStartFailed {
		t.Errorf("want RunStartFailed, got %q (message: %q)", got.Status, got.Message)
	}
	if got.Message == "" {
		t.Error("want a non-empty outcome message for a start failure")
	}
	return got.Message
}

// ---- small utilities ----

// containsArtifact reports whether the slice contains the given artifact path,
// either as written or under the run-scoped folder prefix of the run.
func containsArtifact(arts []string, target string) bool {
	for _, a := range arts {
		if a == target || a == domain.RunScopedFolder(integrationRunID)+"/"+target {
			return true
		}
	}
	return false
}

// ===== Stage 9: End-to-End Mode Coverage =====
//
// These tests verify assembled-run behaviours across execution modes, HITL
// verification, consultant-driven routing, and run lifecycle management.
// They exercise the full stack with the real file-based ArtifactStore so that
// the orchestration artifact on disk can be inspected and resumed.
//
// Coverage:
//
//   Orchestrated mode (T9.1):
//   - A run in orchestrated mode routes every step through the routing
//     consultant; the engine never produces an auto-route.
//
//   Auto and auto-review all-SUCCESS / deviation (T9.2):
//   - An all-SUCCESS run in auto and auto-review needs zero consultations.
//   - A non-SUCCESS response triggers exactly one consultation per deviation.
//
//   COMPLETED_NEEDS_ACTION auto-route-back (T9.3):
//   - In auto-review, a CNA with an unambiguous OnFindings target auto-routes
//     back without invoking the routing consultant, and the review agent's
//     output artifacts are injected into the target's inputs.
//   - In auto mode the same CNA triggers a routing consultation instead.
//
//   HITL verification (T9.4):
//   - A HITL-dispatched step whose output artifact records human_approved:false
//     triggers exactly one same-agent redispatch; a second non-compliant result
//     becomes a deviation, and the run never advances past the unverified step.
//
//   Stop and orchestrator failure (T9.5):
//   - A consultant stop leaves a resumable artifact on disk.
//   - A consultant failure (transport error) leaves a resumable artifact.
//
//   Commit setup (T9.6):
//   - A commits-enabled run records the branch name in the artifact frontmatter
//     and adds the commit setup dispatch as the first execution log row.
//   - A failed commit setup stops the run as a resumable start failure that
//     keeps the artifact and the setup row; a resume retries setup.
//
//   Resume (T9.7):
//   - A resumed run reads its mode and all other settings from the artifact
//     frontmatter; the caller-supplied RunConfig values are overwritten.
//   - An interrupted step is re-dispatched; nothing is re-asked.

// ---- Stage 9 test doubles ----

// intScriptedRoutingConsultant is a scripted routing consultant for integration
// tests. It returns RoutingInstructions from its queue in FIFO order and
// records every ConsultRouting call so tests can assert call counts.
type intScriptedRoutingConsultant struct {
	instructions []domain.RoutingInstruction
	errors       []error
	idx          int
	CallCount    int
	Requests     []domain.ConsultationRequest
}

func (s *intScriptedRoutingConsultant) ConsultRouting(_ context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	s.CallCount++
	s.Requests = append(s.Requests, req)
	if s.idx >= len(s.instructions) {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("intScriptedRoutingConsultant: queue exhausted (call #%d)", s.CallCount),
		}
	}
	instr := s.instructions[s.idx]
	var err error
	if s.idx < len(s.errors) {
		err = s.errors[s.idx]
	}
	s.idx++
	return instr, err
}

func (s *intScriptedRoutingConsultant) queueDispatch(agent, taskDesc string, rowIndex int) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *intScriptedRoutingConsultant) queueStop(reason string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Stop: &domain.StopInstruction{Reason: reason},
	})
	s.errors = append(s.errors, nil)
}

func (s *intScriptedRoutingConsultant) queueError(failure domain.ConsultationFailure) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{})
	s.errors = append(s.errors, &domain.ConsultationError{
		Failure: failure,
		Detail:  "scripted consultation error",
	})
}

// intFixedApprovalReader implements domain.ApprovalReader, always returning the
// same HumanApproval value for every artifact path.
type intFixedApprovalReader struct {
	approval domain.HumanApproval
}

func (r intFixedApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return r.approval
}

// ---- Stage 9 session constructors ----

// newSessionWithRouting builds a session with the real file-based artifact
// store and a routing consultant wired into session.Deps.
func newSessionWithRouting(
	f *harness.MockAdapter,
	artifactPath string,
	routing domain.RoutingConsultant,
) session.Session {
	store := artifact.NewFileStore(artifactPath)
	return session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Approvals: alwaysApprovedReader{},
		Routing:   routing,
	})
}

// newSessionWithApprovals builds a session with the real file-based artifact
// store and a custom ApprovalReader (no routing consultant wired).
func newSessionWithApprovals(
	f *harness.MockAdapter,
	artifactPath string,
	approvals domain.ApprovalReader,
) session.Session {
	store := artifact.NewFileStore(artifactPath)
	return session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Approvals: approvals,
	})
}

// assertGoldenMatches compares the artifact produced at artifactPath with the
// named golden file byte-exactly. With -update it regenerates the golden file
// instead of comparing.
func assertGoldenMatches(t *testing.T, artifactPath, goldenName string) {
	t.Helper()
	produced, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read produced artifact: %v", readErr)
	}

	goldenPath := filepath.Join(goldenDir, goldenName)

	if *update {
		if writeErr := os.WriteFile(goldenPath, produced, 0600); writeErr != nil {
			t.Fatalf("update: write golden file %q: %v", goldenPath, writeErr)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, goldenErr := os.ReadFile(goldenPath)
	if goldenErr != nil {
		t.Fatalf("read golden file %q: %v (run with -update to generate it)", goldenPath, goldenErr)
	}

	if !bytes.Equal(produced, golden) {
		t.Errorf("produced artifact does not match golden file\n\nProduced:\n%s\n\nGolden:\n%s",
			string(produced), string(golden))
	}
}

// integrationRunID is the valid run_id carried by every run configuration and
// pre-written artifact in the integration tests.
const integrationRunID = "20260727T170000Z-a3f9"

// scopedRunFolder creates and returns the run-scoped folder
// Orchestration-{integrationRunID} inside dir, where a resumed run's artifact
// must live.
func scopedRunFolder(t *testing.T, dir string) string {
	t.Helper()
	folder := filepath.Join(dir, domain.RunScopedFolder(integrationRunID))
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatalf("scopedRunFolder: %v", err)
	}
	return folder
}
