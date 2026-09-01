package main

// catalog_folder_dispatch_test.go covers Stage 4's deployer factory threading
// in the composition root, and the environmentBakedPreflight pass-through
// contract for CatalogFolder:
//
//   When the plan's resolved catalog folder differs from the process-wide
//   default, composedSuiteRunner.Run calls Deps.NewDeployer with the override
//   value to obtain a per-run deployer. When the catalog folders match, the
//   factory is not called and the default deployer is used.
//
//   environmentBakedPreflight overwrites in.Environment, in.Deploy, and
//   in.DeployScratchRoot, but must NOT overwrite in.CatalogFolder — that
//   value is caller-supplied and must pass through to preflight.Validate
//   unchanged.
//
// These tests reference fields not yet in the implementation:
//   - Deps.NewDeployer  (not yet on Deps struct)
//   - Deps.CatalogFolder (not yet on Deps struct)
//   - preflight.Input.CatalogFolder (not yet on Input struct)
//   - preflight.Plan.CatalogFolder (not yet on Plan struct)
//
// They will fail to compile until the implementation adds those fields, which
// is the expected RED state for TDD.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/workspace"
)

// ---------------------------------------------------------------------------
// catalogFactoryDeployer: a deployer returned by the factory, distinct from
// fakeDeployer so tests can confirm which deployer was used.
// ---------------------------------------------------------------------------

// catalogFactoryDeployer is a distinct type for the deployer the factory
// returns, used only in this file. Its identity as a value differs from
// fakeDeployer, allowing tests to verify the factory was actually invoked
// (as opposed to the default deployer being passed through unchanged).
type catalogFactoryDeployer struct {
	calledWith string // the catalogFolder the factory was called with
}

func (d *catalogFactoryDeployer) Render(_ context.Context, _ domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
	return domain.RenderAgentResult{}, nil
}

func (d *catalogFactoryDeployer) Deploy(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
	return domain.DeployResult{}, nil
}

// ---------------------------------------------------------------------------
// minimalDepsForRunnerTest builds a Deps with just enough fields populated to
// let composedSuiteRunner.Run complete with an empty plan (0 tests) without
// panicking. The new fields Deps.NewDeployer and Deps.CatalogFolder are added
// by the caller.
// ---------------------------------------------------------------------------

func minimalDepsForCatalogTest(t *testing.T, defaultCatalog string, factory func(string) domain.AgentDeployer) Deps {
	t.Helper()
	return Deps{
		Clock:         fakeClock{},
		Deploy:        fakeDeployer{},
		CatalogFolder: defaultCatalog, // NEW field — compile error until added
		NewDeployer:   factory,        // NEW field — compile error until added
	}
}

// ---------------------------------------------------------------------------
// T4.3c: NewDeployer called when catalog folder override differs from default
// ---------------------------------------------------------------------------

// TestComposedSuiteRunner_CatalogFolderOverride_CallsNewDeployer verifies that
// when plan.CatalogFolder differs from deps.CatalogFolder (the process-wide
// default), composedSuiteRunner.Run calls the NewDeployer factory with the
// plan's catalog folder value.
func TestComposedSuiteRunner_CatalogFolderOverride_CallsNewDeployer(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"
	const overrideCatalog = "C:/override/catalog"

	var factoryCalled bool
	var gotCatalog string

	factory := func(catalogFolder string) domain.AgentDeployer {
		factoryCalled = true
		gotCatalog = catalogFolder
		return &catalogFactoryDeployer{calledWith: catalogFolder}
	}

	d := minimalDepsForCatalogTest(t, defaultCatalog, factory)
	ws := workspace.NewManager(t.TempDir(), fakeClock{})
	runner := composedSuiteRunner{deps: d, ws: ws, retention: domain.RetainNever}

	plan := preflight.Plan{
		CatalogFolder: overrideCatalog, // NEW field — compile error until added
		// No tests: suite completes immediately without invoking deployer.
	}

	_, _ = runner.Run(context.Background(), plan, fakeProgressSink{})

	if !factoryCalled {
		t.Error("NewDeployer was not called when plan.CatalogFolder differs from deps.CatalogFolder; the factory must be invoked to build a per-run deployer for the override")
	}
	if gotCatalog != overrideCatalog {
		t.Errorf("NewDeployer called with %q, want %q (the factory must receive the plan's override catalog folder, not the default)", gotCatalog, overrideCatalog)
	}
}

// TestComposedSuiteRunner_CatalogFolderDefault_DoesNotCallNewDeployer verifies
// that when plan.CatalogFolder equals deps.CatalogFolder (no override), the
// NewDeployer factory is NOT called. The default deployer (Deps.Deploy) is
// used as-is without constructing a new one.
func TestComposedSuiteRunner_CatalogFolderDefault_DoesNotCallNewDeployer(t *testing.T) {
	const sharedCatalog = "C:/default/catalog"

	factoryCalled := false
	factory := func(catalogFolder string) domain.AgentDeployer {
		factoryCalled = true
		return &catalogFactoryDeployer{calledWith: catalogFolder}
	}

	d := minimalDepsForCatalogTest(t, sharedCatalog, factory)
	ws := workspace.NewManager(t.TempDir(), fakeClock{})
	runner := composedSuiteRunner{deps: d, ws: ws, retention: domain.RetainNever}

	plan := preflight.Plan{
		CatalogFolder: sharedCatalog, // same as default: no per-run deployer needed
	}

	_, _ = runner.Run(context.Background(), plan, fakeProgressSink{})

	if factoryCalled {
		t.Error("NewDeployer was called when plan.CatalogFolder equals deps.CatalogFolder; the factory must NOT be called when no override is in effect (the existing default deployer must be reused)")
	}
}

// ---------------------------------------------------------------------------
// environmentBakedPreflight pass-through: CatalogFolder is not overwritten
// ---------------------------------------------------------------------------

// TestEnvironmentBakedPreflight_DoesNotOverwriteCatalogFolder verifies that
// the environmentBakedPreflight closure passes in.CatalogFolder through to
// preflight.Validate unchanged. The closure explicitly overwrites in.Environment,
// in.Deploy, and in.DeployScratchRoot (those are its whole purpose), but must
// NOT touch in.CatalogFolder — that value is caller-supplied (from a flag,
// env var, or TUI edit) and is not an environment-baked value.
//
// If an implementer incorrectly adds "in.CatalogFolder = ..." to the closure,
// this test catches it: the plan would carry the wrong (empty) value instead
// of the one the caller set.
//
// A non-existent SuitePath is deliberate: Plan.CatalogFolder is resolved as
// the very first operation inside Validate, before the suite-file read, so
// it is populated even when Validate returns early with a missing-suite
// diagnostic.
func TestEnvironmentBakedPreflight_DoesNotOverwriteCatalogFolder(t *testing.T) {
	const wantCatalog = "C:/caller/catalog"

	// Use the real closure — not a mock. Only this tests the actual statement
	// "environmentBakedPreflight does not assign in.CatalogFolder".
	validate := environmentBakedPreflight(domain.EnvironmentReport{}, nil, "")

	plan, _ := validate(preflight.Input{
		SuitePath:     "nonexistent.yaml", // early return, avoids filesystem I/O
		CatalogFolder: wantCatalog,        // NEW field on Input — compile error until added
	})

	if plan.CatalogFolder != wantCatalog {
		t.Errorf("plan.CatalogFolder = %q, want %q; environmentBakedPreflight must not overwrite in.CatalogFolder (it is caller-supplied, not environment-baked)", plan.CatalogFolder, wantCatalog)
	}
}
