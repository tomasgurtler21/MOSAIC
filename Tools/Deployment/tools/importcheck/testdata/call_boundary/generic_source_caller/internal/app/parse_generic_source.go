package app

// parse_generic_source.go is a test fixture representing the named generic-source
// parse operation, which is the second of exactly two files in the application
// layer permitted to call the raw-bytes docformat entry points.
//
// The read-boundary guard must ACCEPT calls to docformat.Parse from this file.
// The generic-source operation is the single entry point for parsing documents
// that are NOT deployed agent files: catalog sources, injection content,
// bundle-conformance inputs, render inputs, and source-model extraction. Its
// name states its purpose so that a reader and the guard can both tell it apart
// from a deployed-file read.
//
// This fixture uses the named function before the real tree contains it: the
// guard reads fixtures as text, so a fixture may name a function that does not
// yet exist in the production package.

import (
	"mosaic-common/docformat"
)

// ParseGenericSource is the named entry point for parsing a document that is
// NOT a deployed agent file. It is one of the two permitted callers of the raw
// docformat.Parse entry point in the application layer.
func ParseGenericSource(src []byte) {
	_, _ = docformat.Parse(src)
}
