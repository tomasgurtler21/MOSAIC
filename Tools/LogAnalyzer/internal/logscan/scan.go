package logscan

import (
	"os"
	"path/filepath"
	"sort"

	"mosaic-log-analyzer/internal/domain"
)

// LogsDirName is the conventional logs-root folder name.
const LogsDirName = "OrchestrationLogs"

// OrchestratorEventFile is the orchestrator stream's file name within a run folder.
const OrchestratorEventFile = "00_orchestrator_events.jsonl"

// AgentEventFile is the event stream file name within an agent-instance folder.
const AgentEventFile = "03_events.jsonl"

// Scanner discovers log data from directory structure alone. It never reads file
// contents and never consults Orchestration-{run_id}/ artifact folders.
type Scanner struct{}

// NewScanner returns a filesystem-backed LogSource.
func NewScanner() *Scanner { return &Scanner{} }

// Default probes for OrchestrationLogs/ beneath workDir. An absent folder
// yields Source{Kind: SourceNotFound}.
func (s *Scanner) Default(workDir string) domain.Source {
	candidate := filepath.Join(workDir, LogsDirName)
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return domain.Source{Kind: domain.SourceNotFound}
	}
	// The conventional OrchestrationLogs/ folder is a logs root by definition.
	return domain.Source{Kind: domain.SourceLogsRoot, Path: candidate}
}

// Classify decides whether path is a logs root, a single run folder, or
// unusable-with-reason. A non-existent path yields SourceNotFound.
func (s *Scanner) Classify(path string) domain.Source {
	info, err := os.Stat(path)
	if err != nil {
		return domain.Source{Kind: domain.SourceNotFound, Path: path}
	}

	// A plain file (not a directory) is unusable.
	if !info.IsDir() {
		return domain.Source{
			Kind:   domain.SourceUnusable,
			Path:   path,
			Reason: "path is a file, not a directory",
		}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return domain.Source{
			Kind:   domain.SourceUnusable,
			Path:   path,
			Reason: "cannot read directory: " + err.Error(),
		}
	}

	// Check if this is a logs root: contains run-id-shaped subdirectory or unknown-run/.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == domain.UnknownRunDirName || domain.IsRunID(name) {
			return domain.Source{Kind: domain.SourceLogsRoot, Path: path}
		}
	}

	// Check if this is a single run folder:
	// Contains 00_orchestrator_events.jsonl and/or agent-instance folders holding 03_events.jsonl.
	// Also handles the case where the path is itself named unknown-run with run-folder structure.
	if hasRunFolderStructure(path, entries) {
		return domain.Source{Kind: domain.SourceSingleRun, Path: path}
	}

	return domain.Source{
		Kind:   domain.SourceUnusable,
		Path:   path,
		Reason: "directory has no recognised log source structure",
	}
}

// hasRunFolderStructure reports whether a directory looks like a single run folder:
// it contains 00_orchestrator_events.jsonl and/or subdirectories holding 03_events.jsonl.
func hasRunFolderStructure(dir string, entries []os.DirEntry) bool {
	for _, e := range entries {
		if !e.IsDir() {
			if e.Name() == OrchestratorEventFile {
				return true
			}
			continue
		}
		// Check if the subdirectory contains an agent event file.
		agentEventPath := filepath.Join(dir, e.Name(), AgentEventFile)
		if _, err := os.Stat(agentEventPath); err == nil {
			return true
		}
	}
	return false
}

// Enumerate lists runs, the unattributable bucket, agent-instance folders and
// their event files for a usable source. A non-usable source yields an empty
// Inventory carrying that Source. No file contents are read.
func (s *Scanner) Enumerate(src domain.Source) (domain.Inventory, []domain.Finding) {
	if !src.IsUsable() {
		return domain.Inventory{Source: src}, nil
	}

	switch src.Kind {
	case domain.SourceLogsRoot:
		return enumerateLogsRoot(src)
	case domain.SourceSingleRun:
		return enumerateSingleRun(src)
	default:
		return domain.Inventory{Source: src}, nil
	}
}

// enumerateLogsRoot enumerates a logs root directory.
func enumerateLogsRoot(src domain.Source) (domain.Inventory, []domain.Finding) {
	entries, err := os.ReadDir(src.Path)
	if err != nil {
		return domain.Inventory{Source: src}, []domain.Finding{{
			Kind:     domain.FindingUnreadableFile,
			Severity: domain.SeverityWarning,
			Path:     src.Path,
			Detail:   "cannot read logs root: " + err.Error(),
		}}
	}

	inv := domain.Inventory{Source: src}
	var findings []domain.Finding

	for _, e := range entries {
		// Skip plain files silently.
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dirPath := filepath.Join(src.Path, name)

		if name == domain.UnknownRunDirName {
			runRef := domain.UnattributableRun()
			entry, entryFindings := enumerateRunDir(runRef, dirPath)
			inv.Unattributable = &entry
			findings = append(findings, entryFindings...)
		} else if domain.IsRunID(name) {
			runRef := domain.NamedRun(name)
			entry, entryFindings := enumerateRunDir(runRef, dirPath)
			inv.Runs = append(inv.Runs, entry)
			findings = append(findings, entryFindings...)
		} else {
			// Unrecognised subdirectory: report as a finding.
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingUnrecognisedFolder,
				Severity: domain.SeverityWarning,
				Path:     dirPath,
				Detail:   "folder name is not a valid run id and is not unknown-run",
			})
		}
	}

	// Sort named runs by run id ascending.
	sort.Slice(inv.Runs, func(i, j int) bool {
		return inv.Runs[i].Run.ID < inv.Runs[j].Run.ID
	})

	return inv, findings
}

// enumerateSingleRun enumerates a single run directory.
func enumerateSingleRun(src domain.Source) (domain.Inventory, []domain.Finding) {
	// Determine the run reference from the directory name.
	name := filepath.Base(src.Path)
	var runRef domain.RunRef
	if name == domain.UnknownRunDirName {
		runRef = domain.UnattributableRun()
	} else if domain.IsRunID(name) {
		runRef = domain.NamedRun(name)
	} else {
		// The directory is a run folder but name doesn't match — treat as unattributable hint.
		runRef = domain.UnattributableRun()
	}

	entry, findings := enumerateRunDir(runRef, src.Path)
	inv := domain.Inventory{Source: src}
	if runRef.IsUnattributable() {
		inv.Unattributable = &entry
	} else {
		inv.Runs = append(inv.Runs, entry)
	}
	return inv, findings
}

// enumerateRunDir enumerates the contents of a run directory (orchestrator file
// and agent-instance folders). It never reads file contents.
func enumerateRunDir(run domain.RunRef, dir string) (domain.RunEntry, []domain.Finding) {
	entry := domain.RunEntry{
		Run: run,
		Dir: dir,
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return entry, []domain.Finding{{
			Kind:     domain.FindingUnreadableFile,
			Severity: domain.SeverityWarning,
			Path:     dir,
			Detail:   "cannot read run directory: " + err.Error(),
		}}
	}

	var findings []domain.Finding

	for _, e := range entries {
		if !e.IsDir() {
			// Check for the orchestrator event file.
			if e.Name() == OrchestratorEventFile {
				entry.OrchestratorFile = filepath.Join(dir, e.Name())
			}
			// Other plain files are silently ignored.
			continue
		}

		// It's a subdirectory: treat as an agent-instance folder.
		agentDir := filepath.Join(dir, e.Name())
		agentEntry := domain.AgentEntry{
			Dir:          agentDir,
			InstanceHint: InstanceHintFromDir(e.Name()),
		}

		// Check for the agent event file by path existence.
		agentEventPath := filepath.Join(agentDir, AgentEventFile)
		if _, err := os.Stat(agentEventPath); err == nil {
			agentEntry.EventFile = agentEventPath
		} else {
			// Missing event file: report a finding.
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingMissingEventFile,
				Severity: domain.SeverityWarning,
				Path:     agentDir,
				Detail:   "agent-instance folder contains no " + AgentEventFile,
			})
		}

		entry.Agents = append(entry.Agents, agentEntry)
	}

	// Sort agents by Dir ascending.
	sort.Slice(entry.Agents, func(i, j int) bool {
		return entry.Agents[i].Dir < entry.Agents[j].Dir
	})

	return entry, findings
}

// InstanceHintFromDir maps a sanitized agent-instance folder name back to an
// AgentInstanceID. The result is advisory only; the agent_instance_id on the
// events is authoritative.
func InstanceHintFromDir(name string) domain.AgentInstanceID {
	return domain.AgentInstanceID(name)
}
