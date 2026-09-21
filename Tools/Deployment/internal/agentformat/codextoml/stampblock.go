package codextoml

import (
	"bytes"

	"mosaic-deploy/internal/agentfields"
)

// stampblock.go implements the provenance stamp block writer and reader.
//
// On-disk format of the stamp block:
//
//	# mosaic_id: <value>
//	# mosaic_role: <value>
//	... (all stamps present, in agentfields.All() registry order)
//	            <- blank line separating the stamp block from the TOML keys
//
// One line-ending convention: "\n" (LF only).
// Malformed stamp-shaped lines in prior bytes are replaced by a freshly written
// block rather than carried through.
//
// The stamp key set and its fixed order come from agentfields.All(); the writer
// owns the block's formatting, not its vocabulary.

// writeStampBlock writes a stamp comment block from the given stamp map, in
// agentfields.All() registry order. Only stamps present in the map are written.
// Returns the raw bytes of the block (comment lines plus trailing blank line).
// If no stamps are present, returns nil (no blank line either).
func writeStampBlock(stamps map[string]string) []byte {
	if len(stamps) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, f := range agentfields.All() {
		if v, ok := stamps[f.Deployed]; ok {
			buf.WriteString("# ")
			buf.WriteString(f.Deployed)
			buf.WriteString(": ")
			buf.WriteString(v)
			buf.WriteByte('\n')
		}
	}
	if buf.Len() > 0 {
		buf.WriteByte('\n') // blank separator line after the stamp block
	}
	return buf.Bytes()
}

// readStampBlock reads the leading stamp comment lines from a Codex TOML byte
// slice. It is tolerant: blank lines and non-stamp comments in the header are
// skipped; only lines matching "# <deployed_name>: <value>" for known stamp
// keys are recorded. Reading stops at the first non-comment, non-blank line.
// Returns the stamp name-to-value map (Deployed names only) and the byte offset
// of the first non-header line.
func readStampBlock(b []byte) (stamps map[string]string, headerEnd int) {
	stamps = make(map[string]string)
	i := 0
	for i < len(b) {
		// Find the end of this line.
		eolIdx := bytes.IndexByte(b[i:], '\n')
		var line []byte
		var nextI int
		if eolIdx < 0 {
			// Last line with no trailing newline.
			line = b[i:]
			nextI = len(b)
		} else {
			line = b[i : i+eolIdx]
			nextI = i + eolIdx + 1
		}

		// Strip trailing CR (CRLF tolerance).
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		trimmed := bytes.TrimSpace(line)

		// Blank line: skip, advance.
		if len(trimmed) == 0 {
			i = nextI
			continue
		}

		// Non-comment line: end of header region.
		if trimmed[0] != '#' {
			return stamps, i
		}

		// Comment line: try to parse as "# <key>: <value>".
		rest := bytes.TrimSpace(trimmed[1:]) // everything after '#'
		colonIdx := bytes.IndexByte(rest, ':')
		if colonIdx > 0 {
			key := string(bytes.TrimSpace(rest[:colonIdx]))
			val := string(bytes.TrimSpace(rest[colonIdx+1:]))
			// Only record if the key is a known stamp Deployed name.
			for _, f := range agentfields.All() {
				if f.Deployed == key {
					stamps[key] = val
					break
				}
			}
		}

		i = nextI
	}
	return stamps, len(b)
}
