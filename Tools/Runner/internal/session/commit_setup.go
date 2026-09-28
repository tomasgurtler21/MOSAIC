package session

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/domain"
)

// hasCheckpointClassAgent reports whether any declared infrastructure agent
// has Class == "checkpoint".
func hasCheckpointClassAgent(agents []domain.DeclaredInfraAgent) bool {
	for _, a := range agents {
		if a.Class == "checkpoint" {
			return true
		}
	}
	return false
}

// hasCommitClassAgent reports whether any declared infrastructure agent
// has Class == "commit".
func hasCommitClassAgent(agents []domain.DeclaredInfraAgent) bool {
	for _, a := range agents {
		if a.Class == "commit" {
			return true
		}
	}
	return false
}

// firstCommitClassAgent returns the first declared infrastructure agent with
// Class == "commit", or nil if none is declared.
func firstCommitClassAgent(agents []domain.DeclaredInfraAgent) *domain.DeclaredInfraAgent {
	for i := range agents {
		if agents[i].Class == "commit" {
			return &agents[i]
		}
	}
	return nil
}

// branchMarkerRe matches the [branch:{name}] marker pattern in a status_message.
// The branch name is captured in group 1.
var branchMarkerRe = regexp.MustCompile(`\[branch:([^\]]+)\]`)

// extractBranchRef scans statusMessage for a [branch:{name}] marker and
// returns the branch name, trimmed of surrounding whitespace. Returns "" when
// no marker is found.
func extractBranchRef(statusMessage string) string {
	m := branchMarkerRe.FindStringSubmatch(statusMessage)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// checkpointMarkerRe matches the [checkpoint:{sha}] marker pattern in a
// status_message. The sha is captured in group 1.
var checkpointMarkerRe = regexp.MustCompile(`\[checkpoint:([^\]]+)\]`)

// extractCheckpointRef scans statusMessage for a [checkpoint:{sha}] marker
// and returns the sha string. Returns "" when no marker is found.
func extractCheckpointRef(statusMessage string) string {
	m := checkpointMarkerRe.FindStringSubmatch(statusMessage)
	if m == nil {
		return ""
	}
	return m[1]
}

// resolveCommitSetupAgent resolves the commit-class infrastructure agent that
// performs commit setup. It runs before the artifact is created so a missing
// or unresolvable agent is an ordinary refusal that leaves nothing behind.
func resolveCommitSetupAgent(declared []domain.DeclaredInfraAgent, orchDir string) (domain.AgentReference, string) {
	commitAgent := firstCommitClassAgent(declared)
	if commitAgent == nil {
		return domain.AgentReference{}, "commits enabled but no commit-class agent found"
	}
	agentRef, err := agentresolve.ResolveOne(orchDir, commitAgent.Name)
	if err != nil {
		return domain.AgentReference{}, "commit setup: cannot resolve agent definition: " + err.Error()
	}
	return agentRef, ""
}

// needsCommitSetup reports whether the run must dispatch commit setup: commits
// are enabled and the artifact records no commit_branch yet. A recorded
// commit_branch means setup already succeeded and is never run again.
func needsCommitSetup(state domain.ArtifactState) bool {
	return state.Commits && state.CommitBranch == ""
}

// runCommitSetup dispatches the commit-class agent as an ordinary out-of-band
// invocation against an artifact that already exists. The invocation takes the
// next sequence and always gets an Execution Log row, whatever the protocol
// outcome; the row is an infrastructure row, so current_state does not move.
//
// Only a SUCCESS response carrying a readable [branch:{name}] marker completes
// setup: the branch is then recorded through the store. Any other outcome
// returns a RunStartFailed outcome that keeps the artifact and the setup row;
// the run stays resumable and a resume retries setup.
//
// On success done is false and the returned state includes the setup row and
// commit_branch. A store failure is returned as an error.
func (s *sessionImpl) runCommitSetup(
	ctx context.Context,
	agentRef domain.AgentReference,
	state domain.ArtifactState,
	runID string,
) (domain.ArtifactState, domain.RunOutcome, bool, error) {
	seq := state.GlobalSequence + 1
	req := domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#%d", agentRef.Identifier, seq),
		RunID:           runID,
		TaskDescription: "commit setup: establish the target branch for this run",
	}
	response, invokeErr := s.invokeAndLog(ctx, agentRef, req)
	step := domain.CompletedStep{
		Seq:              seq,
		AgentInstance:    req.AgentInstanceID,
		Timestamp:        s.deps.Clock.Now(),
		IsInfrastructure: true,
	}
	if invokeErr != nil {
		step.Status = domain.StatusBLOCKED
		step.ErrorCode = domain.ErrorTOOL_UNAVAILABLE
		step.Summary = invokeErr.Error()
	} else {
		step.Status = response.StatusCode
		step.ErrorCode = response.ErrorCode
		step.Summary = response.StatusMessage
	}
	recorded, err := s.deps.Store.Apply(ctx, state, step)
	if err != nil {
		return state, domain.RunOutcome{Status: domain.RunFailed, Message: "commit setup row apply failed: " + err.Error()}, true, err
	}

	if invokeErr != nil {
		out := s.startFailed("commit setup dispatch failed: "+invokeErr.Error(), invokeErr)
		return recorded, out, true, nil
	}
	if response.StatusCode != domain.StatusSUCCESS {
		reason := response.StatusMessage
		if response.ErrorReason != "" {
			reason = response.ErrorReason
		}
		out := s.startFailed(fmt.Sprintf("commit setup did not succeed (%s): %s", response.StatusCode, reason), nil)
		return recorded, out, true, nil
	}
	branch := extractBranchRef(response.StatusMessage)
	if branch == "" {
		out := s.startFailed("commit setup: no [branch:{name}] marker in status_message", nil)
		return recorded, out, true, nil
	}
	withBranch, err := s.deps.Store.SetCommitBranch(ctx, branch, s.deps.Clock.Now())
	if err != nil {
		return recorded, domain.RunOutcome{Status: domain.RunFailed, Message: "recording commit_branch failed: " + err.Error()}, true, err
	}
	return withBranch, domain.RunOutcome{}, false, nil
}
