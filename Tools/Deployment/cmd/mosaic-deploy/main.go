// Command mosaic-deploy is the entry point for the MOSAIC deployment tool.
//
// When a subcommand is present in the arguments (deploy or update), the CLI adapter handles
// the invocation non-interactively. When no subcommand is present and a terminal is attached,
// the TUI frontend would be invoked (TUI wiring is a later stage).
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

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"

	// Import built-in harnesses so their init() functions run and register
	// themselves with the package-level registry before Discover is called.
	_ "mosaic-deploy/internal/harness/builtin/claudecode"
	_ "mosaic-deploy/internal/harness/builtin/ghcpcli"
	_ "mosaic-deploy/internal/harness/builtin/opencode"
	_ "mosaic-deploy/internal/harness/builtin/vscodeghcp"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mosaic-deploy <deploy|update> [flags]")
		os.Exit(cli.ExitUsage)
	}

	// Step 1: Pre-scan global flags needed for dependency wiring. cobra will
	// re-parse these when cli.Run calls root.Execute; the two parses agree.
	mosaicRoot, allowExternal := scanGlobalFlags(args)

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
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(cli.ExitFailure)
		}
		mosaicRoot = resolved
	}

	// Step 3: Load project-level tool configuration. Absent file yields documented
	// defaults (no error); other I/O errors are fatal.
	toolConfigStore := config.NewToolConfigStore(mosaicRoot)
	toolCfg, err := toolConfigStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load tool config: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 4: Construct the logger before catalog/registry (StartRun is called
	// inside the app layer per run, not here).
	logger := logging.New(mosaicRoot, toolCfg)

	// Step 5: Load the MOSAIC source catalog.
	cat, err := catalog.Load(mosaicRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load catalog: %v\n", err)
		os.Exit(cli.ExitFailure)
	}

	// Step 6: Discover available harnesses. External modules are allowed when
	// either the command-line flag or the tool config enables them.
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:    mosaicRoot,
		AllowExternal: allowExternal || toolCfg.AllowExternalModules,
		GOOS:          runtime.GOOS,
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
	interaction := cli.NewInteraction(cli.PreAnswers{}, todoCollector, os.Stdout)

	deps := app.Deps{
		Catalog:     cat,
		Registry:    reg,
		Planner:     plan.New(),
		Executor:    deploy.NewExecutor(manifestStore, logger, todoCollector),
		Manifest:    manifestStore,
		ToolConfig:  toolConfigStore,
		UserConfig:  config.NewUserConfigStore(mosaicRoot),
		Logger:      logger,
		Todo:        todoCollector,
		Interaction: interaction,
		MosaicRoot:  mosaicRoot,
		GOOS:        runtime.GOOS,
	}

	// Step 8: Dispatch to the CLI adapter with a fully-wired service. cli.Run
	// handles all subcommand routing and output formatting; main.go is purely
	// responsible for dependency construction and process exit.
	code := cli.Run(context.Background(), args, app.New(deps), os.Stdout, os.Stderr)
	os.Exit(code)
}

// scanGlobalFlags does a minimal pre-scan of args for the two global flags that
// influence dependency wiring: --mosaic-root and --allow-external. cobra will
// re-parse all flags when cli.Run executes; this pre-scan exists only so that
// main.go can construct app.New(deps) before handing control to cli.Run.
//
// The scan understands both --flag value and --flag=value forms. It does not
// validate other flags — unknown flags are left for cobra to handle.
func scanGlobalFlags(args []string) (mosaicRoot string, allowExternal bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mosaic-root" && i+1 < len(args):
			mosaicRoot = args[i+1]
			i++
		case strings.HasPrefix(arg, "--mosaic-root="):
			mosaicRoot = strings.TrimPrefix(arg, "--mosaic-root=")
		case arg == "--allow-external":
			allowExternal = true
		}
	}
	return
}
