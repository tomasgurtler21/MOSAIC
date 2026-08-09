package harness

// Harness identities. These constants are the identity half of the catalog
// entries below; they exist as named constants so a composition root's
// switch cases and a test's expectations name the same thing.
const (
	HarnessIDClaudeCode = "claude-code"
	HarnessIDOpenCode   = "opencode"
)

// CLIHarness is one CLI-backed harness this module can spawn through: its
// stable identity and how to name it to a person.
type CLIHarness struct {
	// ID is the stable identity a --harness flag accepts and a composition
	// root switches on. It never changes once shipped: it appears in scripts
	// and in stored run configuration.
	ID string

	// Label is the human-readable name a selection UI or a help text shows.
	// It is presentation only; nothing ever switches on it.
	Label string
}

// cliHarnesses is the catalog's single declaration of every CLI-backed
// harness this module can spawn through, in stable declared order.
//
// This is the single place a new CLI harness's identity is added. Adding an
// entry here is what makes it appear in every consumer's flag validation,
// usage text and selection UI; each consumer additionally maps the identity
// to a concrete adapter in its own composition root, and a coverage test in
// each consumer fails when that mapping is missed.
//
// The catalog answers "which harnesses exist and what are they called". It
// never answers "how is one constructed": each tool's adapters implement
// that tool's own port, whose types live in that tool's internal packages,
// which this module must not import.
//
// Every entry here is CLI-backed by construction — that is what the type's
// name says and what this list is for. A tool-local test double is not a
// CLI harness and has no entry; a tool that accepts one composes it into its
// own accepted set alongside these entries.
var cliHarnesses = []CLIHarness{
	{ID: HarnessIDClaudeCode, Label: "Claude Code CLI"},
	{ID: HarnessIDOpenCode, Label: "OpenCode CLI"},
}

// CLIHarnesses returns every CLI-backed harness this module can spawn
// through, in a stable order, as a fresh slice the caller may not corrupt
// for the next caller.
func CLIHarnesses() []CLIHarness {
	out := make([]CLIHarness, len(cliHarnesses))
	copy(out, cliHarnesses)
	return out
}

// LookupCLIHarness returns the entry for id. The second result distinguishes
// "no such harness" from a zero-valued entry; it never panics.
func LookupCLIHarness(id string) (CLIHarness, bool) {
	for _, e := range cliHarnesses {
		if e.ID == id {
			return e, true
		}
	}
	return CLIHarness{}, false
}

// IsCLIHarness reports whether id names a known CLI-backed harness.
func IsCLIHarness(id string) bool {
	_, ok := LookupCLIHarness(id)
	return ok
}
