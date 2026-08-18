// Package domain is the base vocabulary layer of mosaic-log-analyzer.
// It defines every type, value object, and port interface the rest of the
// module depends on. The domain package imports nothing from this module;
// all other packages may only reach inward toward domain, never outward.
// This guarantees the invariants of the hexagonal architecture: the core
// vocabulary is free of infrastructure concerns and can be exercised by
// pure unit tests without any I/O setup.
package domain
