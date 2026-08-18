// Package main is a conforming fixture standing in for the real binary
// composition root: it is the one package in this fixture module allowed to
// import a concrete harness adapter directly.
package main

import "mosaic-agent-test/internal/harness/claudecode"

var _ = claudecode.Adapter{}

func main() {}
