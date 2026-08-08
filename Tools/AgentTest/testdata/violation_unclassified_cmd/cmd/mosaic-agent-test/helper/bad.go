// Package helper is a fixture proving the composition-root exemption is
// narrow: it sits beneath cmd/mosaic-agent-test but is not the composition
// root package itself, so it must still be classified (and rejected as
// unclassified) rather than silently inheriting the root's exemption
// because it shares a path prefix with it.
package helper

var Marker = 1
