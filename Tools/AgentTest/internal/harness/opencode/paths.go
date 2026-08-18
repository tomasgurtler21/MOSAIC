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
