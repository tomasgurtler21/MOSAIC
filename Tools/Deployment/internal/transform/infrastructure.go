package transform

import (
	"bytes"
	"fmt"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// InfrastructureBlock carries the metadata needed to assemble one
// <InfrastructureAgent type="core" name="{key}"> block in the orchestrator's
// InfrastructureAgents injection region.
type InfrastructureBlock struct {
	Key         string                        // agent key (section identifier)
	Name        string                        // agent display name; emitted when non-empty and distinct from Key
	Version     string                        // agent version (for the version attribute on the section tag)
	Class       string                        // infrastructure class
	Description string                        // agent description (prose for the table)
	OnFailure   string                        // "halt" or "continue"
	Triggers    []domain.InfrastructureTrigger // one table row per trigger
}

// AssembleInfrastructureBlocks produces the assembled bytes for the
// <InfrastructureAgents type="project"> region from the given infrastructure agents.
//
// Each agent produces one <InfrastructureAgent type="core" name="{key}"> block containing a
// markdown table with columns: Class | Trigger | Param | On Failure | Description.
// One row is emitted per trigger entry; Class, On Failure, and Description are repeated
// on every row. An empty TriggerParam is rendered as "-".
//
// Agents are emitted in the order they appear in the input slice. The caller is responsible
// for providing a deterministic order.
//
// Returns nil bytes and nil keys when the input slice is empty (producing an empty injection
// region, which is valid per the design).
func AssembleInfrastructureBlocks(agents []InfrastructureBlock) (assembled []byte, keys []string) {
	if len(agents) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	keys = make([]string, 0, len(agents))

	for _, agent := range agents {
		writeInfrastructureBlock(&buf, agent)
		keys = append(keys, agent.Key)
	}

	return buf.Bytes(), keys
}

// writeInfrastructureBlock writes one infrastructure agent section block to buf.
// The section open tag carries the agent's version as a tag attribute; no version
// comment is emitted inside the section.
func writeInfrastructureBlock(buf *bytes.Buffer, agent InfrastructureBlock) {
	// Section open tag with version attribute. RenderOpenTagLine errors only on invalid
	// names or kinds; both are controlled here, so the error is safely ignored.
	openTag, _ := docformat.RenderOpenTagLine(docformat.NodeSection, "InfrastructureAgent:"+agent.Key, agent.Version)
	buf.Write(openTag)

	// Emit the display name when it differs from the key. When an agent uses a
	// human-readable Name distinct from its key, this makes the section self-describing.
	// When Name equals Key (which is the common case for real catalog agents), nothing
	// is emitted, preserving the existing format.
	if agent.Name != "" && agent.Name != agent.Key {
		fmt.Fprintf(buf, "%s\n\n", agent.Name)
	}

	// Table header and separator.
	buf.WriteString("| Class | Trigger | Param | On Failure | Description |\n")
	buf.WriteString("|-------|---------|-------|------------|-------------|\n")

	// One data row per trigger.
	for _, trig := range agent.Triggers {
		param := trig.TriggerParam
		if param == "" {
			param = "-"
		}
		fmt.Fprintf(buf, "| %s | %s | %s | %s | %s |\n",
			agent.Class, trig.Trigger, param, agent.OnFailure, agent.Description)
	}

	// Section close tag. RenderCloseTagLine errors only on invalid names; controlled here.
	closeTag, _ := docformat.RenderCloseTagLine("InfrastructureAgent:" + agent.Key)
	buf.Write(closeTag)
}
