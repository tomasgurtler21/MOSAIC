# Workflow Index

Fixture for index/disk reconciliation testing.
Two mismatches are intentionally present:
  1. "missing-workflow" appears in the index but has no corresponding file on disk (index-orphan).
  2. "Test/orphan-on-disk.md" exists on disk but is not listed in the index (file-orphan).

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| present-workflow | Test | 1.0 | Present Workflow | A workflow that exists both in the index and on disk. | present | Fixture | `Test/present-workflow.md` |
| missing-workflow | Test | 1.0 | Missing Workflow | Listed in the index but has no corresponding file on disk. | missing | Fixture | `Test/missing-workflow.md` |
