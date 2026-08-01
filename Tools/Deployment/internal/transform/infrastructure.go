package transform

import (
	"bytes"
	"fmt"

	"mosaic-deploy/internal/domain"
)

// InfrastructureBlock carries the metadata needed to assemble one
// [[SECTION:InfrastructureAgent:{key}]] block in the orchestrator's
// InfrastructureAgents injection region.
type InfrastructureBlock struct {
	Key         string                        // agent key (section identifier)
	Version     string                        // agent version (for infra-version comment)
	Class       string                        // infrastructure class
	Description string                        // agent description (prose for the table)
	OnFailure   string                        // "halt" or "continue"
	Triggers    []domain.InfrastructureTrigger // one table row per trigger
}

// AssembleInfrastructureBlocks produces the assembled bytes for the
// [[INJECTION:InfrastructureAgents]] region from the given infrastructure agents.
//
// Each agent produces one [[SECTION:InfrastructureAgent:{key}]] block containing a
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

// writeInfrastructureBlock writes one [[SECTION:InfrastructureAgent:{key}]] block to buf.
func writeInfrastructureBlock(buf *bytes.Buffer, agent InfrastructureBlock) {
	// Section open marker.
	fmt.Fprintf(buf, "[[SECTION:InfrastructureAgent:%s]]\n", agent.Key)

	// Version comment immediately inside the section.
	fmt.Fprintf(buf, "<!-- infra-version: %s -->\n", agent.Version)

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

	// Section close marker.
	fmt.Fprintf(buf, "[[/SECTION:InfrastructureAgent:%s]]\n", agent.Key)
}
