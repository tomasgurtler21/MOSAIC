package opencode

// RepoContentDir is this harness's content directory relative to the MOSAIC root. It is
// declared explicitly because the directory name is not derivable from the descriptor id
// or display_name ("opencode" vs "OpenCode").
//
// Built-in module construction reads HarnessInjections.md and HarnessInjectionsOrchestrator.md
// from <MosaicRoot>/RepoContentDir at run time, so editing those files and rerunning the
// tool changes the injected content with no rebuild.
const RepoContentDir = "Catalog/Agents/OpenCode"
