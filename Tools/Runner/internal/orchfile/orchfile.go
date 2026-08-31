// Package orchfile reads an orchestrator agent file and enumerates its workflow
// regions. It uses a kind-agnostic depth-first traversal to find nodes whose
// name starts with "Workflow:" at any nesting depth, so both authored workflow
// regions (<Workflow type="core" name="{id}">) and deploy-managed workflow
// regions (<Workflow type="managed" name="{id}">) are discovered. The two
// shapes are interleaved in document order and each region is returned exactly
// once.
//
// Each workflow region carries its identifier (from the region's name attribute),
// version (from the region's version attribute via the parser), and raw content
// bytes (for the workflow parser).
//
// Refusals: missing file, no workflow regions found, missing version attribute,
// duplicate identifier, empty identifier. Every refusal produces a *domain.RefusalError
// naming the specific condition and the resource involved.
package orchfile

import (
	"fmt"
	"os"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// collectWorkflowNodes performs a depth-first, document-order traversal of
// nodes and appends any node whose name starts with "Workflow:" to dst.
// Every kind of node (section, deployed, injection, custom) is visited so that
// both authored workflow regions (type="core") and deploy-managed workflow
// regions (type="managed") are found exactly once.
func collectWorkflowNodes(nodes []*docformat.Node, dst *[]*docformat.Node) {
	for _, n := range nodes {
		if strings.HasPrefix(n.Name(), "Workflow:") {
			*dst = append(*dst, n)
		}
		collectWorkflowNodes(n.Children(), dst)
	}
}

// EnumerateWorkflows reads the file at the given path and returns all workflow
// regions found at any nesting depth, identified by a boundary tag whose name
// starts with "Workflow:". Both authored regions (<Workflow type="core"
// name="{id}">) and deploy-managed regions (<Workflow type="managed"
// name="{id}">) are found, interleaved in document order, each exactly once.
//
// Returns a *domain.RefusalError with a specific message naming the file and
// region if:
//   - The file does not exist or cannot be read
//   - No workflow regions are found
//   - A region has no parseable identifier (empty id part)
//   - A region has no version attribute
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

	// Collect all nodes named "Workflow:{id}" at any nesting depth, regardless
	// of node kind, so both authored (type="core") and deploy-managed
	// (type="managed") workflow regions are found in document order.
	var workflowNodes []*docformat.Node
	collectWorkflowNodes(doc.Body().TopLevelNodes(), &workflowNodes)

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

		// Read version from the region's version attribute.
		version := node.Version()
		if version == "" {
			return nil, &domain.RefusalError{
				Component: "orchfile",
				Resource:  id,
				Reason:    "no version attribute found in workflow region",
			}
		}

		content := node.Content()

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
// <InfrastructureAgents type="project"> region. Each agent is identified by its
// <InfrastructureAgent type="core" name="{name}"> boundary tag.
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

		// Read version from the region's version attribute (optional).
		version := node.Version()

		content := node.Content()

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

// WorkflowNotFoundReason returns the Reason carried by the *domain.RefusalError
// GetWorkflow produces when the file declares no workflow with the given
// identifier. Callers that must tell that refusal apart from the other ways
// reading the file can fail compare against this: several of those failures
// (a missing version attribute, a duplicate identifier) name the same
// component and the same resource, so the reason is what distinguishes them.
func WorkflowNotFoundReason(id string) string {
	return fmt.Sprintf("workflow identifier %q not found in file", id)
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
		Reason:    WorkflowNotFoundReason(id),
	}
}
