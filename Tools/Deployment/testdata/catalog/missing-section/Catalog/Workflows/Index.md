# Workflow Index

Fixture for WorkflowSection missing-block testing.
The "no-section-workflow" entry has a corresponding file on disk, but that file does not
contain a [[SECTION:Workflow:no-section-workflow]] block. WorkflowSection must return an
error for this case.

| ID | Category | Version | Name | Description | Hint | Author | File |
|----|----------|---------|------|-------------|------|--------|------|
| no-section-workflow | Test | 1.0 | No Section Workflow | A workflow file that exists on disk but contains no section block. | missing block | Fixture | `Test/no-section-workflow.md` |
