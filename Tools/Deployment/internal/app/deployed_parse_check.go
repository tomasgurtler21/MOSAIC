package app

import (
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
)

// probePlannedPathForAgent probes the planned target path of an id-bearing agent that has no
// index match. A file there that cannot be parsed cleanly (parse error, no frontmatter, or no
// id) is reported present and parse-failed so the planner raises a conflict instead of
// planning a create that would overwrite it. A cleanly parsed file (with a different id) or an
// absent file yields Present: false.
func probePlannedPathForAgent(workspace, targetPath, modelKey string, scanFlagged bool) domain.DeployedArtifactState {
	state := probeDeployedArtifact(workspace, targetPath, modelKey)
	if !state.Present {
		return domain.DeployedArtifactState{Present: false}
	}

	problem := plannedPathParseProblem(filepath.Join(workspace, targetPath))
	if problem == "" && !scanFlagged {
		return domain.DeployedArtifactState{Present: false}
	}

	state.ParseFailed = true
	state.ParseProblem = problem
	if problem != "" {
		// Stamps read from a file that is not a clean agent document are not trustworthy.
		state.Version = ""
		state.HarnessVersion = ""
	}
	return state
}

// plannedPathParseProblem returns a description of why the file at fullPath cannot be used as a
// deployed agent, or "" when it parses cleanly and carries an id.
func plannedPathParseProblem(fullPath string) string {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return err.Error()
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return err.Error()
	}
	fm := doc.Frontmatter()
	if !fm.Present() {
		return "no frontmatter"
	}
	idField, _ := agentfields.ByGeneric("id")
	for _, key := range agentfields.ReadOrder(idField) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
			return ""
		}
	}
	return "frontmatter has no id"
}
