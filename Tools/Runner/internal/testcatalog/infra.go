package testcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// agentFrontmatter is a minimal frontmatter struct used to detect the
// infrastructure: field in Subagents/MosaicTest/ agent .md files.
type agentFrontmatter struct {
	Infrastructure string `yaml:"infrastructure"`
}

// InfrastructureAgentKeys returns the sorted list of infrastructure agent
// keys (filename stems) discovered from the Subagents/MosaicTest/ directory
// under the catalog root.
//
// Returns nil when the directory does not exist or cannot be read ("don't
// know" -- the deploy tool will ask interactively). Returns a non-nil empty
// []string{} when the directory exists but contains no agents with an
// infrastructure: frontmatter field ("explicitly none").
//
// Individual files with malformed frontmatter are skipped without error.
// Non-.md files and subdirectories are ignored.
func (c *Catalog) InfrastructureAgentKeys() []string {
	dir := filepath.Join(c.root, "Subagents", "MosaicTest")

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory missing or unreadable: "don't know" semantics.
		return nil
	}

	keys := []string{}
	for _, entry := range entries {
		// Skip subdirectories.
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip non-.md files.
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		filePath := filepath.Join(dir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Skip unreadable files.
			continue
		}

		fm, err := parseAgentFrontmatter(string(data))
		if err != nil {
			// Skip files with malformed frontmatter.
			continue
		}

		if fm.Infrastructure != "" {
			stem := strings.TrimSuffix(name, ".md")
			keys = append(keys, stem)
		}
	}

	sort.Strings(keys)
	return keys
}

// parseAgentFrontmatter parses the YAML frontmatter from an agent .md file.
// Returns an error if the file does not have valid YAML frontmatter.
func parseAgentFrontmatter(content string) (*agentFrontmatter, error) {
	// Normalize line endings.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, "---\n") {
		// No frontmatter delimiter -- treat as missing frontmatter (no infrastructure field).
		return &agentFrontmatter{}, nil
	}

	rest := content[4:] // skip opening "---\n"
	end := strings.Index(rest, "\n---")
	if end < 0 {
		// No closing delimiter -- malformed.
		return nil, errors.New("missing closing YAML frontmatter delimiter")
	}

	yamlContent := rest[:end]

	var fm agentFrontmatter
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return nil, err
	}

	return &fm, nil
}
