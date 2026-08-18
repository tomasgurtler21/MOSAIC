package opencode

// Scope enumeration, the probe seam and pure per-document inspection. See
// ContractsDesign.md's "AgentTest: Scope Inspection" for the full contract
// (scope table and inspectOneScope folding rules).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-agent-test/internal/domain"
)

// ScopeProbe reads a configuration scope's raw document. Abstracted so scope
// inspection is testable from fixtures and so the adapter's promise never to
// write outside the sandbox is structural: this seam has no write.
//
// What "document" means is per-scope and fixed by the scope's Name:
//   - a configuration-file scope's document is the file's bytes;
//   - a plugin-directory scope's document is a newline-separated listing of
//     the plugin entries the directory contains.
type ScopeProbe interface {
	Read(scope domain.ConfigScope) ([]byte, error)
}

// ErrScopeAbsent reports that a probed scope does not exist (as distinct
// from existing but being unreadable).
var ErrScopeAbsent = errors.New("opencode: configuration scope absent")

// InterceptedToolName is the dispatch tool this adapter's interception
// points target.
const InterceptedToolName = "task"

// Scopes enumerates the configuration scopes this harness merges, lowest
// precedence first. The sandbox scope is included so the enumeration is a
// complete description rather than a list of exceptions.
//
// Ordering is global-then-sandbox: a project scope overrides a global one,
// so the sandbox scope is highest precedence and comes last.
func Scopes(s domain.Sandbox) []domain.ConfigScope {
	dir := userConfigDir()
	return []domain.ConfigScope{
		{
			Name: "user-config",
			Path: filepath.Join(dir, ConfigFileName),
			// Neither non-sandbox scope is claimed Isolatable: no environment
			// variable that relocates OpenCode's user-scope configuration is
			// confirmed, and the adapter must not claim a neutralization
			// capability it cannot back up.
			InSandbox:  false,
			Isolatable: false,
		},
		{
			Name:       "user-plugins",
			Path:       filepath.Join(dir, "plugin"),
			InSandbox:  false,
			Isolatable: false,
		},
		{
			Name:       "sandbox",
			Path:       filepath.Join(s.SubjectDir, PluginsRelDir),
			InSandbox:  true,
			Isolatable: false,
		},
	}
}

// userConfigDir resolves OpenCode's user-scope configuration directory: it
// honours XDG_CONFIG_HOME when set and falls back to <home>/.config/opencode,
// so inspection resolves the same path the CLI would. The adapter does not
// set this variable in any spawn plan, so it does not claim to have
// neutralized either scope that lives under it.
func userConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "opencode")
}

// InspectScope decides whether one scope registers something that could
// rewrite the intercepted call's input. Pure over the document bytes, so
// every judgement it makes is testable from a fixture.
//
// The verdict is deliberately conservative: any plugin present may register
// a pre-invocation hook, and neither a document nor a directory listing
// alone can tell whether it does. A false "this competes" surfaces as a
// pre-flight refusal an operator can see and act on; a false "clean" would
// silently corrupt every verdict the tool produces.
func InspectScope(scope domain.ConfigScope, doc []byte) (domain.ScopeFinding, error) {
	switch scope.Name {
	case "user-config":
		return inspectUserConfigScope(scope, doc)
	case "user-plugins":
		return inspectUserPluginsScope(scope, doc), nil
	default:
		return domain.ScopeFinding{Scope: scope}, nil
	}
}

// inspectUserConfigScope reports RewritesInput when the document declares a
// non-empty "plugin" array.
func inspectUserConfigScope(scope domain.ConfigScope, doc []byte) (domain.ScopeFinding, error) {
	var top struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal(doc, &top); err != nil {
		return domain.ScopeFinding{}, fmt.Errorf("opencode: parsing scope document %q: %w", scope.Path, err)
	}
	if len(top.Plugin) == 0 {
		return domain.ScopeFinding{Scope: scope}, nil
	}
	return domain.ScopeFinding{
		Scope:         scope,
		RewritesInput: true,
		Detail: fmt.Sprintf(
			"%s declares a non-empty plugin array (%d entries); any plugin present may register a hook that rewrites %q's input",
			scope.Name, len(top.Plugin), InterceptedToolName,
		),
	}, nil
}

// inspectUserPluginsScope reports RewritesInput when the directory listing
// is non-empty.
func inspectUserPluginsScope(scope domain.ConfigScope, doc []byte) domain.ScopeFinding {
	if strings.TrimSpace(string(doc)) == "" {
		return domain.ScopeFinding{Scope: scope}
	}
	return domain.ScopeFinding{
		Scope:         scope,
		RewritesInput: true,
		Detail: fmt.Sprintf(
			"%s is a non-empty plugin-directory listing; any plugin present may register a hook that rewrites %q's input",
			scope.Name, InterceptedToolName,
		),
	}
}

// fsScopeProbe is the real filesystem ScopeProbe. Selected whenever
// Options.ScopeProbe is nil. It never writes: the "never writes outside the
// sandbox" promise is structural because this seam has no write method.
type fsScopeProbe struct{}

func (fsScopeProbe) Read(scope domain.ConfigScope) ([]byte, error) {
	if scope.Name == "user-plugins" {
		entries, err := os.ReadDir(scope.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, ErrScopeAbsent
			}
			return nil, fmt.Errorf("opencode: reading scope %q: %w", scope.Path, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return []byte(strings.Join(names, "\n")), nil
	}

	data, err := os.ReadFile(scope.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrScopeAbsent
		}
		return nil, fmt.Errorf("opencode: reading scope %q: %w", scope.Path, err)
	}
	return data, nil
}

// inspectOneScope reads and judges one non-sandbox scope, folding every
// outcome — absent, unreadable, malformed, clean, rewriting — into a
// ScopeFinding that never silently reads as "inspected and clean" when it
// was not.
func inspectOneScope(probe ScopeProbe, scope domain.ConfigScope) domain.ScopeFinding {
	doc, err := probe.Read(scope)
	if err != nil {
		if errors.Is(err, ErrScopeAbsent) {
			return domain.ScopeFinding{
				Scope:       scope,
				Neutralized: scope.Isolatable,
				// This adapter never relocates or otherwise interferes with a
				// scope: it neither isolates nor breaks anything, so it
				// reports true unconditionally, per ContractsDesign.md's
				// domain.ScopeFinding contract.
				PreservesSubjectFunction: true,
				Detail:                   fmt.Sprintf("%s configuration scope not found; treated as absent, not inspected", scope.Name),
			}
		}
		return domain.ScopeFinding{
			Scope:         scope,
			RewritesInput: true,
			Detail:        fmt.Sprintf("%s configuration scope could not be read: %v; treated as unresolved for safety", scope.Name, err),
		}
	}

	finding, err := InspectScope(scope, doc)
	if err != nil {
		return domain.ScopeFinding{
			Scope:         scope,
			RewritesInput: true,
			Detail:        fmt.Sprintf("%s configuration scope document malformed: %v", scope.Name, err),
		}
	}

	neutralized := scope.Isolatable && !finding.RewritesInput
	detail := finding.Detail
	if finding.RewritesInput && !neutralized && detail == "" {
		detail = fmt.Sprintf("%s configuration scope registers something that rewrites the intercepted call's input", scope.Name)
	}
	return domain.ScopeFinding{
		Scope:         scope,
		RewritesInput: finding.RewritesInput,
		Neutralized:   neutralized,
		// This adapter never relocates or otherwise interferes with a scope:
		// it neither isolates nor breaks anything, so it reports true
		// unconditionally, per ContractsDesign.md's domain.ScopeFinding
		// contract.
		PreservesSubjectFunction: true,
		Detail:                   detail,
	}
}

// ConfigScopes implements domain.HarnessAdapter. It enumerates scopes
// independent of any particular sandbox: the port method takes none, so the
// sandbox-scoped entry it returns carries no real root and exists only so
// InspectScopes filters it out uniformly.
func (a *Adapter) ConfigScopes() []domain.ConfigScope {
	return Scopes(domain.Sandbox{})
}

// InspectScopes implements domain.HarnessAdapter. It examines every
// non-sandbox scope ConfigScopes declares through this adapter's ScopeProbe
// (the real filesystem unless Options.ScopeProbe overrides it), and never
// folds an absent, unreadable or malformed scope into the findings as an
// ordinary clean inspection.
func (a *Adapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	probe := a.opts.ScopeProbe
	if probe == nil {
		probe = fsScopeProbe{}
	}

	var findings []domain.ScopeFinding
	for _, scope := range a.ConfigScopes() {
		if scope.InSandbox {
			continue
		}
		findings = append(findings, inspectOneScope(probe, scope))
	}
	return findings, nil
}
