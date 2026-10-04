package ghcptrust

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newTestChecker returns a checker whose COPILOT_HOME is copilotHome and whose
// home directory is a separate, empty directory.
func newTestChecker(t *testing.T, copilotHome string) (*Checker, string) {
	t.Helper()
	home := t.TempDir()
	c := NewChecker(
		WithGetenv(func(k string) string {
			if k == "COPILOT_HOME" {
				return copilotHome
			}
			return ""
		}),
		WithUserHomeDir(func() (string, error) { return home, nil }),
		WithCaseInsensitive(false),
	)
	return c, home
}

// writeConfig writes a Copilot config.json with the given raw content.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// trustedConfig renders a config.json with the given trustedFolders entries.
func trustedConfig(t *testing.T, entries ...string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"trustedFolders": entries})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigPath_UsesCopilotHomeWhenSet(t *testing.T) {
	copilotHome := t.TempDir()
	c, _ := newTestChecker(t, copilotHome)

	got, err := c.ConfigPath()

	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(copilotHome, "config.json"); filepath.Clean(got) != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}

func TestConfigPath_FallsBackToHomeDotCopilot(t *testing.T) {
	for _, tc := range []struct{ name, copilotHome string }{{"unset", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			c, home := newTestChecker(t, tc.copilotHome)

			got, err := c.ConfigPath()

			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(home, ".copilot", "config.json"); filepath.Clean(got) != want {
				t.Fatalf("ConfigPath = %q, want %q", got, want)
			}
		})
	}
}

func TestConfigPath_NeverSettingsJSON(t *testing.T) {
	c, _ := newTestChecker(t, t.TempDir())

	got, _ := c.ConfigPath()

	if filepath.Base(got) != "config.json" {
		t.Fatalf("ConfigPath = %q, want a config.json path", got)
	}
}

func TestConfigPath_HomeUnresolvableReturnsError(t *testing.T) {
	c := NewChecker(
		WithGetenv(func(string) string { return "" }),
		WithUserHomeDir(func() (string, error) { return "", errors.New("no home") }),
	)

	got, err := c.ConfigPath()

	if err == nil {
		t.Fatalf("err = nil, want an error (path %q)", got)
	}
}

func TestCheck_ClassifiesWorkDir(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "ws", "repo")
	cases := []struct {
		name    string
		content string
		want    Status
	}{
		{"exact entry", trustedConfig(t, workDir), StatusTrusted},
		{"ancestor entry", trustedConfig(t, filepath.Join(root, "ws")), StatusTrusted},
		{"one of several entries", trustedConfig(t, filepath.Join(root, "x"), workDir), StatusTrusted},
		{"child-only entry", trustedConfig(t, filepath.Join(workDir, "child")), StatusUntrusted},
		{"unrelated entry", trustedConfig(t, filepath.Join(root, "other")), StatusUntrusted},
		{"empty array", `{"trustedFolders": []}`, StatusUntrusted},
		{"key absent", `{"theme": "dark"}`, StatusUntrusted},
		{"comment header and BOM", "\xEF\xBB\xBF// Copilot config\n// do not edit\n" + trustedConfig(t, workDir), StatusTrusted},
		{"comment header, untrusted", "// Copilot config\n" + trustedConfig(t, filepath.Join(root, "other")), StatusUntrusted},
		{"not json", `garbage`, StatusIndeterminate},
		{"truncated", `{"trustedFolders": [`, StatusIndeterminate},
		{"trustedFolders null", `{"trustedFolders": null}`, StatusIndeterminate},
		{"trustedFolders wrong type", `{"trustedFolders": "x"}`, StatusIndeterminate},
		{"empty file", ``, StatusIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copilotHome := t.TempDir()
			cfgPath := writeConfig(t, copilotHome, tc.content)
			c, _ := newTestChecker(t, copilotHome)

			res := c.Check(workDir)

			if res.Status != tc.want {
				t.Fatalf("Status = %q, want %q (reason %q)", res.Status, tc.want, res.Reason)
			}
			if filepath.Clean(res.ConfigPath) != cfgPath {
				t.Errorf("ConfigPath = %q, want %q", res.ConfigPath, cfgPath)
			}
			if tc.want == StatusIndeterminate && res.Reason == "" {
				t.Error("Reason is empty for an indeterminate result")
			}
			if tc.want != StatusIndeterminate && res.Reason != "" {
				t.Errorf("Reason = %q, want empty", res.Reason)
			}
		})
	}
}

func TestCheck_CaseInsensitiveWhenConfigured(t *testing.T) {
	copilotHome := t.TempDir()
	writeConfig(t, copilotHome, trustedConfig(t, filepath.Join(copilotHome, "Repo")))
	workDir := filepath.Join(copilotHome, "REPO", "sub")
	home := t.TempDir()
	build := func(ci bool) *Checker {
		return NewChecker(
			WithGetenv(func(k string) string {
				if k == "COPILOT_HOME" {
					return copilotHome
				}
				return ""
			}),
			WithUserHomeDir(func() (string, error) { return home, nil }),
			WithCaseInsensitive(ci),
		)
	}

	insensitive := build(true).Check(workDir)
	sensitive := build(false).Check(workDir)

	if insensitive.Status != StatusTrusted {
		t.Errorf("case-insensitive Status = %q, want trusted", insensitive.Status)
	}
	if sensitive.Status != StatusUntrusted {
		t.Errorf("case-sensitive Status = %q, want untrusted", sensitive.Status)
	}
}

func TestCheck_MissingFileIsIndeterminate(t *testing.T) {
	copilotHome := t.TempDir()
	c, _ := newTestChecker(t, copilotHome)

	res := c.Check(filepath.Join(copilotHome, "ws"))

	if res.Status != StatusIndeterminate {
		t.Fatalf("Status = %q, want indeterminate", res.Status)
	}
	if res.ConfigPath == "" || res.Reason == "" {
		t.Errorf("want ConfigPath and Reason populated, got %+v", res)
	}
}

func TestCheck_UnresolvableHomeIsIndeterminateWithEmptyConfigPath(t *testing.T) {
	c := NewChecker(
		WithGetenv(func(string) string { return "" }),
		WithUserHomeDir(func() (string, error) { return "", errors.New("no home") }),
	)

	res := c.Check(t.TempDir())

	if res.Status != StatusIndeterminate {
		t.Fatalf("Status = %q, want indeterminate", res.Status)
	}
	if res.ConfigPath != "" {
		t.Errorf("ConfigPath = %q, want empty", res.ConfigPath)
	}
}

func TestCheck_UsesHomeDotCopilotWhenCopilotHomeUnset(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	c, home := newTestChecker(t, "")
	dir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, trustedConfig(t, workDir))

	res := c.Check(workDir)

	if res.Status != StatusTrusted {
		t.Fatalf("Status = %q, want trusted (reason %q)", res.Status, res.Reason)
	}
}

func TestCheck_SettingsJSONTrustIsIgnored(t *testing.T) {
	copilotHome := t.TempDir()
	workDir := filepath.Join(copilotHome, "repo")
	writeConfig(t, copilotHome, `{"trustedFolders": []}`)
	settings := filepath.Join(copilotHome, "settings.json")
	if err := os.WriteFile(settings, []byte(trustedConfig(t, workDir)), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := newTestChecker(t, copilotHome)

	res := c.Check(workDir)

	if res.Status != StatusUntrusted {
		t.Fatalf("Status = %q, want untrusted (settings.json must be ignored)", res.Status)
	}
}

func TestCheck_SettingsJSONAloneIsIndeterminate(t *testing.T) {
	copilotHome := t.TempDir()
	workDir := filepath.Join(copilotHome, "repo")
	if err := os.WriteFile(filepath.Join(copilotHome, "settings.json"), []byte(trustedConfig(t, workDir)), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := newTestChecker(t, copilotHome)

	res := c.Check(workDir)

	if res.Status != StatusIndeterminate {
		t.Fatalf("Status = %q, want indeterminate (no config.json)", res.Status)
	}
}

func TestCheck_NeverModifiesCopilotHome(t *testing.T) {
	copilotHome := t.TempDir()
	workDir := filepath.Join(copilotHome, "repo")
	content := "// header\n" + trustedConfig(t, filepath.Join(copilotHome, "other"))
	cfgPath := writeConfig(t, copilotHome, content)
	c, _ := newTestChecker(t, copilotHome)

	_ = c.Check(workDir)
	_ = NewChecker(WithGetenv(func(string) string { return t.TempDir() })).Check(workDir)

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Errorf("config.json changed: %q", after)
	}
	entries, err := os.ReadDir(copilotHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("Copilot home has %d entries after Check, want only config.json", len(entries))
	}
}
