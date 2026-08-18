// Package artifact implements the ArtifactStore port for Orchestration.md.
// It reads, constructs, and atomically rewrites Orchestration.md in the canonical
// format defined in Development/Designs/OrchestrationArtifactFormat.md.
//
// Parsing and rendering are exposed as package-level functions so that tests can
// verify each step independently of file I/O.
package artifact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mosaic-common/docformat"
	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// NewFileStore creates a new file-based ArtifactStore that reads from and
// writes to the Orchestration.md file at the given path.
//
// Writes are atomic: the implementation uses write-to-temp-then-rename (FR-34).
func NewFileStore(path string) domain.ArtifactStore {
	return &fileStore{path: path}
}

// IsRunScopedArtifactPath reports whether path's parent directory is a
// run-scoped orchestration folder, i.e. named "Orchestration-{run_id}" where
// run_id satisfies domain.IsValidRunID.
//
// Reports false for a relative path, for a parent that is not an
// Orchestration-* folder, and for an Orchestration-* folder whose suffix is not
// a canonical run_id.
//
// This is a predicate only. Non-run-scoped absolute paths remain legal for
// Create (many existing tests build stores under t.TempDir()); the predicate
// exists so the condition can be logged without changing behaviour.
//
// Its sole production consumer is newLoggedArtifactStore in cmd/mosaic-run,
// which uses it to decide whether to emit EventArtifactPathNonRunScoped. The
// artifact package itself never logs and never takes a DebugLogger.
func IsRunScopedArtifactPath(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	parentDir := filepath.Base(filepath.Dir(path))
	runID, ok := domain.ParseRunFolder(parentDir)
	if !ok {
		return false
	}
	return domain.IsValidRunID(runID)
}

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
	// Trim the trailing newline from tag bytes before searching: files checked out
	// on Windows may have CRLF line endings, so searching for a tag with a bare LF
	// suffix would fail to match. The tag content itself (without the newline) is
	// sufficient to confirm the section is present.
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

	// Build ArtifactState from frontmatter.
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
	} else {
		// Absent key defaults to mosaic-owned (the recommended variant).
		state.CommitBranchVariant = domain.CommitBranchMOSAICOwned
	}
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
	overrides := parseInfrastructureOverrides(string(fmContent))
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

// Render serialises an ArtifactState to the canonical markdown bytes.
//
// Stage and Checkpoint fields that are "" in the ArtifactState are rendered as
// "-" in the execution log table. No other component ever sees or produces "-".
//
// Column widths in the output tables are at least as wide as the header and each
// cell value. Round-tripping a parsed file through Render produces identical bytes
// when the column widths in the original separator row were already >= all cell
// values.
func Render(state domain.ArtifactState) ([]byte, error) {
	var buf bytes.Buffer

	// --- Frontmatter ---
	buf.WriteString("---\n")
	buf.WriteString("type: orchestration-artifact\n")
	if state.RunID != "" {
		buf.WriteString("run_id: " + state.RunID + "\n")
	}
	buf.WriteString("workflow: " + string(state.Workflow) + "\n")
	buf.WriteString("workflow_version: \"" + string(state.WorkflowVersion) + "\"\n")
	buf.WriteString("task: \"" + state.Task + "\"\n")
	buf.WriteString("started: " + state.Started.UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("last_updated: " + state.LastUpdated.UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("global_sequence: " + strconv.Itoa(state.GlobalSequence) + "\n")
	if state.Mode != "" {
		buf.WriteString("mode: " + string(state.Mode) + "\n")
	}
	if state.Checkpoints {
		buf.WriteString("checkpoints: enabled\n")
	} else {
		buf.WriteString("checkpoints: disabled\n")
	}
	if state.Commits {
		buf.WriteString("commits: enabled\n")
	} else {
		buf.WriteString("commits: disabled\n")
	}
	commitVariant := state.CommitBranchVariant
	if commitVariant == "" {
		commitVariant = domain.CommitBranchMOSAICOwned
	}
	buf.WriteString("commit_branch_variant: " + string(commitVariant) + "\n")
	if state.CommitBranch != "" {
		buf.WriteString("commit_branch: " + state.CommitBranch + "\n")
	}
	if state.PreConsultation {
		buf.WriteString("pre_consultation: enabled\n")
	} else {
		buf.WriteString("pre_consultation: disabled\n")
	}
	if state.ManualResolution {
		buf.WriteString("manual_resolution: enabled\n")
	} else {
		buf.WriteString("manual_resolution: disabled\n")
	}
	if len(state.InfrastructureOverrides) > 0 {
		buf.WriteString("infrastructure_overrides:\n")
		for _, ov := range state.InfrastructureOverrides {
			buf.WriteString("  " + ov.AgentName + ":\n")
			buf.WriteString("    triggers:\n")
			for _, tr := range ov.Triggers {
				buf.WriteString("      - trigger: " + tr.Trigger + "\n")
				if tr.Param != "" {
					buf.WriteString("        trigger_param: " + tr.Param + "\n")
				}
			}
		}
	}
	buf.WriteString("current_state:\n")
	cs := state.CurrentState
	buf.WriteString("  phase: " + renderNullable(cs.Phase) + "\n")
	buf.WriteString("  stage: " + renderNullable(cs.Stage) + "\n")
	buf.WriteString("  last_status: " + renderNullable(string(cs.LastStatus)) + "\n")
	buf.WriteString("  last_agent: " + renderNullableQuoted(cs.LastAgent) + "\n")
	buf.WriteString("  error_code: " + renderNullable(string(cs.ErrorCode)) + "\n")
	buf.WriteString("---\n")

	// --- Body ---
	buf.WriteString("\n")

	// ExecutionLog section
	execLogOpen, err := docformat.RenderOpenTagLine(docformat.NodeSection, "ExecutionLog", "")
	if err != nil {
		return nil, fmt.Errorf("artifact: render ExecutionLog open tag: %w", err)
	}
	execLogClose, err := docformat.RenderCloseTagLine("ExecutionLog")
	if err != nil {
		return nil, fmt.Errorf("artifact: render ExecutionLog close tag: %w", err)
	}
	buf.Write(execLogOpen)
	buf.Write(renderExecutionLog(state.ExecutionLog))
	buf.Write(execLogClose)

	buf.WriteString("\n")

	// Artifacts section
	artifactsOpen, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Artifacts", "")
	if err != nil {
		return nil, fmt.Errorf("artifact: render Artifacts open tag: %w", err)
	}
	artifactsClose, err := docformat.RenderCloseTagLine("Artifacts")
	if err != nil {
		return nil, fmt.Errorf("artifact: render Artifacts close tag: %w", err)
	}
	buf.Write(artifactsOpen)
	buf.Write(renderArtifactRegistry(state.ArtifactRegistry))
	buf.Write(artifactsClose)

	buf.WriteString("\n")

	// WorkflowNotes section
	workflowNotesOpen, err := docformat.RenderOpenTagLine(docformat.NodeSection, "WorkflowNotes", "")
	if err != nil {
		return nil, fmt.Errorf("artifact: render WorkflowNotes open tag: %w", err)
	}
	workflowNotesClose, err := docformat.RenderCloseTagLine("WorkflowNotes")
	if err != nil {
		return nil, fmt.Errorf("artifact: render WorkflowNotes close tag: %w", err)
	}
	buf.Write(workflowNotesOpen)
	buf.Write(renderWorkflowNotes(state.WorkflowNotes))
	buf.Write(workflowNotesClose)

	return buf.Bytes(), nil
}

// TruncateSummary applies the head-50 + tail-50 truncation rule from the artifact
// format spec to the given summary string.
//
// Messages of 100 characters or fewer are returned unchanged.
// Messages longer than 100 characters are truncated: the first 50 and last 50
// characters are kept, joined by " … " (space, ellipsis, space).
//
// Pipe characters ("|") and newlines are stripped from the result because they
// are invalid inside a markdown table cell. Stripping is applied before truncation
// so that the 50/50 split is counted on the already-clean string.
func TruncateSummary(s string) string {
	// Strip pipe characters and newlines.
	clean := strings.NewReplacer("|", "", "\n", "", "\r", "").Replace(s)

	if len(clean) <= 100 {
		return clean
	}

	head := clean[:50]
	tail := clean[len(clean)-50:]
	return head + " … " + tail
}

// --- fileStore ---

type fileStore struct {
	path string
}

func (f *fileStore) Read(ctx context.Context) (domain.ArtifactState, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ArtifactState{}, os.ErrNotExist
		}
		return domain.ArtifactState{}, err
	}
	state, err := Parse(data)
	if err != nil {
		// Set the Resource to the file path in RefusalErrors.
		var re *domain.RefusalError
		if errors.As(err, &re) {
			re.Resource = f.path
		}
		return domain.ArtifactState{}, err
	}
	return state, nil
}

func (f *fileStore) Create(ctx context.Context, info domain.WorkflowInfo, task string, settings domain.RunSettings, now time.Time, runID string) (domain.ArtifactState, error) {
	// Reject a non-absolute store path before any filesystem side effects.
	// A relative path would land in the process CWD, never in the intended
	// run-scoped folder. Absolute non-run-scoped paths are permitted (many
	// tests build stores under t.TempDir()).
	if !filepath.IsAbs(f.path) {
		return domain.ArtifactState{}, fmt.Errorf("artifact store path must be absolute, got %q", f.path)
	}

	// Fail if file already exists.
	if _, err := os.Stat(f.path); err == nil {
		return domain.ArtifactState{}, fmt.Errorf("artifact: file already exists at %s", f.path)
	}

	state := domain.ArtifactState{
		Type:            "orchestration-artifact",
		RunID:           runID,
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now.UTC(),
		LastUpdated:     now.UTC(),
		GlobalSequence:  0,
		RunSettings:     settings,
	}

	data, err := Render(state)
	if err != nil {
		return domain.ArtifactState{}, err
	}

	// Ensure the run-scoped folder exists before writing. The folder
	// (e.g. Orchestration-{run_id}/) is never created by the caller;
	// Create is responsible for initialising the entire run directory.
	if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
		return domain.ArtifactState{}, err
	}

	if err := os.WriteFile(f.path, data, 0644); err != nil {
		return domain.ArtifactState{}, err
	}

	return state, nil
}

func (f *fileStore) Apply(ctx context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	// Build the new execution log entry.
	newEntry := domain.ExecutionLogEntry{
		Seq:        step.Seq,
		Agent:      step.AgentInstance,
		Phase:      step.Phase,
		Stage:      step.Stage,
		Status:     step.Status,
		Timestamp:  step.Timestamp,
		Summary:    TruncateSummary(step.Summary),
		Inputs:     step.Inputs,
		Checkpoint: step.Checkpoint,
	}

	// Build the new state.
	newState := state
	newState.ExecutionLog = append(append([]domain.ExecutionLogEntry(nil), state.ExecutionLog...), newEntry)
	newState.GlobalSequence = state.GlobalSequence + 1
	newState.LastUpdated = step.Timestamp

	// current_state is updated only for workflow steps. An infrastructure
	// step leaves phase, stage, last_status, last_agent, and error_code
	// exactly as they were, on disk as well as in the returned state, so the
	// recorded workflow position continues to name the last workflow step.
	// The invocation is still fully recorded above and below: the execution
	// log entry, sequence bump, and artifact registry upsert all apply to an
	// infrastructure step unchanged.
	if !step.IsInfrastructure {
		newState.CurrentState = domain.CurrentState{
			Phase:      step.Phase,
			Stage:      step.Stage,
			LastStatus: step.Status,
			LastAgent:  step.AgentInstance,
			ErrorCode:  step.ErrorCode,
		}
	}

	// Upsert artifact registry entries.
	registry := append([]domain.ArtifactRegistryEntry(nil), state.ArtifactRegistry...)
	for _, art := range step.OutputArtifacts {
		createdIn := step.Phase
		if step.Stage != "" {
			createdIn = step.Phase + "." + step.Stage
		}
		entry := domain.ArtifactRegistryEntry{
			Artifact:  art,
			CreatedIn: createdIn,
			CreatedBy: step.AgentInstance,
		}
		registry = upsertRegistry(registry, entry)
	}
	newState.ArtifactRegistry = registry

	// Render and write atomically.
	data, err := Render(newState)
	if err != nil {
		return domain.ArtifactState{}, err
	}
	if err := atomicWrite(f.path, data); err != nil {
		return domain.ArtifactState{}, err
	}

	return newState, nil
}

// --- internal helpers ---

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
	fmContent = string(rest[:idx+1]) // include the \n before the closing ---
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

// extractSectionContent extracts the bytes between a section's open and close tags.
// Returns (content, true) on success, (nil, false) if the section is not found.
func extractSectionContent(body []byte, sectionName string) ([]byte, bool) {
	openTag, err := docformat.RenderOpenTagLine(docformat.NodeSection, sectionName, "")
	if err != nil {
		return nil, false
	}
	closeTag, err := docformat.RenderCloseTagLine(sectionName)
	if err != nil {
		return nil, false
	}

	openIdx := bytes.Index(body, openTag)
	if openIdx < 0 {
		return nil, false
	}

	contentStart := openIdx + len(openTag)
	remaining := body[contentStart:]

	closeIdx := bytes.Index(remaining, closeTag)
	if closeIdx < 0 {
		return nil, false
	}

	return remaining[:closeIdx], true
}

// parseExecutionLog parses the execution log table from section content.
func parseExecutionLog(content []byte) ([]domain.ExecutionLogEntry, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.Parse(content)
	if err != nil {
		return nil, err
	}
	if len(t.Rows) == 0 {
		return nil, nil
	}

	seqCol := t.Column("Seq")
	agentCol := t.Column("Agent")
	phaseCol := t.Column("Phase")
	stageCol := t.Column("Stage")
	statusCol := t.Column("Status")
	tsCol := t.Column("Timestamp")
	summaryCol := t.Column("Summary")
	inputsCol := t.Column("Inputs")
	checkpointCol := t.Column("Checkpoint")

	var entries []domain.ExecutionLogEntry
	for _, row := range t.Rows {
		entry := domain.ExecutionLogEntry{}

		if seqCol >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(row[seqCol]))
			if err == nil {
				entry.Seq = n
			}
		}
		if agentCol >= 0 {
			entry.Agent = strings.TrimSpace(row[agentCol])
		}
		if phaseCol >= 0 {
			entry.Phase = strings.TrimSpace(row[phaseCol])
		}
		if stageCol >= 0 {
			v := strings.TrimSpace(row[stageCol])
			if v == "-" {
				v = ""
			}
			entry.Stage = v
		}
		if statusCol >= 0 {
			entry.Status = domain.StatusCode(strings.TrimSpace(row[statusCol]))
		}
		if tsCol >= 0 {
			ts, err := time.Parse(time.RFC3339, strings.TrimSpace(row[tsCol]))
			if err == nil {
				entry.Timestamp = ts
			}
		}
		if summaryCol >= 0 {
			entry.Summary = strings.TrimSpace(row[summaryCol])
		}
		if inputsCol >= 0 {
			v := strings.TrimSpace(row[inputsCol])
			if v == "-" {
				v = ""
			}
			entry.Inputs = v
		}
		if checkpointCol >= 0 {
			v := strings.TrimSpace(row[checkpointCol])
			if v == "-" {
				v = ""
			}
			entry.Checkpoint = v
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

// parseArtifactRegistry parses the artifact registry table from section content.
func parseArtifactRegistry(content []byte) ([]domain.ArtifactRegistryEntry, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.Parse(content)
	if err != nil {
		return nil, err
	}
	if len(t.Rows) == 0 {
		return nil, nil
	}

	artCol := t.Column("Artifact")
	createdInCol := t.Column("Created In")
	createdByCol := t.Column("Created By")

	var entries []domain.ArtifactRegistryEntry
	for _, row := range t.Rows {
		entry := domain.ArtifactRegistryEntry{}
		if artCol >= 0 {
			entry.Artifact = strings.TrimSpace(row[artCol])
		}
		if createdInCol >= 0 {
			entry.CreatedIn = strings.TrimSpace(row[createdInCol])
		}
		if createdByCol >= 0 {
			entry.CreatedBy = strings.TrimSpace(row[createdByCol])
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseWorkflowNotes parses the workflow notes table from section content.
func parseWorkflowNotes(content []byte) ([]domain.WorkflowNote, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.Parse(content)
	if err != nil {
		return nil, err
	}
	if len(t.Rows) == 0 {
		return nil, nil
	}

	seqCol := t.Column("Seq")
	noteCol := t.Column("Note")

	var notes []domain.WorkflowNote
	for _, row := range t.Rows {
		note := domain.WorkflowNote{}
		if seqCol >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(row[seqCol]))
			if err == nil {
				note.Seq = n
			}
		}
		if noteCol >= 0 {
			note.Note = strings.TrimSpace(row[noteCol])
		}
		notes = append(notes, note)
	}
	return notes, nil
}

// renderExecutionLog renders the execution log entries as a markdown table.
func renderExecutionLog(entries []domain.ExecutionLogEntry) []byte {
	headers := []string{"Seq", "Agent", "Phase", "Stage", "Status", "Timestamp", "Summary", "Inputs", "Checkpoint"}
	t := mdtable.Table{Header: headers}

	for _, e := range entries {
		stage := e.Stage
		if stage == "" {
			stage = "-"
		}
		inputs := e.Inputs
		if inputs == "" {
			inputs = "-"
		}
		checkpoint := e.Checkpoint
		if checkpoint == "" {
			checkpoint = "-"
		}
		row := []string{
			strconv.Itoa(e.Seq),
			e.Agent,
			e.Phase,
			stage,
			string(e.Status),
			e.Timestamp.UTC().Format(time.RFC3339),
			e.Summary,
			inputs,
			checkpoint,
		}
		t = t.AppendRow(row)
	}

	return t.Render()
}

// renderArtifactRegistry renders the artifact registry entries as a markdown table.
func renderArtifactRegistry(entries []domain.ArtifactRegistryEntry) []byte {
	headers := []string{"Artifact", "Created In", "Created By"}
	t := mdtable.Table{Header: headers}

	for _, e := range entries {
		row := []string{e.Artifact, e.CreatedIn, e.CreatedBy}
		t = t.AppendRow(row)
	}

	return t.Render()
}

// renderWorkflowNotes renders the workflow notes as a markdown table.
func renderWorkflowNotes(notes []domain.WorkflowNote) []byte {
	headers := []string{"Seq", "Note"}
	t := mdtable.Table{Header: headers}

	for _, n := range notes {
		row := []string{strconv.Itoa(n.Seq), n.Note}
		t = t.AppendRow(row)
	}

	return t.Render()
}

// renderNullable renders a string value, replacing "" with "null".
func renderNullable(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

// renderNullableQuoted renders a string value as double-quoted YAML, replacing "" with "null".
func renderNullableQuoted(s string) string {
	if s == "" {
		return "null"
	}
	return `"` + s + `"`
}

// upsertRegistry upserts an artifact registry entry: updates an existing entry
// with the same Artifact path, or appends a new one.
func upsertRegistry(registry []domain.ArtifactRegistryEntry, entry domain.ArtifactRegistryEntry) []domain.ArtifactRegistryEntry {
	for i, e := range registry {
		if e.Artifact == entry.Artifact {
			registry[i] = entry
			return registry
		}
	}
	return append(registry, entry)
}

// atomicWrite writes data to path atomically using write-to-temp-then-rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-orchestration-*.md")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmpName, path)
}
