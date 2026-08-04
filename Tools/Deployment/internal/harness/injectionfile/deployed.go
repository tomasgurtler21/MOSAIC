// Package injectionfile provides parsing functions for harness content files.
// This file adds ParseDeployedRegions, ParseDeployedRegionsWithVersion, and LoadDir,
// which operate on [[DEPLOYED:Name]] regions — the format harness content files use
// after the run-time harness content migration.
package injectionfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

// Standard file names for harness content in a harness content directory.
const (
	SharedFileName       = "HarnessInjections.md"
	OrchestratorFileName = "HarnessInjectionsOrchestrator.md"
)

// HarnessContent is one harness's parsed content files.
type HarnessContent struct {
	// Shared is the region map from HarnessInjections.md, applied to every agent.
	Shared map[string]string
	// Orchestrator is the region map from HarnessInjectionsOrchestrator.md, overlaid for
	// the orchestrator agent only.
	Orchestrator map[string]string
	// OrchestratorVersion is the HarnessInjectionsOrchestrator.md frontmatter `version`,
	// the authoritative source of the descriptor's OrchestratorInjectionsVersion.
	OrchestratorVersion string
}

// ParseDeployedRegions extracts all [[DEPLOYED:Name]] regions from a MOSAIC-formatted
// Markdown file and returns a map from region name to its whitespace-trimmed content.
//
// A region declared with empty content produces an entry with an empty-string value; an
// absent region produces no key. [[INJECTION:]] and [[SECTION:]] regions are ignored.
// A file with no deployed regions returns an empty non-nil map and a nil error.
func ParseDeployedRegions(src []byte) (map[string]string, error) {
	regions, _, err := ParseDeployedRegionsWithVersion(src)
	return regions, err
}

// ParseDeployedRegionsWithVersion is ParseDeployedRegions plus the frontmatter `version`
// scalar. version is "" when the frontmatter is absent or carries no version.
func ParseDeployedRegionsWithVersion(src []byte) (regions map[string]string, version string, err error) {
	doc, parseErr := docformat.Parse(src)
	if parseErr != nil {
		return nil, "", fmt.Errorf("injectionfile: parse failed: %w", parseErr)
	}

	if fm := doc.Frontmatter(); fm.Present() {
		if v, ok := fm.Get("version"); ok && v.Kind == mosaic.KindScalar {
			version = v.Scalar
		}
	}

	nodes := doc.Body().DeployedRegions()
	result := make(map[string]string, len(nodes))

	for _, node := range nodes {
		// Detect unclosed regions: if the node has no close tag, its Bytes() will not
		// contain the closing tag form.
		closeTag := "[[/DEPLOYED:" + node.Name() + "]]"
		if !bytes.Contains(node.Bytes(), []byte(closeTag)) {
			return nil, "", fmt.Errorf("injectionfile: [[DEPLOYED:%s]] is opened but never closed", node.Name())
		}
		// Detect duplicate names.
		if _, exists := result[node.Name()]; exists {
			return nil, "", fmt.Errorf("injectionfile: duplicate [[DEPLOYED:%s]] region; each boundary name must appear at most once per document", node.Name())
		}
		// Trim whitespace and normalize line endings so that content read from CRLF-checked-out
		// files (e.g. on Windows) matches what LF-checked-out files produce.
		content := strings.TrimSpace(string(node.Content()))
		content = strings.ReplaceAll(content, "\r\n", "\n")
		result[node.Name()] = content
	}
	return result, version, nil
}

// LoadDir reads both harness content files from dir and returns their parsed content.
//
// Strict: a missing or unparseable file returns an error naming the file. This is the
// loader for built-in harness modules, where absent content is a broken installation.
// The tolerant descriptor-only and external tiers keep their own existing loaders unchanged.
func LoadDir(dir string) (HarnessContent, error) {
	sharedPath := filepath.Join(dir, SharedFileName)
	sharedBytes, err := os.ReadFile(sharedPath)
	if err != nil {
		return HarnessContent{}, fmt.Errorf("injectionfile: read %s: %w", sharedPath, err)
	}

	shared, err := ParseDeployedRegions(sharedBytes)
	if err != nil {
		return HarnessContent{}, fmt.Errorf("injectionfile: parse %s: %w", sharedPath, err)
	}

	orchPath := filepath.Join(dir, OrchestratorFileName)
	orchBytes, err := os.ReadFile(orchPath)
	if err != nil {
		return HarnessContent{}, fmt.Errorf("injectionfile: read %s: %w", orchPath, err)
	}

	orch, orchVersion, err := ParseDeployedRegionsWithVersion(orchBytes)
	if err != nil {
		return HarnessContent{}, fmt.Errorf("injectionfile: parse %s: %w", orchPath, err)
	}

	return HarnessContent{
		Shared:              shared,
		Orchestrator:        orch,
		OrchestratorVersion: orchVersion,
	}, nil
}
