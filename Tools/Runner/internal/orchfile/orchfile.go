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
	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// versionCommentRe matches <!-- workflow-version: {version} --> comments.
var versionCommentRe = regexp.MustCompile(`<!--\s*workflow-version:\s*(\S+)\s*-->`)

// infraVersionCommentRe matches <!-- infra-version: {version} --> comments.
var infraVersionCommentRe = regexp.MustCompile(`<!--\s*infra-version:\s*(\S+)\s*-->`)

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

// EnumerateInfrastructureAgents reads the file at the given path and returns
// all infrastructure agent declarations found in the
// [[INJECTION:InfrastructureAgents]] region. Each agent is identified by its
// [[SECTION:InfrastructureAgent:{name}]] boundary tag.
//
// Returns an empty slice (not an error) when the injection region is absent or
// empty — this is valid per the design.
//
// Returns a *domain.RefusalError if:
//   - The file does not exist or cannot be read
//   - A section has an empty name
//   - Duplicate section names exist
func EnumerateInfrastructureAgents(path string) ([]domain.DeclaredInfraAgent, error) {
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

	// Collect all sections named "InfrastructureAgent:{name}" at any nesting depth.
	allSections := doc.Body().SectionsDeep()
	var infraNodes []*docformat.Node
	for _, s := range allSections {
		if strings.HasPrefix(s.Name(), "InfrastructureAgent:") {
			infraNodes = append(infraNodes, s)
		}
	}

	// Empty is valid — no infrastructure agents deployed.
	if len(infraNodes) == 0 {
		return nil, nil
	}

	seen := map[string]bool{}
	var agents []domain.DeclaredInfraAgent

	for _, node := range infraNodes {
		name := strings.TrimPrefix(node.Name(), "InfrastructureAgent:")
		if name == "" {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  path,
				Reason:    "infrastructure agent section has empty name",
			}
		}
		if seen[name] {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  name,
				Reason:    fmt.Sprintf("duplicate infrastructure agent name %q", name),
			}
		}
		seen[name] = true

		content := node.Content()

		// Extract version from the <!-- infra-version: {version} --> comment.
		version := ""
		submatch := infraVersionCommentRe.FindSubmatch(content)
		if submatch != nil {
			version = string(submatch[1])
		}

		// Parse the markdown table inside the section.
		agent, parseErr := parseInfraAgentTable(name, version, content)
		if parseErr != nil {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  name,
				Reason:    fmt.Sprintf("cannot parse infrastructure agent table: %v", parseErr),
			}
		}

		agents = append(agents, agent)
	}

	return agents, nil
}

// parseInfraAgentTable parses the declaration table inside an infrastructure
// agent section and returns a DeclaredInfraAgent.
func parseInfraAgentTable(name, version string, content []byte) (domain.DeclaredInfraAgent, error) {
	agent := domain.DeclaredInfraAgent{
		Name:    name,
		Version: version,
	}

	t, err := mdtable.Parse(content)
	if err != nil {
		// No table found — return agent with no triggers (valid).
		return agent, nil
	}
	if len(t.Rows) == 0 {
		return agent, nil
	}

	classCol := t.Column("Class")
	triggerCol := t.Column("Trigger")
	paramCol := t.Column("Param")
	onFailureCol := t.Column("On Failure")

	for _, row := range t.Rows {
		if classCol >= 0 && agent.Class == "" {
			agent.Class = strings.TrimSpace(row[classCol])
		}
		if onFailureCol >= 0 && agent.OnFailure == "" {
			agent.OnFailure = strings.TrimSpace(row[onFailureCol])
		}

		tr := domain.DeclaredInfraTrigger{}
		if triggerCol >= 0 {
			tr.Trigger = strings.TrimSpace(row[triggerCol])
		}
		if paramCol >= 0 {
			p := strings.TrimSpace(row[paramCol])
			if p != "-" {
				tr.Param = p
			}
		}
		if tr.Trigger != "" {
			agent.Triggers = append(agent.Triggers, tr)
		}
	}

	return agent, nil
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
