package main

import (
	"strings"

	"mosaic-run/internal/cli"
)

// stripBoolFlag returns a copy of args with all occurrences of the named
// boolean flag removed. It handles the bare "--flag" form and the
// "--flag=true"/"--flag=false" forms. This is used to remove entry-point-only
// flags (like --dev) before passing args to a cobra command that does not
// register them.
func stripBoolFlag(args []string, flag string) []string {
	prefix := flag + "="
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, prefix) {
			continue // drop this token
		}
		result = append(result, arg)
	}
	return result
}

// firstPositionalArg returns the first genuine positional argument in args —
// a token that is neither a flag nor the value of a preceding value-bearing
// flag — or "" when no positional argument is found. Uses the combined
// value-bearing flag set (run and test subcommands) via cli.AllValueBearingFlagNames()
// so that test subcommand values like "--catalog /some/path" are not
// misidentified as positional arguments.
func firstPositionalArg(args []string) string {
	valueBearing := make(map[string]bool)
	for _, name := range cli.AllValueBearingFlagNames() {
		valueBearing[name] = true
	}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		if !strings.Contains(arg, "=") && valueBearing[arg] {
			skipNext = true
		}
	}
	return ""
}

// scanBoolFlag reports whether a boolean flag (e.g. "--tui") appears anywhere in args.
func scanBoolFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// boolFlagState is the tri-state outcome of pre-scanning a boolean flag, needed
// because a flag whose cobra default is true cannot be pre-scanned with a
// two-state helper: "absent" and "explicitly false" must be distinguishable.
type boolFlagState int

const (
	boolFlagAbsent boolFlagState = iota // flag not present in args
	boolFlagTrue                        // "--flag" or "--flag=true"
	boolFlagFalse                       // "--flag=false"
)

// scanBoolFlagState classifies a boolean flag's presence in args, understanding
// the bare "--flag" form and the "--flag=true"/"--flag=false" forms. It mirrors
// cobra's boolean flag parsing for pre-scan purposes only.
func scanBoolFlagState(args []string, flag string) boolFlagState {
	prefix := flag + "="
	for _, arg := range args {
		if arg == flag {
			return boolFlagTrue
		}
		if strings.HasPrefix(arg, prefix) {
			val := strings.ToLower(strings.TrimPrefix(arg, prefix))
			if val == "false" {
				return boolFlagFalse
			}
			return boolFlagTrue
		}
	}
	return boolFlagAbsent
}

// scanBoolFlagDefault is the default-aware version of scanBoolFlag. It returns
// def when the flag is absent, true for the bare flag form, and honours
// --flag=true / --flag=false forms explicitly.
//
// Every boolean pre-scan in this file must pass the same def as the cobra flag
// declaration's default, so the pre-scan and the parsed flag cannot disagree.
func scanBoolFlagDefault(args []string, flag string, def bool) bool {
	switch scanBoolFlagState(args, flag) {
	case boolFlagTrue:
		return true
	case boolFlagFalse:
		return false
	default: // boolFlagAbsent
		return def
	}
}

// preConsultFromArgs resolves the effective pre-consultation setting from args,
// applying the same default (true) that the --pre-consult cobra flag declaration
// uses. This is the call site whose default literal pins agreement: any change
// to the cobra default that does not also update this call is caught by the
// TestPreConsultFromArgs_* tests.
func preConsultFromArgs(args []string) bool {
	return scanBoolFlagDefault(args, "--pre-consult", true)
}

// hasFlag reports whether args contains the named flag in either the
// "--flag value" or "--flag=value" form. Unlike scanFlag it returns no value,
// and unlike scanBoolFlag it matches the "--flag=value" form; it exists for
// flags whose mere presence is decisive during the pre-scan.
func hasFlag(args []string, flag string) bool {
	prefix := flag + "="
	for _, arg := range args {
		if arg == flag {
			return true
		}
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

// hasPositionalArg reports whether args contains at least one genuine positional
// argument — a token that is neither a flag nor the value of a preceding
// value-bearing flag. The value-bearing set comes from cli.AllValueBearingFlagNames(),
// which covers both the run and test subcommands' flags so that neither
// subcommand's value arguments are misidentified as positional arguments.
//
// Scan rules (left to right):
//   - A token that starts with "-" and is in the value-bearing set consumes the
//     following token as its value; that value is not positional.
//   - A token that starts with "-" and contains "=" has its value embedded; no
//     following token is consumed.
//   - A token that starts with "-" otherwise (boolean flag or unknown flag)
//     consumes nothing.
//   - A token that does not start with "-" and was not consumed as a value is a
//     genuine positional argument.
func hasPositionalArg(args []string) bool {
	valueBearing := make(map[string]bool)
	for _, name := range cli.AllValueBearingFlagNames() {
		valueBearing[name] = true
	}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue // consumed as the value of the preceding value-bearing flag
		}
		if !strings.HasPrefix(arg, "-") {
			return true // not a flag and not consumed as a value: genuine positional
		}
		// It is a flag token. If it contains "=" the value is embedded; if not
		// and it names a value-bearing flag, the next token is its value.
		if !strings.Contains(arg, "=") && valueBearing[arg] {
			skipNext = true
		}
	}
	return false
}

// scanFlag does a minimal pre-scan of args for a named flag. It understands both
// "--flag value" and "--flag=value" forms, consistent with cobra's flag parsing.
func scanFlag(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
		prefix := flag + "="
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}
