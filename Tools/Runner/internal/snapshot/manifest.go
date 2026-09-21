package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mosaic-run/internal/domain"
)

// Manifest is the recovery manifest written to the backup directory before
// in-place transforms are applied. It records which files were (or would be)
// transformed so that RestoreFromBackup knows which files to copy back.
type Manifest struct {
	Timestamp time.Time       `json:"timestamp"`  // UTC, diagnostic only
	AgentsDir string          `json:"agents_dir"` // absolute path, for manual recovery
	Files     []ManifestEntry `json:"files"`      // files that were transformed
}

// ManifestEntry records one field-level transformation applied to a file.
//
// Cardinality: one entry per (filename, field) pair. A file with two
// transformed fields produces two entries. A file matched by rules but
// whose content is unchanged produces zero entries.
//
// Ordering: entries are ordered by filename (lexicographic), then by field
// name (lexicographic) within the same filename. This deterministic order
// enables byte-for-byte comparison in tests.
type ManifestEntry struct {
	Filename      string `json:"filename"`       // base name (e.g. "orchestrator-script.md")
	Field         string `json:"field"`          // frontmatter field (e.g. "mode")
	OriginalValue string `json:"original_value"` // diagnostic only (restore uses whole-file copy)
}

// ManifestErrorKind distinguishes missing from corrupt manifest errors so
// callers can handle each case differently.
type ManifestErrorKind int

const (
	// ManifestMissing indicates the manifest file does not exist. This means
	// a partial backup (crashed before WriteManifest completed).
	ManifestMissing ManifestErrorKind = iota + 1

	// ManifestCorrupt indicates the manifest file exists but cannot be parsed.
	// The backup should be left intact for manual inspection.
	ManifestCorrupt
)

// ManifestError is the structured error returned by ReadManifest.
type ManifestError struct {
	Kind    ManifestErrorKind
	Path    string // path to the manifest file
	Wrapped error  // underlying error (nil for missing)
}

func (e *ManifestError) Error() string {
	switch e.Kind {
	case ManifestMissing:
		return fmt.Sprintf("manifest missing: %s", e.Path)
	case ManifestCorrupt:
		if e.Wrapped != nil {
			return fmt.Sprintf("manifest corrupt: %s: %v", e.Path, e.Wrapped)
		}
		return fmt.Sprintf("manifest corrupt: %s", e.Path)
	default:
		return fmt.Sprintf("manifest error (kind %d): %s", e.Kind, e.Path)
	}
}

func (e *ManifestError) Unwrap() error {
	return e.Wrapped
}

// WriteManifest computes the list of files that would be transformed by the
// given rules (dry-run), then writes recovery-manifest.json into backupDir.
// Written atomically (write to temp file, then os.Rename).
// If no files match, the manifest is still written with an empty Files slice.
// Returns *domain.RefusalError on filesystem failures.
func WriteManifest(backupDir, agentsDir string, rules []TransformRule) error {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  agentsDir,
			Reason:    fmt.Sprintf("cannot read agents directory: %v", err),
		}
	}

	files := make([]ManifestEntry, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isMDFile(entry.Name()) {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(agentsDir, entry.Name()))
		if readErr != nil {
			return &domain.RefusalError{
				Component: "snapshot",
				Resource:  entry.Name(),
				Reason:    fmt.Sprintf("cannot read agent file for manifest: %v", readErr),
			}
		}
		fileEntries := dryRunTransform(entry.Name(), content, rules)
		files = append(files, fileEntries...)
	}

	// Sort entries by filename, then by field within the same filename, for
	// deterministic output that enables byte-for-byte comparison in tests.
	sort.Slice(files, func(i, j int) bool {
		if files[i].Filename != files[j].Filename {
			return files[i].Filename < files[j].Filename
		}
		return files[i].Field < files[j].Field
	})

	m := Manifest{
		Timestamp: time.Now().UTC(),
		AgentsDir: agentsDir,
		Files:     files,
	}

	data, err := json.Marshal(m)
	if err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("cannot marshal manifest: %v", err),
		}
	}

	// Write atomically: write to temp file then rename.
	tmpPath := filepath.Join(backupDir, ManifestTempFileName)
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  tmpPath,
			Reason:    fmt.Sprintf("cannot write manifest temp file: %v", err),
		}
	}
	manifestPath := filepath.Join(backupDir, ManifestFileName)
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		_ = os.Remove(tmpPath)
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  manifestPath,
			Reason:    fmt.Sprintf("cannot rename manifest temp file: %v", err),
		}
	}
	return nil
}

// ReadManifest reads and parses recovery-manifest.json from backupDir.
//
// Returns (*Manifest, nil) on success.
// Returns (nil, *ManifestError{Kind: ManifestMissing}) if the file does not exist.
// Returns (nil, *ManifestError{Kind: ManifestCorrupt}) if it cannot be parsed.
func ReadManifest(backupDir string) (*Manifest, error) {
	manifestPath := filepath.Join(backupDir, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ManifestError{
				Kind: ManifestMissing,
				Path: manifestPath,
			}
		}
		return nil, &ManifestError{
			Kind:    ManifestCorrupt,
			Path:    manifestPath,
			Wrapped: err,
		}
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, &ManifestError{
			Kind:    ManifestCorrupt,
			Path:    manifestPath,
			Wrapped: err,
		}
	}
	return &m, nil
}

// dryRunTransform parses the YAML frontmatter of content and returns a
// ManifestEntry for each frontmatter field that would be changed by one of
// the given rules. The filename parameter is used to populate ManifestEntry.Filename.
//
// A rule matches when a frontmatter line is exactly "{rule.Field}: {rule.OldValue}"
// (with leading whitespace stripped and trailing \r stripped for CRLF files).
// Trailing spaces/tabs on the value are also stripped before comparison,
// matching applyRulesToLine's behavior.
func dryRunTransform(filename string, content []byte, rules []TransformRule) []ManifestEntry {
	if len(content) == 0 || len(rules) == 0 {
		return nil
	}

	lines := bytes.Split(content, []byte("\n"))
	if len(lines) == 0 || strings.TrimSpace(string(lines[0])) != "---" {
		return nil
	}

	// Find closing "---" delimiter.
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			closingIdx = i
			break
		}
	}
	if closingIdx == -1 {
		return nil
	}

	var entries []ManifestEntry
	for i := 1; i < closingIdx; i++ {
		lineStr := string(lines[i])
		trimmed := strings.TrimLeft(lineStr, " \t")

		// Strip trailing \r (CRLF files).
		if strings.HasSuffix(trimmed, "\r") {
			trimmed = trimmed[:len(trimmed)-1]
		}
		// Strip trailing spaces/tabs (matches applyRulesToLine behavior).
		trimmed = strings.TrimRight(trimmed, " \t")

		for _, rule := range rules {
			target := rule.Field + ": " + rule.OldValue
			if trimmed == target {
				entries = append(entries, ManifestEntry{
					Filename:      filename,
					Field:         rule.Field,
					OriginalValue: rule.OldValue,
				})
			}
		}
	}
	return entries
}
