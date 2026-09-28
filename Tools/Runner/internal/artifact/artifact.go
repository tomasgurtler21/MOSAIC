// Package artifact implements the ArtifactStore port for Orchestration.md.
// It reads, constructs, and atomically rewrites Orchestration.md in the canonical
// format defined in Development/Designs/OrchestrationArtifactFormat.md.
//
// Parsing and rendering are exposed as package-level functions so that tests can
// verify each step independently of file I/O.
package artifact
