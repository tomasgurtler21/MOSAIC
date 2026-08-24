---
run_id: "{run_id}"
created_by: "contracts-designer#6"
human_approved: true
---
# Contracts Design

## Verbose Flag Interface

- `cmd/widgets/main.go`: add `--verbose` / `-v` bool flag via pflag
- `internal/diagnostics`: `SetVerbose(bool)` controls stderr output
- `internal/diagnostics`: `Itemf(format, args...)` prints when verbose is on
