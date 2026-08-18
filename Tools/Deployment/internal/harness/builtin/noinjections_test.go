package builtin_test

// Tests verifying that no YAML descriptor (embedded or on-disk in Tools/Deployment/descriptors/)
// contains an injections: key after the migration to HarnessInjections.md-sourced injections.
//
// These tests satisfy the contract that injection content is sourced exclusively from
// HarnessInjections.md files, and the YAML injections: field is deprecated and removed.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the absolute path to the repository root, navigating up from this
// package's directory. Go tests run with cwd set to the package directory.
// Package is at Tools/Deployment/internal/harness/builtin/ — 5 levels below repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// TestNoInjections_EmbeddedDescriptors verifies that none of the four embedded YAML
// descriptors (claude-code.yaml, opencode.yaml, ghcp-cli.yaml, vscode-ghcp.yaml) contain
// an injections: key. This confirms the YAML field has been removed from all built-in sources.
func TestNoInjections_EmbeddedDescriptors(t *testing.T) {
	root := repoRoot(t)
	descriptors := []struct {
		harness string
		path    string
	}{
		{"claude-code", filepath.Join(root, "Tools", "Deployment", "internal", "harness", "builtin", "claudecode", "claude-code.yaml")},
		{"opencode", filepath.Join(root, "Tools", "Deployment", "internal", "harness", "builtin", "opencode", "opencode.yaml")},
		{"ghcp-cli", filepath.Join(root, "Tools", "Deployment", "internal", "harness", "builtin", "ghcpcli", "ghcp-cli.yaml")},
		{"vscode-ghcp", filepath.Join(root, "Tools", "Deployment", "internal", "harness", "builtin", "vscodeghcp", "vscode-ghcp.yaml")},
	}

	for _, d := range descriptors {
		d := d
		t.Run(d.harness, func(t *testing.T) {
			data, err := os.ReadFile(d.path)
			if err != nil {
				t.Fatalf("read %s: %v", d.path, err)
			}
			if containsInjectionsKey(data) {
				t.Errorf("embedded descriptor %q contains an injections: key; it must be removed (injection content is sourced from HarnessInjections.md)", d.path)
			}
		})
	}
}

// TestNoInjections_DeploymentDescriptors verifies that none of the deployment descriptors in
// Tools/Deployment/descriptors/ contain an injections: key. This confirms the YAML field
// has been removed from all on-disk deployment descriptor sources.
func TestNoInjections_DeploymentDescriptors(t *testing.T) {
	root := repoRoot(t)
	descriptorsDir := filepath.Join(root, "Tools", "Deployment", "descriptors")

	entries, err := os.ReadDir(descriptorsDir)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("deployment descriptors directory not found at %s", descriptorsDir)
		}
		t.Fatalf("read descriptors directory: %v", err)
	}

	yamlFound := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		yamlFound = true
		name := e.Name()
		path := filepath.Join(descriptorsDir, name)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if containsInjectionsKey(data) {
				t.Errorf("deployment descriptor %q contains an injections: key; it must be removed (injection content is sourced from HarnessInjections.md)", path)
			}
		})
	}

	if !yamlFound {
		t.Skipf("no YAML files found in %s", descriptorsDir)
	}
}

// containsInjectionsKey reports whether the YAML content contains a top-level injections:
// key. This is a line-level heuristic: a line beginning with "injections:" indicates the
// deprecated field is present.
func containsInjectionsKey(data []byte) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("injections:")) {
			return true
		}
	}
	return false
}
