package agentformat

// translator.go is a test fixture representing a forbidden upward-dependency import
// in the translator layer. The translator-layer direction rule must reject any import
// of a harness package from internal/agentformat.
//
// The translator layer must depend only downward. It must never import a harness
// package, the application layer, transform, the frontends, or any infrastructure
// package. Importing a harness package from the translator layer inverts the
// dependency direction and creates coupling that the architecture is built to prevent.
//
// The import-boundary guard must reject this file with a message naming the file
// and the reason.

import (
	_ "mosaic-deploy/internal/harness/builtin/codex"
)
