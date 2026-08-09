// Package harness — Runner's accepted --harness value set.
//
// Runner accepts one value the shared catalog does not carry: "fake", a
// tool-local test double. This file composes the catalog's CLI-backed
// harnesses with that test double, once, for all four Runner surfaces that
// previously restated harness identity literals: the --harness flag's usage
// string, its validation, its usage-error message, and the TUI
// configuration screen's harness step.
package harness

import (
	"strings"

	commonharness "mosaic-common/harness"
)

// FakeHarnessID is Runner's tool-local test double. It is not a CLI harness
// and deliberately has no catalog entry: it spawns no process, and offering
// it as a harness in a selection UI would misrepresent it.
const FakeHarnessID = "fake"

// Selection is one accepted --harness value.
type Selection struct {
	ID    string
	Label string

	// CLIBacked distinguishes a real harness that spawns a CLI process from
	// a tool-local test double. A selection UI offers only CLI-backed
	// entries; the flag accepts both.
	CLIBacked bool
}

// Selections returns every value Runner's --harness flag accepts: the
// tool-local test double first (it is the flag's default), then every
// CLI-backed harness the shared catalog declares, in the catalog's order.
func Selections() []Selection {
	catalog := commonharness.CLIHarnesses()
	sels := make([]Selection, 0, len(catalog)+1)
	sels = append(sels, Selection{ID: FakeHarnessID, Label: "Fake (test double)", CLIBacked: false})
	for _, entry := range catalog {
		sels = append(sels, Selection{ID: entry.ID, Label: entry.Label, CLIBacked: true})
	}
	return sels
}

// Accepts reports whether id is an accepted --harness value.
func Accepts(id string) bool {
	if id == FakeHarnessID {
		return true
	}
	return commonharness.IsCLIHarness(id)
}

// CLISelections returns only the CLI-backed entries — what a selection UI
// offers, since the test double is a flag-only value.
func CLISelections() []Selection {
	catalog := commonharness.CLIHarnesses()
	sels := make([]Selection, 0, len(catalog))
	for _, entry := range catalog {
		sels = append(sels, Selection{ID: entry.ID, Label: entry.Label, CLIBacked: true})
	}
	return sels
}

// FlagValues renders the accepted identities for the flag's usage string,
// pipe-separated in accepted order: "fake|claude-code|opencode".
func FlagValues() string {
	ids := make([]string, 0, len(Selections()))
	for _, s := range Selections() {
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, "|")
}

// FlagValueList renders the accepted identities for the usage-error message,
// comma-separated in accepted order: "fake, claude-code, opencode".
func FlagValueList() string {
	ids := make([]string, 0, len(Selections()))
	for _, s := range Selections() {
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, ", ")
}
