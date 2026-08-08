// Package runner violates the use-case rule by importing a frontend (cli).
package runner

import "mosaic-agent-test/internal/cli"

var _ = cli.Runner{}
