// Command mosaic-deploy is the entry point for the MOSAIC deployment tool.
//
// When a subcommand is present in the arguments (deploy or update), the CLI adapter handles
// the invocation non-interactively. When no subcommand is present and a terminal is attached
// to both stdin and stdout, the TUI frontend is launched instead. This is also what happens
// when the binary is double-clicked from Explorer: Windows attaches a fresh console with a
// real stdin/stdout, so isatty is true and the TUI takes over.
//
// Dependency construction (catalog, registry, manifest, config, logging, todo) is done here
// once, before dispatching to a frontend. Neither the CLI nor TUI packages construct their
// own infrastructure; they receive a fully-wired app.Service.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/mattn/go-isatty"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/pathinput"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
	"mosaic-deploy/internal/tui"

	commonharness "mosaic-common/harness"

	// Import built-in harnesses so their init() functions run and register
	// themselves with the package-level registry before Discover is called.
	_ "mosaic-deploy/internal/harness/builtin/claudecode"
	_ "mosaic-deploy/internal/harness/builtin/ghcpcli"
	_ "mosaic-deploy/internal/harness/builtin/opencode"
	_ "mosaic-deploy/internal/harness/builtin/vscodeghcp"
)

// wantsTUI reports whether mosaic-deploy should launch the interactive TUI: no subcommand
// was given, and both stdin and stdout are attached to a real terminal (not a pipe/redirect).
func wantsTUI(args []string) bool {
	if len(args) != 0 {
		return false
	}
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// isFreshConsole reports whether mosaic-deploy is running in a freshly-spawned
// console with no parent terminal — the typical double-click scenario on Windows.
// When this is true, the console window closes as soon as the process exits, so
// any error message printed to stderr would disappear before the user has a chance
// to read it.
//
// The heuristic reuses wantsTUI's conditions (no subcommand given and stdin/stdout
// are real terminals), restricted to Windows where the auto-closing console problem
// applies. On non-Windows platforms this always returns false because POSIX terminals
// and terminal emulators do not close automatically on process exit.
func isFreshConsole(args []string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return wantsTUI(args)
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 && !wantsTUI(args) {
		fmt.Fprintln(os.Stderr, "usage: mosaic-deploy <deploy|update> [flags]")
		os.Exit(cli.ExitUsage)
	}

	// Step 1: Pre-scan global flags needed for dependency wiring. cobra will
	// re-parse these when cli.Run calls root.Execute; the two parses agree.
	mosaicRoot, catalogFolderFlag, allowExternal := scanGlobalFlags(args)

	// Step 2: Resolve the MOSAIC root from the working directory when the flag
	// is not provided explicitly.
	if mosaicRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot determine working directory: %v\n", err)
			os.Exit(cli.ExitFailure)
		}
		resolved, err := catalog.ResolveRoot(wd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: mosaic-deploy must be run from the MOSAIC repository root: %v\n", err)
			if isFreshConsole(args) {
				fmt.Fprint(os.Stderr, "Press Enter to exit...")
				fmt.Scanln() //nolint:errcheck
			}
			os.Exit(cli.ExitFailure)
		}
		mosaicRoot = resolved
	}

	// Step 2b: Resolve and validate the catalogue folder. When --catalog-folder is
	// absent, the default {mosaic-root}/Catalog is used without validation. When
	// it is explicitly supplied, the path is made absolute and validated fail-fast
	// before any further dependency construction.
	catalogFolder, err := resolveCatalogFolder(mosaicRoot, catalogFolderFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 3: Load project-level tool configuration. Absent file yields documented
	// defaults (no error); other I/O errors are fatal.
	toolConfigStore := config.NewToolConfigStore(mosaicRoot)
	toolCfg, err := toolConfigStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load tool config: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 3b: Load user-local tool configuration. Absent file yields an empty
	// config (no error); other I/O errors are fatal. Loaded here so that the
	// ToolMappings hook below can capture both config stores at registry discovery
	// time, before the app.Service is constructed.
	userConfigStore := config.NewUserConfigStore(mosaicRoot)
	userCfg, err := userConfigStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load user config: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 4: Construct the logger before catalog/registry (StartRun is called
	// inside the app layer per run, not here).
	logger := logging.New(mosaicRoot, toolCfg)

	// Step 5: Load the MOSAIC source catalog. The resolved catalogFolder is the
	// exclusive source for agents and workflows; bundle and protocol always come
	// from mosaicRoot.
	cat, err := catalog.Load(mosaicRoot, catalogFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load catalog: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 6: Discover available harnesses. External modules are allowed when
	// either the command-line flag or the tool config enables them. The ToolMappings
	// hook wires config-declared tool-destination mappings into each harness module's
	// descriptor at construction time, so that destination fields are emitted on every
	// deploy and update without any further module-level knowledge of config stores.
	// The ModelCatalog hook overlays the shared catalog's model data onto the three
	// CLI-backed harnesses, so Descriptor().Models reflects the single source of truth
	// for model IDs and format hints; any harness the shared catalog does not know keeps
	// its own descriptor-sourced catalog unchanged.
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:    mosaicRoot,
		AllowExternal: allowExternal || toolCfg.AllowExternalModules,
		GOOS:          runtime.GOOS,
		ToolMappings: func(harnessID string, declared []domain.ToolMapping) []domain.ToolMapping {
			return config.EffectiveToolMappings(harnessID, declared, toolCfg.ToolDestinations, userCfg.ToolDestinations)
		},
		ModelCatalog: func(harnessID string, declared domain.ModelCatalog) domain.ModelCatalog {
			cat, ok := commonharness.LookupModelCatalog(harnessID)
			if !ok {
				return domain.ModelCatalog{} // miss → keep descriptor's own catalog
			}
			return domain.ModelCatalog{IDs: cat.IDs, FormatHint: cat.FormatHint}
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: discover harnesses: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 7: Construct the remaining single-instance dependencies. The todo
	// collector is shared between the Interaction port and the executor so
	// both sinks write to the same checklist.
	todoCollector := todo.NewCollector()
	manifestStore := manifest.NewStore()

	// The Interaction port differs by frontend: the TUI needs a ProgramRef so the
	// service's blocking calls can round-trip through the Bubble Tea program; the
	// CLI needs the non-interactive, pre-answers-only implementation. It is set
	// below, per branch, before app.New(deps) is called.
	deps := app.Deps{
		Catalog:        cat,
		Registry:       reg,
		Planner:        plan.New(),
		Executor:       deploy.NewExecutor(manifestStore, logger, todoCollector),
		Manifest:       manifestStore,
		ToolConfig:     toolConfigStore,
		UserConfig:     userConfigStore,
		Logger:         logger,
		Todo:           todoCollector,
		MosaicRoot:     mosaicRoot,
		GOOS:           runtime.GOOS,
		ProtocolLoader: catalog.FileProtocolLoader{},
		BundleLoader:   catalog.FileBundleLoader{},
	}

	if wantsTUI(args) {
		ref := tui.NewInteraction()
		deps.Interaction = ref
		reloadFunc := buildReloadFunc(mosaicRoot, deps)
		if err := tui.Run(context.Background(), app.New(deps), tui.Options{
			Interaction:   ref,
			CatalogFolder: catalogFolder,
			ReloadCatalog: reloadFunc,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(cli.ExitFailure)
		}
		os.Exit(cli.ExitSuccess)
	}

	preAnswers := buildPreAnswers(args)
	deps.Interaction = cli.NewInteraction(preAnswers, todoCollector, os.Stdout)

	// Step 8: Dispatch to the CLI adapter with a fully-wired service. cli.Run
	// handles all subcommand routing and output formatting; main.go is purely
	// responsible for dependency construction and process exit.
	code := cli.Run(context.Background(), args, app.New(deps), os.Stdout, os.Stderr)
	os.Exit(code)
}

// scanGlobalFlags does a minimal pre-scan of args for the global flags that
// influence dependency wiring: --mosaic-root, --catalog-folder, and --allow-external.
// cobra will re-parse all flags when cli.Run executes; this pre-scan exists only so
// that main.go can construct app.New(deps) before handing control to cli.Run.
//
// catalogFolder is the raw user-supplied value, or "" when the flag is absent.
// Defaulting to {mosaic-root}/Catalog is done by resolveCatalogFolder, not here.
//
// The scan understands both --flag value and --flag=value forms. It does not
// validate other flags — unknown flags are left for cobra to handle.
func scanGlobalFlags(args []string) (mosaicRoot, catalogFolder string, allowExternal bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mosaic-root" && i+1 < len(args):
			mosaicRoot = pathinput.Unquote(strings.TrimSpace(args[i+1]))
			i++
		case strings.HasPrefix(arg, "--mosaic-root="):
			mosaicRoot = pathinput.Unquote(strings.TrimSpace(strings.TrimPrefix(arg, "--mosaic-root=")))
		case arg == "--catalog-folder" && i+1 < len(args):
			catalogFolder = pathinput.Unquote(strings.TrimSpace(args[i+1]))
			i++
		case strings.HasPrefix(arg, "--catalog-folder="):
			catalogFolder = pathinput.Unquote(strings.TrimSpace(strings.TrimPrefix(arg, "--catalog-folder=")))
		case arg == "--allow-external":
			allowExternal = true
		}
	}
	return
}

// buildReloadFunc constructs the catalogue reload capability supplied to the TUI.
// When the user confirms a catalogue folder different from the one loaded at startup,
// the TUI calls the returned function to reload the catalogue from the new folder and
// rebuild the service around it. All other dependencies (registry, logger, config stores,
// manifest store, todo collector, interaction) are reused unchanged from baseDeps.
func buildReloadFunc(mosaicRoot string, baseDeps app.Deps) tui.CatalogReloadFunc {
	return func(catalogFolder string) (app.Service, error) {
		newCat, err := catalog.Load(mosaicRoot, catalogFolder)
		if err != nil {
			return nil, err
		}
		newDeps := baseDeps
		newDeps.Catalog = newCat
		return app.New(newDeps), nil
	}
}

// buildPreAnswers pre-scans args for --selections and, when present, parses the file
// into a PreAnswers value for the non-interactive Interaction. The scan follows the
// same --flag value / --flag=value form as scanGlobalFlags. When the flag is absent,
// or when the file cannot be read or parsed, an empty PreAnswers is returned so that
// cobra's own flag validation and exit-code logic remain the authoritative handler for
// bad --selections values.
func buildPreAnswers(args []string) cli.PreAnswers {
	var selectionsPath string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--selections" && i+1 < len(args):
			selectionsPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--selections="):
			selectionsPath = strings.TrimPrefix(arg, "--selections=")
		}
	}
	if selectionsPath == "" {
		return cli.PreAnswers{}
	}
	pa, err := cli.PreAnswersFromSelectionsFile(selectionsPath)
	if err != nil {
		// Return empty so cobra handles the error at flag-parse time with the correct exit code.
		return cli.PreAnswers{}
	}
	return pa
}
