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

// Parse parses the canonical orchestration artifact format from the given bytes
// and returns the in-memory ArtifactState.
//
// Returns *domain.RefusalError if the content is not in the canonical format
// (missing type field, missing required sections, truncated file, or old template
// format without [[SECTION:...]] boundary tags).
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
	if !bytes.Contains(data, []byte("[[SECTION:ExecutionLog]]")) {
		return refuse("missing [[SECTION:ExecutionLog]] section")
	}
	if !bytes.Contains(data, []byte("[[/SECTION:ExecutionLog]]")) {
		return refuse("file appears truncated: missing [[/SECTION:ExecutionLog]] closing tag")
	}
	if !bytes.Contains(data, []byte("[[SECTION:Artifacts]]")) {
		return refuse("missing [[SECTION:Artifacts]] section")
	}
	if !bytes.Contains(data, []byte("[[/SECTION:Artifacts]]")) {
		return refuse("file appears truncated: missing [[/SECTION:Artifacts]] closing tag")
	}

	// Build ArtifactState from frontmatter.
	state := domain.ArtifactState{
		Type: "orchestration-artifact",
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
	if v, ok := topLevel["checkpoints"]; ok {
		state.Checkpoints = (v == "enabled")
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
		return refuse("[[SECTION:ExecutionLog]] content could not be extracted")
	}
	logEntries, err := parseExecutionLog(execContent)
	if err != nil {
		return refuse("failed to parse execution log: " + err.Error())
	}
	state.ExecutionLog = logEntries

	artsContent, ok := extractSectionContent(body, "Artifacts")
	if !ok {
		return refuse("[[SECTION:Artifacts]] content could not be extracted")
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
	buf.WriteString("workflow: " + string(state.Workflow) + "\n")
	buf.WriteString("workflow_version: \"" + string(state.WorkflowVersion) + "\"\n")
	buf.WriteString("task: \"" + state.Task + "\"\n")
	buf.WriteString("started: " + state.Started.UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("last_updated: " + state.LastUpdated.UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("global_sequence: " + strconv.Itoa(state.GlobalSequence) + "\n")
	if state.Checkpoints {
		buf.WriteString("checkpoints: enabled\n")
	} else {
		buf.WriteString("checkpoints: disabled\n")
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
	buf.WriteString("[[SECTION:ExecutionLog]]\n")
	buf.Write(renderExecutionLog(state.ExecutionLog))
	buf.WriteString("[[/SECTION:ExecutionLog]]\n")

	buf.WriteString("\n")

	// Artifacts section
	buf.WriteString("[[SECTION:Artifacts]]\n")
	buf.Write(renderArtifactRegistry(state.ArtifactRegistry))
	buf.WriteString("[[/SECTION:Artifacts]]\n")

	buf.WriteString("\n")

	// WorkflowNotes section
	buf.WriteString("[[SECTION:WorkflowNotes]]\n")
	buf.Write(renderWorkflowNotes(state.WorkflowNotes))
	buf.WriteString("[[/SECTION:WorkflowNotes]]\n")

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

func (f *fileStore) Create(ctx context.Context, info domain.WorkflowInfo, task string, checkpoints bool, now time.Time) (domain.ArtifactState, error) {
	// Fail if file already exists.
	if _, err := os.Stat(f.path); err == nil {
		return domain.ArtifactState{}, fmt.Errorf("artifact: file already exists at %s", f.path)
	}

	state := domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now.UTC(),
		LastUpdated:     now.UTC(),
		GlobalSequence:  0,
		Checkpoints:     checkpoints,
	}

	data, err := Render(state)
	if err != nil {
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
		Checkpoint: step.Checkpoint,
	}

	// Build the new state.
	newState := state
	newState.ExecutionLog = append(append([]domain.ExecutionLogEntry(nil), state.ExecutionLog...), newEntry)
	newState.GlobalSequence = state.GlobalSequence + 1
	newState.LastUpdated = step.Timestamp

	newState.CurrentState = domain.CurrentState{
		Phase:      step.Phase,
		Stage:      step.Stage,
		LastStatus: step.Status,
		LastAgent:  step.AgentInstance,
		ErrorCode:  step.ErrorCode,
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
func splitDocument(data []byte) (fmContent string, bodyContent string, hasFM bool) {
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
// pairs and the nested current_state pairs. This minimal parser handles only the
// subset used by orchestration artifacts.
func parseFrontmatter(content string) (topLevel map[string]string, currentState map[string]string, err error) {
	topLevel = make(map[string]string)
	currentState = make(map[string]string)

	lines := strings.Split(content, "\n")
	inCurrentState := false

	for _, line := range lines {
		if strings.TrimRight(line, "\r") == "" {
			continue
		}

		// Detect the current_state block header.
		if strings.TrimRight(line, "\r") == "current_state:" {
			inCurrentState = true
			continue
		}

		if inCurrentState {
			if strings.HasPrefix(line, "  ") {
				// Nested current_state key.
				trimmed := line[2:]
				key, value := parseYAMLLine(trimmed)
				if key != "" {
					currentState[key] = value
				}
				continue
			}
			// No longer in nested block.
			inCurrentState = false
		}

		key, value := parseYAMLLine(line)
		if key != "" {
			topLevel[key] = value
		}
	}

	return topLevel, currentState, nil
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

// extractSectionContent extracts the bytes between [[SECTION:name]] and [[/SECTION:name]].
// Returns (content, true) on success, (nil, false) if the section is not found.
func extractSectionContent(body []byte, sectionName string) ([]byte, bool) {
	openTag := []byte("[[SECTION:" + sectionName + "]]\n")
	closeTag := []byte("[[/SECTION:" + sectionName + "]]")

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
	headers := []string{"Seq", "Agent", "Phase", "Stage", "Status", "Timestamp", "Summary", "Checkpoint"}
	t := mdtable.Table{Header: headers}

	for _, e := range entries {
		stage := e.Stage
		if stage == "" {
			stage = "-"
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
