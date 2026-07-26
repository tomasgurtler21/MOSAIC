// Package catalog reads the MOSAIC repository tree once and exposes the full set of agents,
// skills, hook bundles, and workflows as typed domain values. It is the only package that opens
// source files for reading; every other package receives pre-loaded values or raw bytes from the
// catalog. It reports index/disk reconciliation mismatches and hook bundle integrity errors as
// structured issues rather than aborting on the first problem.
package catalog
