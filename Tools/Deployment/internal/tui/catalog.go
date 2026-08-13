package tui

import "mosaic-deploy/internal/app"

// CatalogReloadFunc rebuilds an app.Service against a different catalogue folder.
// The implementation is supplied by the composition root: it loads a catalogue from
// catalogFolder (with the unchanged MOSAIC root), rebuilds app.Deps around it — reusing the
// same Interaction ProgramRef, registry, logger, config stores, manifest store, and todo
// collector — and returns app.New(deps).
//
// It returns a non-nil error when the folder cannot be loaded. On error the caller must keep
// its existing service; a partially-built service is never returned alongside an error.
type CatalogReloadFunc func(catalogFolder string) (app.Service, error)
