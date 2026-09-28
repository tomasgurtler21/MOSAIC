package artifact

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"mosaic-common/docformat"
	"mosaic-run/internal/domain"
)

// Parse parses the canonical orchestration artifact format from the given bytes
// and returns the in-memory ArtifactState.
//
// Returns *domain.RefusalError if the content is not in the canonical format
// (missing type field, missing required sections, or a truncated file).
//
// Empty-value convention: cells that contain "-" in the execution log Stage or
// Checkpoint columns are returned as "" in the ArtifactState. No other component
// ever sees or produces "-" for these fields.
func Parse(data []byte) (domain.ArtifactState, error) {
	refuse := func(reason string) (domain.ArtifactState, error) {
		return domain.ArtifactState{}, &domain.RefusalError{
			Component: "artifact",
			Resource:  "",
			Reason:    reason,
		}
	}

	// Split into frontmatter and body.
	fmContent, bodyContent, hasFM := splitDocument(data)
	if !hasFM {
		return refuse("missing frontmatter (no --- delimiters found)")
	}

	// Parse frontmatter key-value pairs.
	topLevel, currentState, err := parseFrontmatter(fmContent)
	if err != nil {
		return refuse("failed to parse frontmatter: " + err.Error())
	}

	// Check required type field first.
	typeVal, ok := topLevel["type"]
	if !ok || typeVal != "orchestration-artifact" {
		return refuse("missing or incorrect 'type: orchestration-artifact' field")
	}

	// Check for required sections in raw bytes (before full parse).
	if err := checkRequiredSections(data); err != nil {
		return domain.ArtifactState{}, err
	}

	// Build ArtifactState from frontmatter.
	state, err := buildStateFromFrontmatter(fmContent, topLevel, currentState)
	if err != nil {
		return domain.ArtifactState{}, err
	}

	// Parse body sections.
	body := []byte(bodyContent)

	execContent, ok := extractSectionContent(body, "ExecutionLog")
	if !ok {
		return refuse("ExecutionLog section content could not be extracted")
	}
	logEntries, err := parseExecutionLog(execContent)
	if err != nil {
		return refuse("failed to parse execution log: " + err.Error())
	}
	state.ExecutionLog = logEntries

	artsContent, ok := extractSectionContent(body, "Artifacts")
	if !ok {
		return refuse("Artifacts section content could not be extracted")
	}
	regEntries, err := parseArtifactRegistry(artsContent)
	if err != nil {
		return refuse("failed to parse artifact registry: " + err.Error())
	}
	state.ArtifactRegistry = regEntries

	// WorkflowNotes section is optional; absence means empty notes.
	if notesContent, ok := extractSectionContent(body, "WorkflowNotes"); ok {
		notes, err := parseWorkflowNotes(notesContent)
		if err == nil {
			state.WorkflowNotes = notes
		}
	}

	return state, nil
}

// checkRequiredSections verifies the required XML-tagged sections are present
// and not truncated. It checks raw bytes before full parsing so that the error
// message preserves the original check order.
// Trim the trailing newline from tag bytes before searching: files checked out
// on Windows may have CRLF line endings, so searching for a tag with a bare LF
// suffix would fail to match.
func checkRequiredSections(data []byte) error {
	refuse := func(reason string) error {
		return &domain.RefusalError{
			Component: "artifact",
			Reason:    reason,
		}
	}

	execLogOpenTagRaw, _ := docformat.RenderOpenTagLine(docformat.NodeSection, "ExecutionLog", "")
	execLogCloseTagRaw, _ := docformat.RenderCloseTagLine("ExecutionLog")
	artifactsOpenTagRaw, _ := docformat.RenderOpenTagLine(docformat.NodeSection, "Artifacts", "")
	artifactsCloseTagRaw, _ := docformat.RenderCloseTagLine("Artifacts")
	execLogOpenTag := bytes.TrimSuffix(execLogOpenTagRaw, []byte("\n"))
	execLogCloseTag := bytes.TrimSuffix(execLogCloseTagRaw, []byte("\n"))
	artifactsOpenTag := bytes.TrimSuffix(artifactsOpenTagRaw, []byte("\n"))
	artifactsCloseTag := bytes.TrimSuffix(artifactsCloseTagRaw, []byte("\n"))
	if !bytes.Contains(data, execLogOpenTag) {
		return refuse(`missing <ExecutionLog type="core"> section`)
	}
	if !bytes.Contains(data, execLogCloseTag) {
		return refuse("file appears truncated: missing </ExecutionLog> closing tag")
	}
	if !bytes.Contains(data, artifactsOpenTag) {
		return refuse(`missing <Artifacts type="core"> section`)
	}
	if !bytes.Contains(data, artifactsCloseTag) {
		return refuse("file appears truncated: missing </Artifacts> closing tag")
	}
	return nil
}

// buildStateFromFrontmatter constructs an ArtifactState from the parsed
// frontmatter maps. All field parsing and validation is done here so that
// Parse itself stays short.
func buildStateFromFrontmatter(fmContent string, topLevel, currentState map[string]string) (domain.ArtifactState, error) {
	refuse := func(reason string) (domain.ArtifactState, error) {
		return domain.ArtifactState{}, &domain.RefusalError{
			Component: "artifact",
			Reason:    reason,
		}
	}

	state := domain.ArtifactState{
		Type: "orchestration-artifact",
	}

	if v, ok := topLevel["run_id"]; ok {
		state.RunID = v
	}
	if v, ok := topLevel["workflow"]; ok {
		state.Workflow = domain.WorkflowID(v)
	}
	if v, ok := topLevel["workflow_version"]; ok {
		state.WorkflowVersion = domain.WorkflowVersion(v)
	}
	if v, ok := topLevel["task"]; ok {
		state.Task = v
	}
	if v, ok := topLevel["started"]; ok && v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return refuse("invalid 'started' timestamp: " + err.Error())
		}
		state.Started = t
	}
	if v, ok := topLevel["last_updated"]; ok && v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return refuse("invalid 'last_updated' timestamp: " + err.Error())
		}
		state.LastUpdated = t
	}
	if v, ok := topLevel["global_sequence"]; ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return refuse("invalid 'global_sequence': " + err.Error())
		}
		state.GlobalSequence = n
	}
	if v, ok := topLevel["mode"]; ok {
		mode, err := domain.ParseExecutionMode(v)
		if err != nil {
			return refuse("invalid 'mode' value: " + err.Error())
		}
		state.Mode = mode
	}
	if v, ok := topLevel["checkpoints"]; ok {
		switch v {
		case "enabled":
			state.Checkpoints = true
		case "disabled":
			state.Checkpoints = false
		default:
			return refuse("invalid 'checkpoints' value " + `"` + v + `"` + "; valid values: enabled, disabled")
		}
	}
	if v, ok := topLevel["commits"]; ok {
		switch v {
		case "enabled":
			state.Commits = true
		case "disabled":
			state.Commits = false
		default:
			return refuse("invalid 'commits' value " + `"` + v + `"` + "; valid values: enabled, disabled")
		}
	}
	if v, ok := topLevel["commit_branch_variant"]; ok {
		variant, err := domain.ParseCommitBranchVariant(v)
		if err != nil {
			return refuse("invalid 'commit_branch_variant' value: " + err.Error())
		}
		state.CommitBranchVariant = variant
	} else if state.Commits {
		// When commits are enabled and the key is absent, default to the
		// recommended variant (preserves behavior for pre-fix artifacts).
		state.CommitBranchVariant = domain.CommitBranchMOSAICOwned
	}
	// When commits are disabled and the key is absent, CommitBranchVariant
	// remains its zero value (empty string), as it is meaningless in that case.
	if v, ok := topLevel["commit_branch"]; ok {
		state.CommitBranch = v
	}
	if v, ok := topLevel["pre_consultation"]; ok {
		switch v {
		case "enabled":
			state.PreConsultation = true
		case "disabled":
			state.PreConsultation = false
		default:
			return refuse("invalid 'pre_consultation' value " + `"` + v + `"` + "; valid values: enabled, disabled")
		}
	}
	if v, ok := topLevel["manual_resolution"]; ok {
		switch v {
		case "enabled":
			state.ManualResolution = true
		case "disabled":
			state.ManualResolution = false
		default:
			return refuse("invalid 'manual_resolution' value " + `"` + v + `"` + "; valid values: enabled, disabled")
		}
	}

	// Parse infrastructure_overrides block (optional; nil when absent).
	overrides := parseInfrastructureOverrides(fmContent)
	if len(overrides) > 0 {
		state.InfrastructureOverrides = overrides
	}

	// Parse current_state nested block.
	cs := domain.CurrentState{}
	if v, ok := currentState["phase"]; ok {
		cs.Phase = v
	}
	if v, ok := currentState["stage"]; ok {
		cs.Stage = v
	}
	if v, ok := currentState["last_status"]; ok {
		cs.LastStatus = domain.StatusCode(v)
	}
	if v, ok := currentState["last_agent"]; ok {
		cs.LastAgent = v
	}
	if v, ok := currentState["error_code"]; ok {
		cs.ErrorCode = domain.ErrorCode(v)
	}
	state.CurrentState = cs

	return state, nil
}

// splitDocument splits the raw document bytes into frontmatter content and body content.
// Returns hasFM=false when no frontmatter delimiters are found.
// CRLF line endings are normalised to LF before splitting so that the function
// behaves correctly on Windows where git may check out files with CRLF endings.
func splitDocument(data []byte) (fmContent string, bodyContent string, hasFM bool) {
	// Normalise CRLF to LF so the parser works on Windows checkouts.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	if !bytes.HasPrefix(data, []byte("---\n")) {
		return "", string(data), false
	}

	rest := data[4:] // skip "---\n"
	closingMarker := []byte("\n---\n")
	idx := bytes.Index(rest, closingMarker)
	if idx < 0 {
		return "", string(data), false
	}

	// fmContent is between the two "---\n" delimiters.
	fmContent = string(rest[:idx+1])  // include the \n before the closing ---
	bodyContent = string(rest[idx+5:]) // skip \n---\n
	return fmContent, bodyContent, true
}

// parseFrontmatter parses the YAML frontmatter content into top-level key-value
// pairs, the nested current_state pairs, and the infrastructure_overrides block.
// This minimal parser handles only the subset used by orchestration artifacts.
func parseFrontmatter(content string) (topLevel map[string]string, currentState map[string]string, err error) {
	topLevel = make(map[string]string)
	currentState = make(map[string]string)

	lines := strings.Split(content, "\n")
	inCurrentState := false
	inInfraOverrides := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimRight(line, "\r")

		if trimmed == "" {
			continue
		}

		// Detect the infrastructure_overrides block header.
		if trimmed == "infrastructure_overrides:" {
			inInfraOverrides = true
			inCurrentState = false
			continue
		}

		// Detect the current_state block header.
		if trimmed == "current_state:" {
			inCurrentState = true
			inInfraOverrides = false
			continue
		}

		// Skip lines that are part of the infrastructure_overrides block
		// (they are parsed separately by parseInfrastructureOverrides).
		if inInfraOverrides {
			if strings.HasPrefix(line, "  ") {
				// Still inside the block.
				continue
			}
			// No longer in the infra overrides block.
			inInfraOverrides = false
		}

		if inCurrentState {
			if strings.HasPrefix(line, "  ") {
				// Nested current_state key.
				nestedTrimmed := line[2:]
				key, value := parseYAMLLine(nestedTrimmed)
				if key != "" {
					currentState[key] = value
				}
				continue
			}
			// No longer in nested block.
			inCurrentState = false
		}

		key, value := parseYAMLLine(trimmed)
		if key != "" {
			topLevel[key] = value
		}
	}

	return topLevel, currentState, nil
}

// parseInfrastructureOverrides parses the infrastructure_overrides block from
// frontmatter content. The block format is:
//
//	infrastructure_overrides:
//	  agent-name:
//	    triggers:
//	      - trigger: STAGE_END
//	      - trigger: INVOCATION_INTERVAL
//	        trigger_param: 10
//
// Returns nil when the block is absent.
func parseInfrastructureOverrides(content string) []domain.InfrastructureOverride {
	lines := strings.Split(content, "\n")
	inBlock := false

	var overrides []domain.InfrastructureOverride
	var currentAgent *domain.InfrastructureOverride
	var currentTrigger *domain.DeclaredInfraTrigger

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")

		if line == "infrastructure_overrides:" {
			inBlock = true
			continue
		}

		if !inBlock {
			continue
		}

		// Check if we've left the block (non-indented line).
		if len(line) > 0 && !strings.HasPrefix(line, " ") {
			break
		}

		// 2-space indent: agent name (e.g., "  checkpoint-manager-git:")
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			trimmed := strings.TrimSpace(line)
			if strings.HasSuffix(trimmed, ":") {
				// Save previous trigger if any
				if currentTrigger != nil && currentAgent != nil {
					currentAgent.Triggers = append(currentAgent.Triggers, *currentTrigger)
					currentTrigger = nil
				}
				// Save previous agent if any
				if currentAgent != nil {
					overrides = append(overrides, *currentAgent)
				}
				agentName := strings.TrimSuffix(trimmed, ":")
				currentAgent = &domain.InfrastructureOverride{AgentName: agentName}
			}
			continue
		}

		// 4-space or more indent: triggers, trigger items
		if strings.HasPrefix(line, "    ") {
			trimmed := strings.TrimSpace(line)

			// Trigger list item start (e.g., "      - trigger: STAGE_END")
			if strings.HasPrefix(trimmed, "- trigger:") {
				// Save previous trigger if any
				if currentTrigger != nil && currentAgent != nil {
					currentAgent.Triggers = append(currentAgent.Triggers, *currentTrigger)
				}
				triggerVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "- trigger:"))
				currentTrigger = &domain.DeclaredInfraTrigger{Trigger: triggerVal}
				continue
			}

			// Trigger param (e.g., "        trigger_param: 10")
			if strings.HasPrefix(trimmed, "trigger_param:") && currentTrigger != nil {
				paramVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "trigger_param:"))
				currentTrigger.Param = paramVal
				continue
			}

			// Ignore "triggers:" line and other structural lines.
			continue
		}
	}

	// Flush trailing trigger and agent.
	if currentTrigger != nil && currentAgent != nil {
		currentAgent.Triggers = append(currentAgent.Triggers, *currentTrigger)
	}
	if currentAgent != nil {
		overrides = append(overrides, *currentAgent)
	}

	return overrides
}

// parseYAMLLine parses a single YAML line of the form "key: value" or "key: \"value\"".
// Returns ("", "") for blank lines and lines without a colon-space separator.
// Double-quoted values have their quotes stripped. "null" values become "".
func parseYAMLLine(line string) (key, value string) {
	line = strings.TrimRight(line, "\r")

	colonSpaceIdx := strings.Index(line, ": ")
	if colonSpaceIdx > 0 {
		key = line[:colonSpaceIdx]
		raw := line[colonSpaceIdx+2:]
		value = parseYAMLScalar(raw)
		return key, value
	}

	// Handle "key:" with no value (like "current_state:").
	if strings.HasSuffix(line, ":") {
		key = line[:len(line)-1]
		return key, ""
	}

	return "", ""
}

// parseYAMLScalar converts a raw YAML scalar string to a Go string.
// Strips double quotes and converts "null" to "".
func parseYAMLScalar(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "null" {
		return ""
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}
