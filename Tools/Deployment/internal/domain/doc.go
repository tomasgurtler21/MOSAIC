// Package domain holds every shared data type and the two port interfaces (HarnessModule and
// Interaction) for the MOSAIC deployment tool. It is the vocabulary every other package depends on.
// This package imports only standard library packages and must never import any other package
// within this module, preserving the acyclic dependency guarantee.
package domain
