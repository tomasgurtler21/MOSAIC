package registry

import "mosaic-deploy/internal/domain"

// BuiltinOptions carries the run context a built-in module needs at construction.
// It is passed to every BuiltinFactory when Discover invokes the registered built-in factories.
type BuiltinOptions struct {
	// MosaicRoot is the root of the MOSAIC repository. Built-in modules read their harness
	// content files from <MosaicRoot>/<their declared content directory>.
	MosaicRoot string
}

// BuiltinFactory constructs a built-in harness module for a run.
// Built-in module packages register a factory by calling Register from their init() function.
// The factory receives BuiltinOptions so it can locate its content files at run time.
//
// RED: Register currently accepts func() (domain.HarnessModule, error); this type is the
// target signature. I6.2 changes Register's parameter to accept BuiltinFactory.
type BuiltinFactory func(opts BuiltinOptions) (domain.HarnessModule, error)
