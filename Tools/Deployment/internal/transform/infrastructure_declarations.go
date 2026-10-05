package transform

import (
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

const (
	infrastructureSectionPrefix = "InfrastructureAgent:"
	infrastructureTableHeader   = "| Class | Trigger | Param | On Failure | Description |"
	infrastructureTableDivider  = "|-------|---------|-------|------------|-------------|"
)

// DeclaredInfrastructureAgent is the read-back form of one deployed
// <InfrastructureAgent> declaration.
type DeclaredInfrastructureAgent struct {
	Key     string              // from the section name "InfrastructureAgent:<key>"
	Version string              // tag version attribute; "" when absent
	Parsed  bool                // true when the content matched the assembler shape
	Block   InfrastructureBlock // all fields set when Parsed; otherwise only Key and Version
}

// ReadInfrastructureDeclarations returns the infrastructure agent declarations present in
// the <InfrastructureAgents type="managed"> region of a deployed document, in document order.
// Never fails: returns nil when nothing can be read.
func ReadInfrastructureDeclarations(deployed []byte) []DeclaredInfrastructureAgent {
	if len(deployed) == 0 {
		return nil
	}
	doc, err := docformat.Parse(deployed)
	if err != nil {
		return nil
	}
	region, ok := doc.Body().Deployed("InfrastructureAgents")
	if !ok || !region.Closed() {
		return nil
	}
	return declarationsFromNodes(region.Children())
}

// declarationsFromNodes reads every direct infrastructure section among nodes.
func declarationsFromNodes(nodes []*docformat.Node) []DeclaredInfrastructureAgent {
	var declared []DeclaredInfrastructureAgent
	for _, node := range nodes {
		if node.Kind() != docformat.NodeSection || !strings.HasPrefix(node.Name(), infrastructureSectionPrefix) {
			continue
		}
		key := strings.TrimPrefix(node.Name(), infrastructureSectionPrefix)
		decl := DeclaredInfrastructureAgent{
			Key:     key,
			Version: node.Version(),
			Block:   InfrastructureBlock{Key: key, Version: node.Version()},
		}
		if block, ok := parseInfrastructureSection(key, node.Version(), node.Content()); ok {
			decl.Parsed = true
			decl.Block = block
		}
		declared = append(declared, decl)
	}
	return declared
}

// parseInfrastructureSection reads a section body written by writeInfrastructureBlock:
// an optional display-name line, the table header and divider, and one row per trigger.
// ok is false for any other shape, including rows that disagree on class, on-failure or
// description.
func parseInfrastructureSection(key, version string, content []byte) (InfrastructureBlock, bool) {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	header := -1
	for i, line := range lines {
		if line == infrastructureTableHeader {
			header = i
			break
		}
	}
	if header < 0 || header+1 >= len(lines) || lines[header+1] != infrastructureTableDivider {
		return InfrastructureBlock{}, false
	}

	block := InfrastructureBlock{Key: key, Version: version}
	for i, line := range lines[:header] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if i != 0 || block.Name != "" {
			return InfrastructureBlock{}, false
		}
		block.Name = line
	}

	rows := 0
	for _, line := range lines[header+2:] {
		if strings.TrimSpace(line) == "" {
			if rows == 0 {
				return InfrastructureBlock{}, false
			}
			break
		}
		cells, ok := splitInfrastructureRow(line)
		if !ok {
			return InfrastructureBlock{}, false
		}
		if rows == 0 {
			block.Class, block.OnFailure, block.Description = cells[0], cells[3], cells[4]
		} else if cells[0] != block.Class || cells[3] != block.OnFailure || cells[4] != block.Description {
			return InfrastructureBlock{}, false
		}
		param := cells[2]
		if param == "-" {
			param = ""
		}
		block.Triggers = append(block.Triggers, domain.InfrastructureTrigger{Trigger: cells[1], TriggerParam: param})
		rows++
	}
	if rows == 0 {
		return InfrastructureBlock{}, false
	}
	// Anything after the table must be blank.
	for _, line := range lines[header+2+rows:] {
		if strings.TrimSpace(line) != "" {
			return InfrastructureBlock{}, false
		}
	}
	return block, true
}

// splitInfrastructureRow splits one table row into its five cells. Descriptions are written
// unescaped, so everything after the fourth separator is the description, pipes included.
func splitInfrastructureRow(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "| ") || !strings.HasSuffix(line, " |") || len(line) < 4 {
		return nil, false
	}
	cells := strings.SplitN(line[2:len(line)-2], " | ", 5)
	if len(cells) != 5 {
		return nil, false
	}
	return cells, true
}
