package claudecode

import (
	"os"
	"strings"
)

// readTranscriptFile is the real Options.TranscriptReader: it reads the
// collaborator transcript file at path and returns its non-empty lines, one
// entry per line, in file order. A missing or unreadable file is returned
// as an error, which recoverCompletionToken treats as "no token
// recoverable" rather than a translation failure.
func readTranscriptFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	entries := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "" {
			continue
		}
		entries = append(entries, trimmed)
	}
	return entries, nil
}
