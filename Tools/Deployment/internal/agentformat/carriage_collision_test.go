package agentformat_test

// carriage_collision_test.go covers the carriage-key collision test. It asserts
// that neither carriage key ("mosaic_carriage", "mosaic_carriage_markers") equals:
//
//   (a) Any agentfields Deployed name or Legacy name.
//   (b) Any key name in the add, drop or key_order lists of any registered builtin
//       harness descriptor.
//
// A collision would cause the container to be treated as a MOSAIC stamp (if it
// collides with an agentfields name) or to be silently dropped or reordered by the
// descriptor (if it collides with a descriptor field name), destroying every
// carried user key.
//
// Stage 13 T13.13 covers the descriptor drop-list collision from the registry-wide
// side; this test covers the agentfields side and the add/key_order lists.
//
// The test navigates the filesystem to load builtin harness YAML descriptors because
// harness/builtin packages are forbidden in the translator layer (the purity rule),
// so the production code cannot check this itself.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/harness/descriptor"
)

// repoRootFromAgentformat returns the repository root from the agentformat test
// cwd. The package is at Tools/Deployment/internal/agentformat/ (4 levels from
// the repo root).
func repoRootFromAgentformat(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root from agentformat: %v", err)
	}
	return abs
}

// TestCarriageKeyCollision_NotInAgentfields verifies that neither carriage key
// equals any agentfields Deployed or Legacy name. A collision would cause
// isMosaicOwned to return true for the container key, which would put the container
// into the stamp block rather than being consumed by the carriage path.
func TestCarriageKeyCollision_NotInAgentfields(t *testing.T) {
	for _, carriageKey := range agentformat.CarriageKeys() {
		for _, field := range agentfields.All() {
			if carriageKey == field.Deployed {
				t.Errorf("carriage key %q collides with agentfields Deployed name %q; this would cause the container to be treated as a MOSAIC stamp", carriageKey, field.Deployed)
			}
			if carriageKey == field.Legacy {
				t.Errorf("carriage key %q collides with agentfields Legacy name %q; this would cause isMosaicOwned to misclassify the container", carriageKey, field.Legacy)
			}
		}
	}
}

// TestCarriageKeyCollision_NotInDescriptorFields verifies that neither carriage key
// appears in the add, drop or key_order lists of any registered builtin harness
// descriptor. A collision would cause the descriptor to silently drop or reorder
// the carriage container, destroying every carried user key.
func TestCarriageKeyCollision_NotInDescriptorFields(t *testing.T) {
	root := repoRootFromAgentformat(t)
	builtinDir := filepath.Join(root, "Tools", "Deployment", "internal", "harness", "builtin")

	// Walk all subdirectories of builtin/ to find YAML descriptor files.
	yamlFiles, err := findBuiltinDescriptorYAMLs(builtinDir)
	if err != nil {
		t.Fatalf("find builtin descriptor YAMLs in %s: %v", builtinDir, err)
	}
	if len(yamlFiles) == 0 {
		t.Fatalf("no builtin descriptor YAML files found under %s; cannot check for collisions", builtinDir)
	}

	for _, yamlPath := range yamlFiles {
		yamlPath := yamlPath
		t.Run(strings.TrimPrefix(yamlPath, builtinDir+string(filepath.Separator)), func(t *testing.T) {
			src, readErr := os.ReadFile(yamlPath)
			if readErr != nil {
				t.Fatalf("read %s: %v", yamlPath, readErr)
			}
			d, parseErr := descriptor.Parse(src, yamlPath)
			if parseErr != nil {
				t.Fatalf("parse %s: %v", yamlPath, parseErr)
			}

			for _, carriageKey := range agentformat.CarriageKeys() {
				// Check Add list.
				for _, addField := range d.Frontmatter.Add {
					if addField.Key == carriageKey {
						t.Errorf("descriptor %s: carriage key %q appears in frontmatter.add; this would cause the descriptor to overwrite the carriage container", yamlPath, carriageKey)
					}
				}

				// Check Drop list.
				for _, dropKey := range d.Frontmatter.Drop {
					if dropKey == carriageKey {
						t.Errorf("descriptor %s: carriage key %q appears in frontmatter.drop; this would cause the descriptor to silently remove the carriage container", yamlPath, carriageKey)
					}
				}

				// Check KeyOrder list.
				for _, orderKey := range d.Frontmatter.KeyOrder {
					if orderKey == carriageKey {
						t.Errorf("descriptor %s: carriage key %q appears in frontmatter.key_order; this would cause the descriptor to reorder or expose the carriage container", yamlPath, carriageKey)
					}
				}
			}
		})
	}
}

// findBuiltinDescriptorYAMLs walks the builtin harness directory and returns all
// YAML files found in immediate subdirectories. The pattern is one subdirectory
// per harness, each containing exactly one descriptor YAML file.
func findBuiltinDescriptorYAMLs(builtinDir string) ([]string, error) {
	var results []string
	entries, err := os.ReadDir(builtinDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subdir := filepath.Join(builtinDir, entry.Name())
		subEntries, readErr := os.ReadDir(subdir)
		if readErr != nil {
			return nil, readErr
		}
		for _, sub := range subEntries {
			if !sub.IsDir() && strings.HasSuffix(sub.Name(), ".yaml") {
				results = append(results, filepath.Join(subdir, sub.Name()))
			}
		}
	}
	return results, nil
}
