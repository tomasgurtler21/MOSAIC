package session_test

// Tests for run identity on resume: an artifact whose run_id is absent, empty,
// malformed or different from its enclosing Orchestration-{run_id}/ folder is
// refused before any invocation, the refusal names the problem, and no
// replacement identity is ever minted.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// resumableLinearState is a linear-workflow artifact with agent-a done and
// agent-b remaining, recording the given run_id.
func resumableLinearState(runID string) domain.ArtifactState {
	return domain.ArtifactState{
		RunID:           runID,
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
}

func TestSession_Resume_InvalidRunIdentity_RefusedBeforeAnyInvocation(t *testing.T) {
	const otherRunID = "20260101T000000Z-abcd"
	tests := []struct {
		name        string
		recordedID  string
		folderRunID string   // run_id the enclosing folder is named after
		problems    []domain.RunIdentityProblem
		mentions    []string // fragments the refusal must contain
	}{
		{
			name:        "empty recorded run_id",
			recordedID:  "",
			folderRunID: testRunID,
			problems:    []domain.RunIdentityProblem{domain.RunIdentityAbsent, domain.RunIdentityEmpty},
			mentions:    []string{"run_id"},
		},
		{
			name:        "malformed run_id",
			recordedID:  "not-a-run-id",
			folderRunID: testRunID,
			problems:    []domain.RunIdentityProblem{domain.RunIdentityMalformed},
			mentions:    []string{"run_id", "malformed"},
		},
		{
			name:        "run_id differs from the enclosing folder",
			recordedID:  otherRunID,
			folderRunID: testRunID,
			problems:    []domain.RunIdentityProblem{domain.RunIdentityFolderMismatch},
			mentions:    []string{otherRunID, testRunID},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ses, f, store, orchPath := newLinearSession(t)
			store.state = resumableLinearState(tc.recordedID)
			store.exists = true
			store.keepRecordedIdentity = true
			cfg := baseLinearConfig(orchPath)
			cfg.IsNewRun = false
			cfg.RunID = tc.folderRunID
			cfg.RunFolder = filepath.Join(filepath.Dir(orchPath), domain.RunScopedFolder(tc.folderRunID))

			// Act
			got, err := ses.Start(context.Background(), cfg)

			// Assert
			msg := requireRefused(t, got, err)
			for _, want := range tc.mentions {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal must name %q, got %q", want, msg)
				}
			}
			var idErr *domain.RunIdentityError
			if !errors.As(got.Cause, &idErr) {
				t.Fatalf("refusal Cause must carry *domain.RunIdentityError, got %T (%v)", got.Cause, got.Cause)
			}
			if !containsProblem(tc.problems, idErr.Problem) {
				t.Errorf("want problem in %v, got %q", tc.problems, idErr.Problem)
			}
			if n := len(f.Invocations()); n != 0 {
				t.Errorf("want no harness invocation before the refusal, got %d", n)
			}
			if len(store.Applied) != 0 {
				t.Errorf("want nothing recorded on a refused resume, got %d applied steps", len(store.Applied))
			}
		})
	}
}

func containsProblem(set []domain.RunIdentityProblem, p domain.RunIdentityProblem) bool {
	for _, s := range set {
		if s == p {
			return true
		}
	}
	return false
}

// A refused resume must not repair the identity: nothing is created, and the
// recorded state keeps the run_id it had.
func TestSession_Resume_InvalidRunIdentity_MintsNoReplacement(t *testing.T) {
	// Arrange
	ses, _, store, orchPath := newLinearSession(t)
	store.state = resumableLinearState("")
	store.exists = true
	store.keepRecordedIdentity = true
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	// Act
	got, err := ses.Start(context.Background(), cfg)

	// Assert
	requireRefused(t, got, err)
	if store.CreatedRunID != "" {
		t.Errorf("want no artifact created on a refused resume, Create saw run_id %q", store.CreatedRunID)
	}
	if store.state.RunID != "" {
		t.Errorf("want the recorded run_id left as found, got %q", store.state.RunID)
	}
}

// A current artifact whose run_id matches its folder resumes normally and its
// run_id reaches the invoked agent, with paths scoped to the run folder.
func TestSession_Resume_CurrentRunIdentity_ResumesAndScopesRequest(t *testing.T) {
	// Arrange
	ses, f, store, orchPath := newLinearSession(t)
	store.state = resumableLinearState(testRunID)
	store.exists = true
	store.keepRecordedIdentity = true
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	// Act
	got, err := ses.Start(context.Background(), cfg)

	// Assert
	requireRunStatus(t, got, err, domain.RunCompleted)
	invs := f.Invocations()
	if len(invs) != 1 {
		t.Fatalf("want exactly agent-b invoked, got %d invocations", len(invs))
	}
	req := invs[0].Request
	if req.RunID != testRunID {
		t.Errorf("request RunID: want %q, got %q", testRunID, req.RunID)
	}
	prefix := domain.RunScopedFolder(testRunID) + "/"
	for _, p := range append(append([]string{}, req.InputArtifacts...), req.OutputArtifacts...) {
		if !strings.HasPrefix(p, prefix) {
			t.Errorf("artifact path %q must be scoped to %q", p, prefix)
		}
	}
}
