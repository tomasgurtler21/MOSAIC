package ghcptrust

import (
	"path/filepath"
	"strings"
)

// IsTrusted reports whether workDir equals, or is a descendant of, any entry.
// Matching is on path-segment boundaries; separators, trailing separators and
// dot segments are normalised, and case is folded when caseInsensitive is set.
func IsTrusted(workDir string, trustedFolders []string, caseInsensitive bool) bool {
	w := normalizePath(workDir, caseInsensitive)
	for _, entry := range trustedFolders {
		e := normalizePath(entry, caseInsensitive)
		if w == e {
			return true
		}
		prefix := e
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

func normalizePath(p string, caseInsensitive bool) string {
	p = filepath.Clean(filepath.FromSlash(p))
	if caseInsensitive {
		p = strings.ToLower(p)
	}
	return p
}
