// Package opencode is the OpenCode harness adapter: identity, declared
// capabilities, spawn planning and envelope decoding. The interception facet
// (ConfigScopes, InspectScopes, Provision, Deprovision, TranslateCall,
// TranslateOutcome) is present so the type satisfies domain.HarnessAdapter at
// compile time, but is explicitly not implemented yet — a later pass
// replaces the stubs in stub.go with real behaviour.
//
// This file declares the harness's project-scoped layout as this adapter's
// own constants, mirroring internal/harness/claudecode's own paths.go:
// deliberately not shared with the deployment tool, since an adapter must
// know its harness's layout regardless and sharing would mean importing an
// entire descriptor model for a handful of short paths.
package opencode

import (
	"path/filepath"

	"mosaic-agent-test/internal/domain"
)

// This harness's project-scoped layout.
const (
	HarnessID      = "opencode"
	AgentsRelDir   = ".opencode/agents"
	PluginsRelDir  = ".opencode/plugins"
	ConfigFileName = "opencode.json"
)

// OpenCodeCLIExecutable names the product CLI this adapter spawns as the
// subject-under-test's own process. Resolving it, including the Windows
// .cmd/.bat shim case, is mosaic-common/harness's job at spawn time, not
// this adapter's.
const OpenCodeCLIExecutable = "opencode"

// ConfigHomeEnvVar is the environment variable this harness resolves its
// user-scope configuration directory from. Setting it in the spawn plan
// is what relocates both non-sandbox scopes into the run's sandbox,
// following the pattern the sibling adapter already establishes for its
// own configuration home.
const ConfigHomeEnvVar = "XDG_CONFIG_HOME"

// ConfigHomeRelDir is where the relocated configuration home lives,
// relative to the sandbox's control directory. It is under ControlDir and
// not under SubjectDir so the subject cannot discover it.
const ConfigHomeRelDir = "opencode-home"

// UserConfigDir returns this harness's user-scope configuration directory for
// sandbox s: the opencode subdirectory inside the relocated XDG_CONFIG_HOME
// the spawn plan sets. Setting ConfigHomeEnvVar in the spawn plan to
// filepath.Join(s.ControlDir, ConfigHomeRelDir) causes the spawned opencode
// process to read its user configuration from this directory rather than from
// the real user home.
//
// Pure path derivation; reads no environment and touches no filesystem.
func UserConfigDir(s domain.Sandbox) string {
	return filepath.Join(s.ControlDir, ConfigHomeRelDir, "opencode")
}
