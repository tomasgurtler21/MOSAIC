package catalog

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/hookbundle"
	"mosaic-deploy/internal/domain"
)

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

// parseHookBundle reads hook.yaml from bundleDir via mosaic-common/hookbundle, maps the
// shared model onto the deployment tool's own domain types, resolves variant reuse, and
// validates the content_hash when present. Content-hash validation, catalog scanning and
// all plan-building policy remain here: mosaic-common/hookbundle owns only the manifest's
// meaning.
func parseHookBundle(bundleDir string) (domain.HookBundle, []Issue, error) {
	yamlPath := filepath.Join(bundleDir, "hook.yaml")
	mw, err := hookbundle.Load(bundleDir)
	if err != nil {
		return domain.HookBundle{}, nil, err
	}

	bundle := domain.HookBundle{
		Key:         mw.ID,
		Version:     mw.Version,
		Description: mw.Description,
		SourceDir:   bundleDir,
		Placeholder: mw.Placeholder,
		Variants:    make(map[string]domain.HookVariant),
	}

	// First pass: build own-file variants.
	for varName, vy := range mw.Variants {
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
	if mw.ContentHash != "" {
		issues = validateContentHash(bundle, mw, bundleDir, yamlPath)
	}

	return bundle, issues, nil
}

// validateContentHash computes the expected hash of all variant files and compares it
// against the stored hash. Reports a hook-hash-mismatch issue on mismatch.
func validateContentHash(bundle domain.HookBundle, mw hookbundle.Manifest, bundleDir, yamlPath string) []Issue {
	// Sort variant names for a deterministic byte order.
	varNames := make([]string, 0, len(mw.Variants))
	for name := range mw.Variants {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)

	h := sha256.New()
	for _, varName := range varNames {
		vy := mw.Variants[varName]
		if !vy.Supported || vy.Reuses != "" {
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

	stored := strings.TrimSpace(mw.ContentHash)
	if computed != stored {
		return []Issue{{
			Severity: docformat.SeverityError,
			Code:     "hook-hash-mismatch",
			Subject:  mw.ID,
			Message: fmt.Sprintf(
				"hook bundle %q content_hash mismatch: stored %q, computed %q",
				mw.ID, stored, computed,
			),
			Path: yamlPath,
		}}
	}
	return nil
}
