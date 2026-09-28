package session

import (
	"errors"
	"fmt"
	"path/filepath"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/orchfile"
)

// refusal logs the refusal reason and constructs a RunOutcome with RunRefused
// status. Every run-start refusal (pre-Store.Create) passes through here so
// that paths which never reach Store.Apply still leave a debug-log trace.
func (s *sessionImpl) refusal(message string) domain.RunOutcome {
	s.deps.Debug.Log(domain.EventSessionRefusal, message)
	return domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: message,
	}
}

// refusalCaused is like refusal but also carries an error cause in the
// outcome's Cause field. It is used when the refusal is attributable to a
// structured error whose identity must survive to the frontend (e.g. a
// *domain.HarnessLaunchError that the TUI needs to detect via errors.As).
func (s *sessionImpl) refusalCaused(message string, cause error) domain.RunOutcome {
	s.deps.Debug.Log(domain.EventSessionRefusal, message)
	return domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: message,
		Cause:   cause,
	}
}

// isWorkflowNotFound reports whether err is orchfile's "this identifier is not
// declared here" refusal for the given workflow ID, as opposed to any other way
// reading the orchestrator file can fail. Component and resource alone do not
// settle it: a workflow region missing its version attribute and a duplicated
// identifier both refuse under the same component naming the same workflow, and
// reporting either of those as a workflow that has gone missing sends the user
// looking for something that is sitting in the file where they expect it. The
// reason is what separates them, so that is what is matched.
func isWorkflowNotFound(err error, workflowID string) bool {
	var refErr *domain.RefusalError
	return errors.As(err, &refErr) &&
		refErr.Component == "orchfile" &&
		refErr.Resource == workflowID &&
		refErr.Reason == orchfile.WorkflowNotFoundReason(workflowID)
}

// resumeWorkflowGoneRefusal refuses a resumed run whose workflow the
// orchestrator file does not declare. The message names both halves of the
// problem -- which workflow is missing and which run is stranded by its absence
// -- because a workspace holds many runs and naming only one of the two leaves
// the user unable to tell whether to fix the file or abandon the run. It says
// nothing about where the identifier came from: the TUI carries it from the
// run's own artifact, but a CLI resume takes it from --workflow, and telling a
// user who has just mistyped that flag to go inspect the run's history sends
// them away from the mistake rather than towards it.
func (s *sessionImpl) resumeWorkflowGoneRefusal(runID, workflowID string) domain.RunOutcome {
	reason := fmt.Sprintf("workflow %q for run %s is not declared in the current "+
		"orchestrator file; it may have been renamed or removed", workflowID, runID)
	return s.refusalCaused(reason, &domain.RefusalError{
		Component: "workflow",
		Resource:  runID,
		Reason:    reason,
	})
}

// resumeRecordedNoWorkflowRefusal refuses a resumed run that carries no
// workflow at all. The message says exactly that rather than reporting a lookup
// that failed for an identifier the user never sees: there is no workflow to go
// looking for, and implying otherwise sends them somewhere with nothing to
// find. The causes are listed as possibilities rather than as a diagnosis --
// the artifact may be missing or unreadable, but it may equally be present and
// well-formed while recording no workflow, and the caller does not tell the
// three apart.
func (s *sessionImpl) resumeRecordedNoWorkflowRefusal(runID string) domain.RunOutcome {
	reason := fmt.Sprintf("run %s records no workflow, so there is nothing to resume it as; "+
		"its artifact may be missing, unreadable, or may record no workflow", runID)
	return s.refusalCaused(reason, &domain.RefusalError{
		Component: "workflow",
		Resource:  runID,
		Reason:    reason,
	})
}

// startFailed builds the outcome for a run start that failed after the run
// artifact was created (commit setup or pre-consultation). Everything written
// so far is kept and a resume retries the failed step. cause may be nil.
func (s *sessionImpl) startFailed(message string, cause error) domain.RunOutcome {
	s.deps.Debug.Log(domain.EventSessionRefusal, message)
	return domain.RunOutcome{
		Status:  domain.RunStartFailed,
		Message: message,
		Cause:   cause,
	}
}

// checkResumeRunIdentity reports why a resumed artifact's recorded run_id is
// unusable, or nil when it is present, well-formed and matches the run the
// enclosing Orchestration-{run_id}/ folder belongs to.
func checkResumeRunIdentity(recorded string, config domain.RunConfig) *domain.RunIdentityError {
	switch {
	case recorded == "":
		return &domain.RunIdentityError{Problem: domain.RunIdentityEmpty, RunID: recorded}
	case !domain.IsValidRunID(recorded):
		return &domain.RunIdentityError{Problem: domain.RunIdentityMalformed, RunID: recorded}
	}
	folderRunID := config.RunID
	if config.RunFolder != "" {
		if id, ok := domain.ParseRunFolder(filepath.Base(config.RunFolder)); ok {
			folderRunID = id
		}
	}
	if folderRunID != "" && recorded != folderRunID {
		return &domain.RunIdentityError{
			Problem: domain.RunIdentityFolderMismatch,
			RunID:   recorded,
			Folder:  domain.RunScopedFolder(folderRunID),
		}
	}
	return nil
}

// runIdentityRefusal refuses a resume whose artifact has an unusable run_id.
// The message names the problem; the cause carries it to the frontends.
func (s *sessionImpl) runIdentityRefusal(idErr *domain.RunIdentityError, config domain.RunConfig) domain.RunOutcome {
	var reason string
	switch idErr.Problem {
	case domain.RunIdentityMalformed:
		reason = fmt.Sprintf("the artifact's run_id %q is malformed; a valid run_id "+
			"({YYYYMMDD}T{HHMMSS}Z-{4-hex}) is required to resume", idErr.RunID)
	case domain.RunIdentityFolderMismatch:
		reason = fmt.Sprintf("the artifact's run_id %q does not match its run folder %q",
			idErr.RunID, idErr.Folder)
	default:
		reason = "the artifact has no run_id (absent or empty); a valid run_id is required to resume"
	}
	return s.refusalCaused(reason, &domain.RefusalError{
		Component: "artifact",
		Resource:  config.RunID,
		Reason:    reason,
		Cause:     idErr,
	})
}
