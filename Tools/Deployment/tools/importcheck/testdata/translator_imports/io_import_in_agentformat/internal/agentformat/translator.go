package agentformat

// translator.go is a test fixture representing a forbidden I/O import in the
// translator layer. The translator-layer purity rule must reject any import of
// "os" (or other I/O, clock, or randomness packages) from internal/agentformat.
//
// The translator layer must be a pure function: no filesystem, network, terminal,
// time, or randomness. This is what makes byte-exact round-trip testing possible
// and prevents silent non-determinism from entering the format translation path.
//
// The import-boundary guard must reject this file with a message naming the file
// and the reason.

import (
	"os"
)

// stdoutHandle holds os.Stdout to make the "os" import used and satisfy the Go
// compiler. This is a fixture file; it is never compiled against the real module.
var stdoutHandle = os.Stdout
