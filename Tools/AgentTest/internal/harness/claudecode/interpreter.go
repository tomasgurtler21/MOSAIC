package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Interpreter is the resolved command the logger bundle's registration
// fragment is made to invoke, replacing the command it publishes.
type Interpreter struct {
	Command string
	Source  InterpreterSource
	Tried   []string // every candidate examined, in order, for the diagnostic
}

// InterpreterSource names where a resolved Interpreter came from.
type InterpreterSource string

const (
	InterpreterConfigured InterpreterSource = "configured"
	InterpreterResolved   InterpreterSource = "resolved"
)

// PublishedInterpreter is the command the bundle's fragment carries and
// which substitution replaces. It is a constant of this adapter because it
// names the text being substituted, not a policy about which interpreter to
// use.
const PublishedInterpreter = "python3"

// InterpreterCandidates are tried in order when no interpreter is
// configured. The list exists because the published command is frequently
// absent on the primary development platform, where the interpreter is
// named differently.
var InterpreterCandidates = []string{"python3", "py", "python"}

var ErrInterpreterUnresolved = errors.New("claudecode: no usable interpreter found")

// ResolveInterpreter returns a usable interpreter. A candidate is accepted
// only after it has been executed successfully, not merely found on the
// path: a shim that resolves and then fails to run is exactly the failure
// this check exists to catch.
func ResolveInterpreter(ctx context.Context, configured string) (Interpreter, error) {
	if configured != "" {
		if err := probeInterpreter(ctx, configured); err != nil {
			return Interpreter{Tried: []string{configured}},
				fmt.Errorf("%w: tried the configured interpreter %q: %v", ErrInterpreterUnresolved, configured, err)
		}
		return Interpreter{Command: configured, Source: InterpreterConfigured, Tried: []string{configured}}, nil
	}

	var tried []string
	for _, candidate := range InterpreterCandidates {
		tried = append(tried, candidate)
		if err := probeInterpreter(ctx, candidate); err == nil {
			return Interpreter{Command: candidate, Source: InterpreterResolved, Tried: tried}, nil
		}
	}
	return Interpreter{Tried: tried},
		fmt.Errorf("%w: tried %s; install one of these or set an interpreter explicitly", ErrInterpreterUnresolved, strings.Join(tried, ", "))
}

// probeInterpreter accepts a candidate only once it has actually run.
func probeInterpreter(ctx context.Context, cmd string) error {
	c := exec.CommandContext(ctx, cmd, "--version")
	return c.Run()
}

// SubstituteInterpreter replaces published with resolved in every entry of s
// that invokes it, and returns the number of entries changed so a
// substitution that silently matched nothing is detectable.
func SubstituteInterpreter(s Settings, published, resolved string) (Settings, int) {
	changed := 0
	out := Settings{Other: s.Other}
	if s.Hooks != nil {
		out.Hooks = make(map[string][]Matcher, len(s.Hooks))
		for key, matchers := range s.Hooks {
			newMatchers := make([]Matcher, len(matchers))
			for i, m := range matchers {
				newHooks := make([]Entry, len(m.Hooks))
				for j, e := range m.Hooks {
					if e.Command == published {
						e.Command = resolved
						changed++
					}
					newHooks[j] = e
				}
				newMatchers[i] = Matcher{Matcher: m.Matcher, Hooks: newHooks}
			}
			out.Hooks[key] = newMatchers
		}
	}
	return out, changed
}
