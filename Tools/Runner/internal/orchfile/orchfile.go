// Package orchfile reads an orchestrator agent file and enumerates its workflow
// regions. It uses docformat's depth-first section lookup to find
// [[SECTION:Workflow:{id}]] nodes at any nesting depth, making it independent
// of the structural depth at which workflows are embedded (bare top-level files
// as well as deployed agents with nested injection slots both work).
//
// Each workflow region carries its identifier (from the section name), version
// (from the <!-- workflow-version: {version} --> comment immediately inside the
// region), and raw content bytes (for the workflow parser).
//
// Refusals: missing file, no workflow regions found, missing version comment,
// duplicate identifier, empty identifier. Every refusal produces a *domain.RefusalError
// naming the specific condition and the resource involved.
package orchfile

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"mosaic-common/docformat"
	"mosaic-run/internal/domain"
)

// versionCommentRe matches <!-- workflow-version: {version} --> comments.
var versionCommentRe = regexp.MustCompile(`<!--\s*workflow-version:\s*(\S+)\s*-->`)

// EnumerateWorkflows reads the file at the given path and returns all workflow
// regions found at any nesting depth, identified by their
// [[SECTION:Workflow:{id}]] boundary tags.
//
// Returns a *domain.RefusalError with a specific message naming the file and
// region if:
//   - The file does not exist or cannot be read
//   - No workflow regions are found
//   - A region has no parseable identifier (empty id part)
//   - A region has no version comment
//   - Two regions declare the same identifier
func EnumerateWorkflows(path string) ([]domain.WorkflowRegion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &domain.RefusalError{
			Component: "orchfile",
			Resource:  path,
			Reason:    "file not found or cannot be read",
		}
	}

	doc, err := docformat.Parse(data)
	if err != nil {
		return nil, &domain.RefusalError{
			Component: "orchfile",
			Resource:  path,
			Reason:    fmt.Sprintf("cannot parse file: %v", err),
		}
	}

	// Collect all sections named "Workflow:{id}" at any nesting depth.
	allSections := doc.Body().SectionsDeep()
	var workflowNodes []*docformat.Node
	for _, s := range allSections {
		if strings.HasPrefix(s.Name(), "Workflow:") {
			workflowNodes = append(workflowNodes, s)
		}
	}

	if len(workflowNodes) == 0 {
		return nil, &domain.RefusalError{
			Component: "orchfile",
			Resource:  path,
			Reason:    "no workflow regions found",
		}
	}

	seen := map[string]bool{}
	var regions []domain.WorkflowRegion

	for _, node := range workflowNodes {
		// Extract the identifier: everything after the "Workflow:" prefix.
		id := strings.TrimPrefix(node.Name(), "Workflow:")
		if id == "" {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  path,
				Reason:    "workflow section has empty identifier",
			}
		}

		content := node.Content()

		// Extract version from the <!-- workflow-version: {version} --> comment.
		submatch := versionCommentRe.FindSubmatch(content)
		if submatch == nil {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  id,
				Reason:    "no version comment found in workflow region",
			}
		}
		version := string(submatch[1])

		// Refuse duplicate identifiers.
		if seen[id] {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  id,
				Reason:    fmt.Sprintf("duplicate workflow identifier %q", id),
			}
		}
		seen[id] = true

		regions = append(regions, domain.WorkflowRegion{
			Info: domain.WorkflowInfo{
				ID:      domain.WorkflowID(id),
				Version: domain.WorkflowVersion(version),
			},
			Content: content,
		})
	}

	return regions, nil
}

// GetWorkflow returns the workflow region with the given identifier from the
// file at path.
//
// Returns a *domain.RefusalError if the identifier is not found in the file,
// or if the file itself cannot be enumerated (see EnumerateWorkflows).
func GetWorkflow(path string, id string) (domain.WorkflowRegion, error) {
	regions, err := EnumerateWorkflows(path)
	if err != nil {
		return domain.WorkflowRegion{}, err
	}

	for _, r := range regions {
		if string(r.Info.ID) == id {
			return r, nil
		}
	}

	return domain.WorkflowRegion{}, &domain.RefusalError{
		Component: "orchfile",
		Resource:  id,
		Reason:    fmt.Sprintf("workflow identifier %q not found in file", id),
	}
}
