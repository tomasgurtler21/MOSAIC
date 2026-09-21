package formatid

import "errors"

// ID is a stable, published agent file-format identifier. Its values appear as
// descriptor data (agent_format_id:), so they are a public vocabulary, not an
// internal detail. The two pinned string values below appear verbatim in the
// Codex descriptor; changing them would invalidate the descriptor and its schema tests.
type ID string

const (
	// Markdown is the default format: the identity translator.
	// Pinned value: "markdown". Any descriptor omitting agent_format_id is treated as Markdown.
	Markdown ID = "markdown"

	// CodexTOML identifies Codex standalone TOML agent files.
	// Pinned value: "codex-toml". This string appears as agent_format_id: "codex-toml"
	// in the Codex descriptor.
	CodexTOML ID = "codex-toml"
)

// ErrUnknownFormatID is returned by Parse for a string outside the vocabulary.
var ErrUnknownFormatID = errors.New("unknown agent format id")

// All returns every known format ID, in declaration order. It is the authority the
// descriptor validator and the registry-equality test both read.
func All() []ID {
	return []ID{Markdown, CodexTOML}
}

// IsKnown reports whether id is a member of All().
func IsKnown(id ID) bool {
	for _, known := range All() {
		if id == known {
			return true
		}
	}
	return false
}

// Parse converts a descriptor-supplied string into an ID. An empty string yields
// Markdown (CD-4: an absent agent_format_id means Markdown). An unrecognised string
// yields ErrUnknownFormatID.
func Parse(s string) (ID, error) {
	if s == "" {
		return Markdown, nil
	}
	id := ID(s)
	if IsKnown(id) {
		return id, nil
	}
	return "", ErrUnknownFormatID
}
