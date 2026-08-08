package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"mosaic-agent-test/internal/domain"
)

// ScopeProbe reads a configuration scope's raw document. Abstracted so scope
// inspection is testable with fixture documents and so the adapter's
// promise never to write outside the sandbox is structural: this seam has
// no write.
type ScopeProbe interface {
	Read(scope domain.ConfigScope) ([]byte, error)
}

var ErrScopeAbsent = errors.New("claudecode: configuration scope absent")

// InterceptedToolName is the dispatch tool this adapter's interception
// points target. A configuration scope's PreToolUse hook only competes with
// this adapter when its matcher would select this tool.
const InterceptedToolName = "Task"

// Scopes enumerates the configuration scopes this harness merges, lowest
// precedence first. The sandbox scope is included so the enumeration is a
// complete description rather than a list of exceptions.
func Scopes(s domain.Sandbox) []domain.ConfigScope {
	return []domain.ConfigScope{
		{
			Name:       "sandbox",
			Path:       filepath.Join(s.SubjectDir, SettingsRelPath),
			InSandbox:  true,
			Isolatable: false,
		},
		{
			Name:       "user",
			Path:       userScopeSettingsPath(),
			InSandbox:  false,
			// The harness honours ConfigHomeEnvVar to relocate its user-scope
			// configuration entirely, which is what makes this scope isolatable.
			Isolatable: true,
		},
	}
}

// userScopeSettingsPath resolves the real filesystem path of this harness's
// user-scope settings document, honouring ConfigHomeEnvVar when set.
func userScopeSettingsPath() string {
	if dir := os.Getenv(ConfigHomeEnvVar); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// InspectScope decides whether one scope's document registers a hook that
// rewrites the intercepted call's input. Pure over the document bytes, so
// every judgement it makes is testable from a fixture.
func InspectScope(scope domain.ConfigScope, doc []byte) (domain.ScopeFinding, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(doc, &top); err != nil {
		return domain.ScopeFinding{}, fmt.Errorf("claudecode: parsing scope document %q: %v", scope.Path, err)
	}

	hooksRaw, ok := top["hooks"]
	if !ok {
		return domain.ScopeFinding{Scope: scope}, nil
	}

	var hooks map[string][]Matcher
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		return domain.ScopeFinding{}, fmt.Errorf("claudecode: parsing scope document %q hooks: %v", scope.Path, err)
	}

	matchers, ok := hooks["PreToolUse"]
	if !ok {
		return domain.ScopeFinding{Scope: scope}, nil
	}

	for _, m := range matchers {
		if len(m.Hooks) == 0 {
			continue
		}
		if matcherAppliesToTask(m.Matcher) {
			return domain.ScopeFinding{
				Scope:         scope,
				RewritesInput: true,
				Detail:        fmt.Sprintf("%s registers a PreToolUse hook matching %q (matcher %q)", scope.Name, InterceptedToolName, m.Matcher),
			}, nil
		}
	}

	return domain.ScopeFinding{Scope: scope}, nil
}

// matcherAppliesToTask reports whether a hook matcher would select
// InterceptedToolName. An empty matcher selects every tool. A matcher this
// harness's own convention treats as a regular expression is compiled and
// matched; a matcher that fails to compile as a regular expression is
// compared literally so a bare tool name still matches itself.
func matcherAppliesToTask(matcher string) bool {
	if matcher == "" {
		return true
	}
	if re, err := regexp.Compile(matcher); err == nil {
		return re.MatchString(InterceptedToolName)
	}
	return matcher == InterceptedToolName
}

// fsScopeProbe is the real filesystem ScopeProbe: it reads a scope's
// document straight from scope.Path. Selected whenever Options.ScopeProbe is
// nil.
type fsScopeProbe struct{}

func (fsScopeProbe) Read(scope domain.ConfigScope) ([]byte, error) {
	data, err := os.ReadFile(scope.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrScopeAbsent
		}
		return nil, fmt.Errorf("claudecode: reading scope %q: %w", scope.Path, err)
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
				Detail:      fmt.Sprintf("%s configuration scope not found; treated as absent, not inspected", scope.Name),
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
		detail = fmt.Sprintf("%s configuration scope registers a hook that rewrites the intercepted call's input", scope.Name)
	}
	return domain.ScopeFinding{
		Scope:         scope,
		RewritesInput: finding.RewritesInput,
		Neutralized:   neutralized,
		Detail:        detail,
	}
}
