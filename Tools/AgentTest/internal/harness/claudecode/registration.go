package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Settings is the registration document as a model. Composition operates on
// this; text splicing of the published fragment is forbidden, because the
// single-rewriter invariant is only expressible over a parsed model and
// determinism is only assertable over a re-serialized one.
type Settings struct {
	Hooks map[string][]Matcher `json:"hooks,omitempty"`

	// Other preserves top-level keys this model does not interpret, so a
	// round trip through the model is lossless. In a freshly created sandbox
	// it is empty by construction; it exists so the model stays honest
	// rather than because setup expects to find anything there.
	Other map[string]json.RawMessage `json:"-"`
}

// Matcher is one selector plus the entries it activates.
type Matcher struct {
	Matcher string  `json:"matcher,omitempty"`
	Hooks   []Entry `json:"hooks"`
}

// Entry is one registered command.
//
// Async and Timeout are pointers because their absence is meaningful: the
// bundle registers one event synchronously and the other eleven
// asynchronously, and that asymmetry is load-bearing — it is what makes the
// logger observational rather than a second rewriter. Composition preserves
// each entry's flags exactly; it never normalizes them.
type Entry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Async   *bool    `json:"async,omitempty"`
	Timeout *int     `json:"timeout,omitempty"`
}

// Contribution is one source's registration, tagged with whether its entries
// rewrite the intercepted call's input. The tag is carried rather than
// inferred: whether an entry rewrites input is a property of what the
// command behind it does, and no amount of inspecting the entry reveals it.
type Contribution struct {
	Source        string // "interceptor", or the bundle id for a bundle fragment
	RewritesInput bool
	Settings      Settings
}

var (
	ErrMultipleRewriters = errors.New("claudecode: more than one registration rewrites the intercepted call's input")
	ErrFragmentMalformed = errors.New("claudecode: registration fragment malformed")
)

// ParseFragment decodes a bundle's published registration fragment into the
// registration model. Composition is structural: the fragment is parsed, the
// interceptor's entries are generated into the same model, and the two are
// combined — never spliced as text.
func ParseFragment(raw string) (Settings, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return Settings{}, fmt.Errorf("%w: %v", ErrFragmentMalformed, err)
	}

	s := Settings{}
	for key, val := range top {
		if key == "hooks" {
			var hooks map[string][]Matcher
			if err := json.Unmarshal(val, &hooks); err != nil {
				return Settings{}, fmt.Errorf("%w: hooks: %v", ErrFragmentMalformed, err)
			}
			s.Hooks = hooks
			continue
		}
		if s.Other == nil {
			s.Other = map[string]json.RawMessage{}
		}
		s.Other[key] = val
	}
	return s, nil
}

// Compose combines contributions per event key.
//
// Ordering is decided, not incidental: within an event key, entries from
// rewriting contributions come first, observational entries after, and
// within a contribution the authored order is preserved. Event keys present
// in only one contribution survive intact, including each entry's
// synchronous or asynchronous flag and its timeout — the bundle's
// synchronous/asynchronous asymmetry is meaningful and must not be
// normalized away.
//
// Compose fails with ErrMultipleRewriters when more than one contribution
// declares RewritesInput for the same event key. Because composition holds
// every entry that will exist in the sandbox at one moment, this is a direct
// check rather than an assumption, and it fires before any agent is
// spawned. The error names every competing contribution.
func Compose(contribs ...Contribution) (Settings, error) {
	rewritersByKey := map[string][]string{}
	for _, c := range contribs {
		if !c.RewritesInput {
			continue
		}
		for key := range c.Settings.Hooks {
			rewritersByKey[key] = append(rewritersByKey[key], c.Source)
		}
	}
	for key, sources := range rewritersByKey {
		if len(sources) > 1 {
			return Settings{}, fmt.Errorf("%w: event %q: %s", ErrMultipleRewriters, key, strings.Join(sources, ", "))
		}
	}

	keySet := map[string]bool{}
	for _, c := range contribs {
		for key := range c.Settings.Hooks {
			keySet[key] = true
		}
	}

	composed := Settings{Hooks: make(map[string][]Matcher, len(keySet))}
	for key := range keySet {
		var blocks []Matcher
		// Rewriting contributions first, in the order they were passed.
		for _, c := range contribs {
			if !c.RewritesInput {
				continue
			}
			if mb, ok := c.Settings.Hooks[key]; ok {
				blocks = append(blocks, mb...)
			}
		}
		// Observational contributions after, in the order they were passed.
		for _, c := range contribs {
			if c.RewritesInput {
				continue
			}
			if mb, ok := c.Settings.Hooks[key]; ok {
				blocks = append(blocks, mb...)
			}
		}
		composed.Hooks[key] = blocks
	}
	return composed, nil
}

// Marshal serializes the composed model exactly once, byte-deterministically
// for a given bundle version: event keys in sorted order, two-space
// indentation, a trailing newline, and no re-encoding of any entry's fields
// beyond what the model carries. Byte determinism is what makes the
// generated configuration golden-file testable, which matters because this
// is the most failure-prone step of setup.
//
// Determinism relies on encoding/json's own guarantee that a map with string
// keys is always marshaled with its keys in sorted order — event keys here,
// and the top-level keys carrying Other alongside "hooks".
func Marshal(s Settings) ([]byte, error) {
	top := make(map[string]json.RawMessage, len(s.Other)+1)
	for k, v := range s.Other {
		top[k] = v
	}
	if len(s.Hooks) > 0 {
		hooksJSON, err := json.Marshal(s.Hooks)
		if err != nil {
			return nil, fmt.Errorf("claudecode: marshaling hooks: %v", err)
		}
		top["hooks"] = hooksJSON
	}

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("claudecode: marshaling settings: %v", err)
	}
	out = append(out, '\n')
	return out, nil
}
