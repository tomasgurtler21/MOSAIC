package app

// service_impl.go is a test fixture representing a forbidden raw docformat.Parse
// call in the application layer. The read-boundary guard must reject any call to
// docformat.Parse from an application-layer file that is not the decode funnel
// (deployed_read.go) or the named generic-source operation.
//
// This file exists to prove that the guard FIRES for a raw-bytes parse call; it
// does not represent production code. Real application code routes all deployed-file
// decodes through the funnel layers in deployed_read.go.

import (
	"mosaic-common/docformat"
)

// parseRawBytes calls docformat.Parse directly on raw bytes from the application
// layer. This is the pattern the read-boundary guard must reject: a raw-bytes
// docformat entry point called from outside the two permitted files.
func parseRawBytes(src []byte) {
	_, _ = docformat.Parse(src)
}
