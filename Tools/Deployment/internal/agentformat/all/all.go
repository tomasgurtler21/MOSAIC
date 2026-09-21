// Package all imports all registered translator implementations so that their
// init() functions run and register translators with the agentformat registry.
// Importing this package is what makes agentformat.Lookup resolve for all known
// formats beyond Markdown.
//
// The Markdown identity translator lives in agentformat itself and self-registers
// there; it does not need to be listed here. This package exists for sub-packages
// (codextoml and any future translator) that cannot self-register through agentformat
// without creating an import cycle.
//
// Usage:
//   - Production code: cmd/mosaic-deploy/main.go blank-imports this package beside
//     the four built-in harness blank imports. That is the non-test owner.
//   - Tests outside the composition root that need a resolvable Codex translator:
//     add _ "mosaic-deploy/internal/agentformat/all" to the test file's imports.
//     Do NOT blank-import internal/agentformat/codextoml directly: that bypasses
//     this wiring package and would keep a broken wiring package test green.
package all

import (
	_ "mosaic-deploy/internal/agentformat/codextoml"
)
