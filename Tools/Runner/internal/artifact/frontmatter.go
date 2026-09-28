package artifact

import (
	"strings"

	"mosaic-run/internal/domain"
)

// parseFrontmatterEntries splits frontmatter into its top-level entries in file
// order. An entry is a non-indented line plus every following indented (or
// top-level list item) line. Blank lines and top-level comments are dropped.
func parseFrontmatterEntries(content string) []domain.FrontmatterEntry {
	var entries []domain.FrontmatterEntry
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		continuation := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "- ")
		if continuation && len(entries) > 0 {
			last := &entries[len(entries)-1]
			last.Lines = append(last.Lines, line)
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, _ := parseYAMLLine(line)
		if key == "" {
			continue
		}
		entries = append(entries, domain.FrontmatterEntry{Key: key, Lines: []string{line}})
	}
	return entries
}

// parseFrontmatter parses the YAML frontmatter content into top-level scalar
// key-value pairs and the nested current_state pairs. This minimal parser
// handles only the subset used by orchestration artifacts; block-valued keys
// are read by their dedicated parsers.
func parseFrontmatter(content string) (topLevel map[string]string, currentState map[string]string, err error) {
	topLevel = make(map[string]string)
	currentState = make(map[string]string)
	for _, e := range parseFrontmatterEntries(content) {
		if e.Key == "current_state" {
			for _, nested := range e.Lines[1:] {
				key, value := parseYAMLLine(strings.TrimSpace(nested))
				if key != "" {
					currentState[key] = value
				}
			}
			continue
		}
		_, value := parseYAMLLine(e.Lines[0])
		topLevel[e.Key] = value
	}
	return topLevel, currentState, nil
}

// modelledFrontmatterKeys are the top-level keys this version reads (or, for
// the legacy aliases, consumes). Every other key is preserved verbatim.
var modelledFrontmatterKeys = map[string]bool{
	"type": true, "run_id": true, "workflow": true, "workflow_version": true,
	"task": true, "started": true, "last_updated": true, "global_sequence": true,
	"checkpoints": true, "commits": true, "commit_branch": true,
	"review_loop_limit": true, "infrastructure_overrides": true,
	"infrastructure_selections": true, "current_state": true,
	"runner_mode": true, "runner_pre_consultation": true, "runner_manual_resolution": true,
	// legacy aliases, consumed on read and never written
	"mode": true, "pre_consultation": true, "manual_resolution": true,
	"commit_branch_variant": true,
}

func unknownEntries(entries []domain.FrontmatterEntry) []domain.FrontmatterEntry {
	var unknown []domain.FrontmatterEntry
	for _, e := range entries {
		if !modelledFrontmatterKeys[e.Key] {
			unknown = append(unknown, e)
		}
	}
	return unknown
}

// parseRunID requires a present, non-empty, well-formed run_id.
func parseRunID(topLevel map[string]string) (string, error) {
	v, ok := topLevel["run_id"]
	problem := domain.RunIdentityProblem("")
	switch {
	case !ok:
		problem = domain.RunIdentityAbsent
	case v == "":
		problem = domain.RunIdentityEmpty
	case !domain.IsValidRunID(v):
		problem = domain.RunIdentityMalformed
	}
	if problem != "" {
		return "", &domain.RefusalError{
			Component: "artifact",
			Reason:    "run_id is " + string(problem) + "; a valid run_id ({YYYYMMDD}T{HHMMSS}Z-{4-hex}) is required",
			Cause:     &domain.RunIdentityError{Problem: problem, RunID: v},
		}
	}
	return v, nil
}

// parseEnabledDisabled reads an "enabled"/"disabled" value for the named key.
func parseEnabledDisabled(key, v string) (bool, error) {
	switch v {
	case "enabled":
		return true, nil
	case "disabled":
		return false, nil
	}
	return false, &domain.RefusalError{
		Component: "artifact",
		Reason:    "invalid '" + key + "' value " + `"` + v + `"` + "; valid values: enabled, disabled",
	}
}

// parseRunnerSettings reads runner_mode, runner_pre_consultation and
// runner_manual_resolution. A legacy alias (mode, pre_consultation,
// manual_resolution) is read only when its prefixed key is absent. The three
// settings must be all present or all absent after alias resolution.
func parseRunnerSettings(topLevel map[string]string, rs *domain.RunSettings) error {
	lookup := func(prefixed, legacy string) (key, value string, ok bool) {
		if v, found := topLevel[prefixed]; found {
			return prefixed, v, true
		}
		if v, found := topLevel[legacy]; found {
			return legacy, v, true
		}
		return "", "", false
	}

	modeKey, modeVal, hasMode := lookup("runner_mode", "mode")
	preKey, preVal, hasPre := lookup("runner_pre_consultation", "pre_consultation")
	manKey, manVal, hasMan := lookup("runner_manual_resolution", "manual_resolution")

	if !hasMode && !hasPre && !hasMan {
		return nil
	}

	var mode domain.ExecutionMode
	if hasMode {
		var err error
		mode, err = domain.ParseExecutionMode(modeVal)
		if err != nil || mode == domain.ExecutionModeUnset {
			reason := "empty value"
			if err != nil {
				reason = err.Error()
			}
			return &domain.RefusalError{
				Component: "artifact",
				Reason:    "invalid '" + modeKey + "' value " + `"` + modeVal + `"` + ": " + reason,
			}
		}
	}
	var pre, man bool
	var err error
	if hasPre {
		if pre, err = parseEnabledDisabled(preKey, preVal); err != nil {
			return err
		}
	}
	if hasMan {
		if man, err = parseEnabledDisabled(manKey, manVal); err != nil {
			return err
		}
	}
	if !(hasMode && hasPre && hasMan) {
		return &domain.RefusalError{
			Component: "artifact",
			Reason:    "runner_mode, runner_pre_consultation and runner_manual_resolution must be all present or all absent",
		}
	}
	rs.Mode = mode
	rs.PreConsultation = pre
	rs.ManualResolution = man
	return nil
}

// parseInfrastructureSelections reads the infrastructure_selections block or
// flow mapping. Returns nil when the key is absent or holds no entries.
func parseInfrastructureSelections(entries []domain.FrontmatterEntry) (map[string]string, error) {
	refuse := func(reason string) (map[string]string, error) {
		return nil, &domain.RefusalError{Component: "artifact", Reason: "invalid 'infrastructure_selections': " + reason}
	}
	for _, e := range entries {
		if e.Key != "infrastructure_selections" {
			continue
		}
		var pairs []string
		if _, inline := parseYAMLLine(e.Lines[0]); inline != "" {
			flow := strings.TrimSpace(inline)
			if !strings.HasPrefix(flow, "{") || !strings.HasSuffix(flow, "}") {
				return refuse("expected a mapping, got " + `"` + inline + `"`)
			}
			for _, p := range strings.Split(flow[1:len(flow)-1], ",") {
				if strings.TrimSpace(p) != "" {
					pairs = append(pairs, strings.TrimSpace(p))
				}
			}
		} else {
			for _, l := range e.Lines[1:] {
				pairs = append(pairs, strings.TrimSpace(l))
			}
		}
		if len(pairs) == 0 {
			return nil, nil
		}
		selections := make(map[string]string, len(pairs))
		for _, p := range pairs {
			class, agent := parseYAMLLine(p)
			if class == "" {
				return refuse("cannot read entry " + `"` + p + `"`)
			}
			if !domain.IsGatedInfraClass(class) {
				return refuse("class " + `"` + class + `"` + " is not a gated class (checkpoint, commit, restore)")
			}
			if agent == "" {
				return refuse("class " + `"` + class + `"` + " has no agent name")
			}
			selections[class] = agent
		}
		return selections, nil
	}
	return nil, nil
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
