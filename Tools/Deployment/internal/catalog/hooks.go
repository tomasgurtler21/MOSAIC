package catalog

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// YAML wire types for hook.yaml
// ---------------------------------------------------------------------------

type hookYAML struct {
	SchemaVersion string                      `yaml:"schema_version"`
	ID            string                      `yaml:"id"`
	Version       string                      `yaml:"version"`
	Description   string                      `yaml:"description"`
	Placeholder   bool                        `yaml:"placeholder"`
	ContentHash   string                      `yaml:"content_hash"`
	Variants      map[string]*hookVariantYAML `yaml:"variants"`
}

type hookVariantYAML struct {
	Supported    bool           `yaml:"supported"`
	Reuses       string         `yaml:"reuses"`
	Files        []hookFileYAML `yaml:"files"`
	Registration []hookRegYAML  `yaml:"registration"`
}

type hookFileYAML struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type hookRegYAML struct {
	ID          string `yaml:"id"`
	TargetPath  string `yaml:"target_path"`
	Performable bool   `yaml:"performable"`
	Instruction string `yaml:"instruction"`
	Fragment    string `yaml:"fragment"`
}

// ---------------------------------------------------------------------------
// Scanner
// ---------------------------------------------------------------------------

// loadHooks scans Agents/Generic/Hooks/ for bundle directories and populates hooks,
// hookIdx, sourcePaths, and any integrity issues on the receiver.
func (c *catalogImpl) loadHooks(root string) []Issue {
	hooksDir := filepath.Join(root, "Agents", "Generic", "Hooks")
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		// Missing hooks directory is not a hard error.
		return nil
	}

	var issues []Issue
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		bundleDir := filepath.Join(hooksDir, entry.Name())
		bundle, bundleIssues, err := parseHookBundle(bundleDir)
		if err != nil {
			continue
		}
		issues = append(issues, bundleIssues...)
		c.hooks = append(c.hooks, bundle)
		c.hookIdx[bundle.Key] = bundle

		// Register hook file source paths.
		for _, variant := range bundle.Variants {
			for _, f := range variant.Files {
				c.sourcePaths[f.SourcePath] = true
			}
		}
	}
	return issues
}

// parseHookBundle reads hook.yaml from bundleDir, builds the HookBundle, resolves variant
// reuse, and validates the content_hash when present.
func parseHookBundle(bundleDir string) (domain.HookBundle, []Issue, error) {
	yamlPath := filepath.Join(bundleDir, "hook.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return domain.HookBundle{}, nil, err
	}

	var hw hookYAML
	if err := yaml.Unmarshal(data, &hw); err != nil {
		return domain.HookBundle{}, nil, fmt.Errorf("hooks: parse %s: %w", yamlPath, err)
	}

	bundle := domain.HookBundle{
		Key:         hw.ID,
		Version:     hw.Version,
		Description: hw.Description,
		SourceDir:   bundleDir,
		Placeholder: hw.Placeholder,
		Variants:    make(map[string]domain.HookVariant),
	}

	// First pass: build own-file variants.
	for varName, vy := range hw.Variants {
		if vy == nil {
			continue
		}
		variant := domain.HookVariant{
			HarnessID:     varName,
			Supported:     vy.Supported,
			ReusesVariant: vy.Reuses,
		}

		if vy.Supported && vy.Reuses == "" {
			variantDir := filepath.Join(bundleDir, varName)
			for _, fy := range vy.Files {
				srcPath := filepath.Join(variantDir, fy.Source)
				variant.Files = append(variant.Files, domain.HookFile{
					SourcePath: srcPath,
					TargetName: fy.Target,
				})
			}
		}

		for _, ry := range vy.Registration {
			variant.Registration = append(variant.Registration, domain.RegistrationStep{
				ID:          ry.ID,
				TargetPath:  ry.TargetPath,
				Fragment:    ry.Fragment,
				Performable: ry.Performable,
				Instruction: ry.Instruction,
			})
		}

		bundle.Variants[varName] = variant
	}

	// Second pass: resolve reuse references.
	for varName, variant := range bundle.Variants {
		if variant.ReusesVariant == "" {
			continue
		}
		ref, ok := bundle.Variants[variant.ReusesVariant]
		if !ok {
			continue
		}
		variant.Files = ref.Files
		bundle.Variants[varName] = variant
	}

	// Validate content_hash when present.
	var issues []Issue
	if hw.ContentHash != "" {
		issues = validateContentHash(bundle, hw, bundleDir)
	}

	return bundle, issues, nil
}

// validateContentHash computes the expected hash of all variant files and compares it
// against the stored hash. Reports a hook-hash-mismatch issue on mismatch.
func validateContentHash(bundle domain.HookBundle, hw hookYAML, bundleDir string) []Issue {
	// Sort variant names for a deterministic byte order.
	varNames := make([]string, 0, len(hw.Variants))
	for name := range hw.Variants {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)

	h := sha256.New()
	for _, varName := range varNames {
		vy := hw.Variants[varName]
		if vy == nil || !vy.Supported || vy.Reuses != "" {
			continue
		}
		variantDir := filepath.Join(bundleDir, varName)
		for _, fy := range vy.Files {
			srcPath := filepath.Join(variantDir, fy.Source)
			fileBytes, err := os.ReadFile(srcPath)
			if err != nil {
				continue
			}
			h.Write(fileBytes)
		}
	}
	computed := fmt.Sprintf("sha256:%x", h.Sum(nil))

	stored := strings.TrimSpace(hw.ContentHash)
	if computed != stored {
		return []Issue{{
			Severity: docformat.SeverityError,
			Code:     "hook-hash-mismatch",
			Subject:  hw.ID,
			Message: fmt.Sprintf(
				"hook bundle %q content_hash mismatch: stored %q, computed %q",
				hw.ID, stored, computed,
			),
			Path: filepath.Join(bundleDir, "hook.yaml"),
		}}
	}
	return nil
}
