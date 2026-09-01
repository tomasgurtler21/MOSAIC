---
run_id: "{run_id}"
created_by: "requirements-refinement#2"
human_approved: false
---
# Requirements

## Verbose Flag

- Add --verbose / -v flag to widgets CLI
- When enabled, print per-item diagnostics to stderr
- Use existing diagnostics.Fprintf pattern
- Default: off (no diagnostics output)
- Acceptance: running `widgets --verbose list` prints per-item lines to stderr
