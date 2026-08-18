package main

// tui_wiring_test.go verifies that the composition root correctly wires the catalogue folder
// and reload capability into tui.Options. These tests exercise buildReloadFunc directly —
// the function that constructs the CatalogReloadFunc closure passed to the TUI.
//
// Without this wiring the catalogue folder screen is never shown (Options.CatalogFolder
// remains at its zero value "") and confirming a different folder has no effect
// (Options.ReloadCatalog is nil).

import (
	"runtime"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildTestDeps constructs a minimal but real app.Deps against the given mosaicRoot and
// catalogFolder. It mirrors the dep-construction sequence in main() so that buildReloadFunc
// is exercised with the same kind of deps the production binary uses.
func buildTestDeps(t *testing.T, mosaicRoot, catalogFolder string) app.Deps {
	t.Helper()

	toolCfgStore := config.NewToolConfigStore(mosaicRoot)
	toolCfg, err := toolCfgStore.Load()
	if err != nil {
		t.Fatalf("load tool config: %v", err)
	}

	userCfgStore := config.NewUserConfigStore(mosaicRoot)
	if _, err := userCfgStore.Load(); err != nil {
		t.Fatalf("load user config: %v", err)
	}

	logger := logging.New(mosaicRoot, toolCfg)

	cat, err := catalog.Load(mosaicRoot, catalogFolder)
	if err != nil {
		t.Fatalf("catalog.Load(%q, %q): %v", mosaicRoot, catalogFolder, err)
	}

	reg, err := registry.Discover(registry.Options{
		MosaicRoot: mosaicRoot,
		GOOS:       runtime.GOOS,
	})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}

	todoCollector := todo.NewCollector()
	manifestStore := manifest.NewStore()

	return app.Deps{
		Catalog:        cat,
		Registry:       reg,
		Planner:        plan.New(),
		Executor:       deploy.NewExecutor(manifestStore, logger, todoCollector),
		Manifest:       manifestStore,
		ToolConfig:     toolCfgStore,
		UserConfig:     userCfgStore,
		Logger:         logger,
		Todo:           todoCollector,
		MosaicRoot:     mosaicRoot,
		GOOS:           runtime.GOOS,
		ProtocolLoader: catalog.FileProtocolLoader{},
		BundleLoader:   catalog.FileBundleLoader{},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestBuildReloadFunc_ReturnsNonNilFunc verifies that buildReloadFunc returns a non-nil
// function. A nil return would mean tui.Options.ReloadCatalog is nil, which silently
// disables the catalogue reload capability for the entire TUI session.
func TestBuildReloadFunc_ReturnsNonNilFunc(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	catalogFolder, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Fatalf("resolveCatalogFolder: %v", err)
	}

	deps := buildTestDeps(t, mosaicRoot, catalogFolder)
	reloadFunc := buildReloadFunc(mosaicRoot, deps)

	if reloadFunc == nil {
		t.Fatal("buildReloadFunc returned nil; " +
			"main.go must supply a non-nil CatalogReloadFunc to tui.Options.ReloadCatalog so " +
			"that confirming a different catalogue folder in the TUI actually reloads the catalogue")
	}
}

// TestBuildReloadFunc_ValidFolder_ReturnsNonNilService verifies that the reload function
// returned by buildReloadFunc produces a non-nil service when called with a valid catalogue
// folder. This is the core contract: the TUI calls this function when the user confirms a
// different folder, and the returned service must be ready for use.
func TestBuildReloadFunc_ValidFolder_ReturnsNonNilService(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	catalogFolder, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Fatalf("resolveCatalogFolder: %v", err)
	}

	deps := buildTestDeps(t, mosaicRoot, catalogFolder)
	reloadFunc := buildReloadFunc(mosaicRoot, deps)

	newSvc, err := reloadFunc(catalogFolder)
	if err != nil {
		t.Fatalf("reloadFunc(%q): %v; "+
			"calling the reload function with a valid catalogue folder must not return an error",
			catalogFolder, err)
	}
	if newSvc == nil {
		t.Fatal("reloadFunc returned nil service; " +
			"a valid catalogue folder must produce a working service for the remainder of the session")
	}
}

// TestBuildReloadFunc_ReturnsDistinctService verifies that calling the reload function with the
// same catalogue folder returns a distinct service instance (not the same pointer as baseDeps).
// The TUI adopts the returned service for the remainder of the session; the function must
// produce a new service, not return the existing one.
func TestBuildReloadFunc_ReturnsDistinctService(t *testing.T) {
	mosaicRoot := makeE2EMosaicRoot(t)
	catalogFolder, err := resolveCatalogFolder(mosaicRoot, "")
	if err != nil {
		t.Fatalf("resolveCatalogFolder: %v", err)
	}

	deps := buildTestDeps(t, mosaicRoot, catalogFolder)
	originalSvc := app.New(deps)
	reloadFunc := buildReloadFunc(mosaicRoot, deps)

	newSvc, err := reloadFunc(catalogFolder)
	if err != nil {
		t.Fatalf("reloadFunc(%q): %v", catalogFolder, err)
	}

	if newSvc == originalSvc {
		t.Error("reloadFunc returned the same service pointer as the original; " +
			"the reload function must construct a new service with the newly-loaded catalogue, " +
			"not return the pre-existing service")
	}
}
