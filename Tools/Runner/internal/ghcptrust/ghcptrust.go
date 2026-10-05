// Package ghcptrust checks whether a working directory is a Copilot CLI trusted
// folder. It reads the Copilot configuration read-only and never writes a file.
package ghcptrust

import (
	"os"
	"path/filepath"
	"runtime"
)

// Status classifies a trust check.
type Status string

const (
	StatusTrusted       Status = "trusted"       // workDir equals or descends from a trustedFolders entry
	StatusUntrusted     Status = "untrusted"     // config read and parsed; no entry covers workDir
	StatusIndeterminate Status = "indeterminate" // config missing/unreadable/unparsable, or home unresolvable
)

// Result is the outcome of one check. Never carries an error that blocks a run.
type Result struct {
	Status     Status
	ConfigPath string // the config.json path consulted ("" if it could not be resolved)
	Reason     string // short human-readable detail for Indeterminate (empty otherwise)
}

// Checker evaluates Copilot CLI folder trust.
type Checker struct {
	getenv          func(string) string
	userHomeDir     func() (string, error)
	caseInsensitive bool
}

// Option configures a Checker (test seams).
type Option func(*Checker)

// WithGetenv replaces the environment lookup.
func WithGetenv(getenv func(string) string) Option {
	return func(c *Checker) { c.getenv = getenv }
}

// WithUserHomeDir replaces the home directory lookup.
func WithUserHomeDir(home func() (string, error)) Option {
	return func(c *Checker) { c.userHomeDir = home }
}

// WithCaseInsensitive sets case-insensitive path comparison.
func WithCaseInsensitive(ci bool) Option {
	return func(c *Checker) { c.caseInsensitive = ci }
}

// NewChecker returns a checker with production defaults. Path comparison is
// case-insensitive on Windows only; case sensitivity on Linux, macOS and UNC
// paths has not been probed against the Copilot CLI.
func NewChecker(opts ...Option) *Checker {
	c := &Checker{
		getenv:          os.Getenv,
		userHomeDir:     os.UserHomeDir,
		caseInsensitive: runtime.GOOS == "windows",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ConfigPath returns $COPILOT_HOME/config.json when COPILOT_HOME is set and
// non-empty, else {home}/.copilot/config.json. Only config.json is honoured by
// the Copilot CLI; settings.json is never consulted.
func (c *Checker) ConfigPath() (string, error) {
	if dir := c.getenv("COPILOT_HOME"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	home, err := c.userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".copilot", "config.json"), nil
}

// Check reads the config at ConfigPath and classifies workDir.
func (c *Checker) Check(workDir string) Result {
	path, err := c.ConfigPath()
	if err != nil {
		return Result{Status: StatusIndeterminate, Reason: "cannot resolve the Copilot config location: " + err.Error()}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Status: StatusIndeterminate, ConfigPath: path, Reason: "cannot read the Copilot config: " + err.Error()}
	}
	folders, err := ParseTrustedFolders(data)
	if err != nil {
		return Result{Status: StatusIndeterminate, ConfigPath: path, Reason: "cannot parse the Copilot config: " + err.Error()}
	}
	if IsTrusted(workDir, folders, c.caseInsensitive) {
		return Result{Status: StatusTrusted, ConfigPath: path}
	}
	return Result{Status: StatusUntrusted, ConfigPath: path}
}
