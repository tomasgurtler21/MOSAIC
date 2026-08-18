package pricing

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"mosaic-log-analyzer/internal/domain"
)

// Put persists one model entry to the pricing YAML file.
//
// If the file does not exist it is created, including any missing parent
// directories. If the file exists the entry is added or the existing entry for
// that model is replaced; all other entries are preserved.
//
// Comment preservation: leading comments in the existing file (lines before the
// first non-comment, non-blank content) are written back to the head of the file
// so user annotations and the shipped file's documentation survive a round-trip.
func (s *Store) Put(_ context.Context, entry domain.ModelPricing) error {
	var file pricingFileYAML
	var headerComments string

	raw, readErr := os.ReadFile(s.path)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return fmt.Errorf("read pricing file: %w", readErr)
		}
		// File absent: start with an empty models map.
		file.Models = make(map[string]modelEntryYAML)
	} else {
		// Extract comment lines that precede the first non-comment content so they
		// can be prepended to the rewritten file.
		headerComments = extractHeaderComments(string(raw))

		if parseErr := yaml.Unmarshal(raw, &file); parseErr != nil {
			return fmt.Errorf("parse pricing file: %w", parseErr)
		}
		if file.Models == nil {
			file.Models = make(map[string]modelEntryYAML)
		}
	}

	// Add or replace the entry.
	file.Models[string(entry.Model)] = fromDomain(entry)

	// Marshal the updated models map back to YAML.
	data, err := yaml.Marshal(&file)
	if err != nil {
		return fmt.Errorf("marshal pricing: %w", err)
	}

	// Reconstruct the file: preserved header comments then the marshaled YAML.
	var buf bytes.Buffer
	if headerComments != "" {
		buf.WriteString(headerComments)
	}
	buf.Write(data)

	// Ensure the parent directory exists before writing.
	if mkdirErr := os.MkdirAll(filepath.Dir(s.path), 0o755); mkdirErr != nil {
		return fmt.Errorf("create pricing config directory: %w", mkdirErr)
	}

	if writeErr := os.WriteFile(s.path, buf.Bytes(), 0o644); writeErr != nil {
		return fmt.Errorf("write pricing file: %w", writeErr)
	}

	return nil
}

// extractHeaderComments returns the leading comment and blank lines from a YAML
// file's raw content. Extraction stops at the first non-comment, non-blank line.
// The returned string ends with a newline when non-empty.
func extractHeaderComments(content string) string {
	var sb strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			sb.WriteString(line)
			sb.WriteString("\n")
		} else {
			break
		}
	}
	return sb.String()
}
