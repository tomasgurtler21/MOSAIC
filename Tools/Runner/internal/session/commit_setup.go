package session

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

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

// commitSetupRecord holds the result of a successful commit setup dispatch,
// kept in memory until the artifact exists so it can be recorded retroactively
// as the first Execution Log row.
type commitSetupRecord struct {
	agentInstance string
	branchName    string
	status        domain.StatusCode
	summary       string
	completedAt   time.Time
}

// doCommitSetupDispatch dispatches the commit-class infrastructure agent to
// establish the target branch for this run. It returns a populated
// commitSetupRecord on success, or a non-empty refusal message on failure.
// A failed dispatch, missing branch marker, or nil commit agent all result in
// a refusal; no artifact is created before this call.
func (s *sessionImpl) doCommitSetupDispatch(
	ctx context.Context,
	declared []domain.DeclaredInfraAgent,
	config domain.RunConfig,
	orchDir string,
) (*commitSetupRecord, string) {
	commitAgent := firstCommitClassAgent(declared)
	if commitAgent == nil {
		return nil, "commits enabled but no commit-class agent found"
	}
	agentRef, resolveErr := agentresolve.ResolveOne(orchDir, commitAgent.Name)
	if resolveErr != nil {
		return nil, "commit setup: cannot resolve agent definition: " + resolveErr.Error()
	}
	req := domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#1", commitAgent.Name),
		RunID:           config.RunID,
		TaskDescription: "commit setup: establish the target branch for this run",
	}
	response, invokeErr := s.invokeAndLog(ctx, agentRef, req)
	completedAt := s.deps.Clock.Now()
	if invokeErr != nil {
		return nil, "commit setup dispatch failed: " + invokeErr.Error()
	}
	branchName := extractBranchRef(response.StatusMessage)
	if branchName == "" {
		return nil, "commit setup: no [branch:{name}] marker in status_message"
	}
	return &commitSetupRecord{
		agentInstance: req.AgentInstanceID,
		branchName:    branchName,
		status:        response.StatusCode,
		summary:       response.StatusMessage,
		completedAt:   completedAt,
	}, ""
}
