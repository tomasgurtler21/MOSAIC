// Package testrun orchestrates the full test execution flow for Runner test
// automation: deploy the catalog, run selected tests sequentially per harness,
// check results, and produce a structured summary.
//
// The orchestrator ties together the catalog reader (testcatalog), deploy
// invoker (testdeploy), and result checker (testcheck). All external
// dependencies are injected as interfaces so unit tests can use fakes without
// touching real subprocesses or the filesystem.
//
// This package sits at the cli/tui peer layer. It must not be imported by
// session, engine, or domain.
package testrun
