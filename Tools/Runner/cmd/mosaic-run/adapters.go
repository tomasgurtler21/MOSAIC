package main

import (
	"path/filepath"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/ghcpcli"
	"mosaic-run/internal/harness/opencode"
	"mosaic-run/internal/harness/roleenv"
)

// newLoggedArtifactStore builds the run's artifact store and records a debug
// entry when the store path is anomalous. It is the single owner of the
// artifact.path.* event family: no other function in any package emits those
// events, and the artifact package itself never logs.
//
// Behaviour is identical to calling artifact.NewFileStore(path) directly — the
// returned store is always non-nil and no path is ever rewritten, substituted
// or rejected here. Emission is a pure side effect.
//
// At most one event is emitted per call; the two conditions are mutually
// exclusive so a log reader can distinguish a hard failure from an informational
// note by event name alone.
func newLoggedArtifactStore(path string, logger domain.DebugLogger) domain.ArtifactStore {
	if !filepath.IsAbs(path) {
		// Non-absolute path: every subsequent Create call will return an error
		// and nothing will be written. Record this so the failure is diagnosable.
		logger.Log(domain.EventArtifactPathRejected, "artifact store path is not absolute",
			domain.F("path", path))
	} else if !artifact.IsRunScopedArtifactPath(path) {
		// Absolute but not under an Orchestration-{run_id} folder: artifacts will
		// be written, but the path is outside the expected run-scoped hierarchy.
		logger.Log(domain.EventArtifactPathNonRunScoped, "artifact store path is not run-scoped",
			domain.F("path", path))
	}
	return artifact.NewFileStore(path)
}

// buildAdapter constructs the HarnessAdapter specified by harnessStr.
//
// When harnessStr is "claude-code", "opencode", or "ghcp-cli", the
// corresponding CLI adapter is created with execPathStr as the executable
// path and timeout as the invocation limit. A zero or negative timeout is
// treated as the default (30 minutes). execPathStr is the executable path
// override supplied via --executable-path; when empty, each harness uses its
// own per-harness default binary name.
// For any other value (including "fake" and unknown strings), MockAdapter is
// returned. Unknown values are not rejected here; cli.Run validates the
// --harness flag and surfaces usage errors for unknown values (AC3.8).
//
// ghcpMode selects the GHCP CLI permission strategy when harnessStr is
// "ghcp-cli". Accepted values are "blanket" and "allowlist". An empty or
// unrecognised value defaults to GHCPCLIModeBlanket (preserving pre-Stage-4
// behavior). The TUI path always supplies a resolved mode; the CLI path
// resolves it from --ghcp-permission-mode.
//
// An optional logger may be passed as the last argument. When provided, the
// CLI adapter is constructed with the logger so that invocation I/O is
// captured in the debug log. When omitted, the adapter uses a no-op logger.
// The fake adapter ignores the logger in all cases.
//
// runFolder is the absolute Orchestration-{run_id} folder of the run the adapter
// is bound to; "" or a non-run folder means no run id is supplied.
func buildAdapter(runFolder, harnessStr, execPathStr, ghcpMode string, timeout time.Duration, loggers ...domain.DebugLogger) domain.HarnessAdapter {
	var logger domain.DebugLogger = domain.NopDebugLogger{}
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	runOpts := runIDOptions(runFolder)
	switch harnessStr {
	case commonharness.HarnessIDClaudeCode:
		exe := execPathStr
		if exe == "" {
			exe = "claude"
		}
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		return claudecode.NewClaudeCodeAdapterWithLogger(exe, timeout, logger, runOpts...)
	case commonharness.HarnessIDOpenCode:
		exe := execPathStr
		if exe == "" {
			exe = "opencode"
		}
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		return withOpenCodeSafetyNet(opencode.NewOpenCodeAdapterWithLogger(exe, timeout, logger, runOpts...), runFolder, logger)
	case commonharness.HarnessIDGHCPCLI:
		exe := execPathStr
		if exe == "" {
			exe = "copilot"
		}
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		mode := commonharness.GHCPCLIPermissionMode(ghcpMode)
		if mode != commonharness.GHCPCLIModeBlanket && mode != commonharness.GHCPCLIModePartialAllowlist {
			mode = commonharness.GHCPCLIModeBlanket
		}
		return ghcpcli.NewGHCPCLIAdapterWithMode(exe, timeout, logger, mode, runOpts...)
	default: // "fake" or unknown
		return harness.NewMockAdapter()
	}
}

// runIDOptions returns the adapter option carrying the run id derived from
// runFolder, or no option when runFolder is empty or not a run folder.
func runIDOptions(runFolder string) []roleenv.Option {
	if runFolder == "" {
		return nil
	}
	runID, ok := domain.ParseRunFolder(filepath.Base(runFolder))
	if !ok {
		return nil
	}
	return []roleenv.Option{roleenv.WithRunID(runID)}
}

// realClock provides the current UTC time.
type realClock struct{}

func (c *realClock) Now() time.Time { return time.Now().UTC() }
