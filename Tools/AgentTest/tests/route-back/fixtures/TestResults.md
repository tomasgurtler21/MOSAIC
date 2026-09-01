---
run_id: "{run_id}"
created_by: "test-runner#17"
human_approved: false
---
# Test Results

## Summary

3 of 5 tests passing. 2 failures in stage 2 diagnostics output formatting.

## Failures

1. test_itemf_format_string: Expected stderr line format "DIAG: item_name" but got "item_name"
2. test_itemf_no_output_when_quiet: Diagnostics output present even without --verbose flag
