// Package builtin is the parent namespace for built-in harness implementations. Each sub-package
// implements domain.HarnessModule for one specific harness and registers itself with the registry
// via an init() function. Built-in harnesses are compiled into the binary and require no external
// files beyond their embedded descriptor.
package builtin
