package claudecode

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"mosaic-agent-test/internal/domain"
)

// versionProbeTimeout bounds the harness CLI version query so a slow or
// hanging CLI can never stall a run.
const versionProbeTimeout = 10 * time.Second

// versionMemo holds the first successfully captured harness version for the
// adapter's lifetime. A failed capture is not remembered, so a later call
// may still succeed.
type versionMemo struct {
	mu      sync.Mutex
	version string
}

// ParseHarnessVersion extracts the version from `claude --version` output.
// It returns the first whitespace-delimited token of the first non-empty
// line when that token is a dotted numeric version (e.g. "2.1.284" from
// "2.1.284 (Claude Code)"), and "" otherwise. Pure.
func ParseHarnessVersion(output []byte) string {
	for _, line := range bytes.Split(output, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) == 0 {
			continue
		}
		if isDottedNumeric(fields[0]) {
			return fields[0]
		}
		return ""
	}
	return ""
}

// isDottedNumeric reports whether s is two or more non-empty all-digit
// components separated by dots.
func isDottedNumeric(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// HarnessVersion implements domain.HarnessVersionReporter. Any failure of
// the probe yields "" so version capture can never fail a run.
func (a *Adapter) HarnessVersion(ctx context.Context) string {
	a.version.mu.Lock()
	defer a.version.mu.Unlock()
	if a.version.version != "" {
		return a.version.version
	}

	ctx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()

	probe := a.opts.VersionProbe
	if probe == nil {
		probe = runVersionCommand
	}
	out, err := probe(ctx)
	if err != nil {
		return ""
	}
	v := ParseHarnessVersion(out)
	a.version.version = v
	return v
}

// runVersionCommand runs `claude --version`.
func runVersionCommand(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, ClaudeCLIExecutable, "--version").Output()
}

var _ domain.HarnessVersionReporter = (*Adapter)(nil)
