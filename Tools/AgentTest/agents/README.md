# Test-Only Agent Definitions

This directory holds generic-form MOSAIC agent definitions for stub collaborators used by
AgentTest test suites. These definitions exist so that a harness dispatch to the collaborator
is a legal dispatch — they do not specify what the collaborator replies with.

## What belongs here

A file belongs here when:

- It is a stub collaborator that test suites dispatch to
- It has no place in the product's `Catalog/` catalogue (it exists only in tests)
- It is sourced by a `stub_agents` entry in a test definition file

What a stub *replies* is declared separately, in the `*.stubs.json` registry alongside each
test definition. These two things serve different purposes and must not be conflated.

## What does not belong here

- No file from this directory is ever added under `Catalog/`
- No file from this directory is registered in the product catalogue
- No reply content belongs here — reply content goes in the stub registry

## File naming convention

Each file is named `<agent-key>.md`, where `<agent-key>` is the `agent` value of the
`CollaboratorIdentity` it stands in for. A test definition refers to it by this path,
relative to the test definition file:

```yaml
stub_agents:
  - identity:
      tool: dispatch
      agent: researcher
    source: ../agents/researcher.md
```

## Required frontmatter fields

Every definition in this directory must carry the following frontmatter fields. A definition
missing any of these will not render correctly through the deployment tool.

| Field | Notes |
|-------|-------|
| `id` | Numeric identifier, unique within this directory. These ids live in a separate namespace from the product catalogue and never collide with it — files here are never scanned by the catalogue. |
| `version` | Any version scalar. Unlike the product catalogue (where an absent `version` is legal), a stub with no version is indistinguishable from a broken one, so it is required here. |
| `name` | Must match the file's base name without the `.md` extension. |
| `description` | One line describing the collaborator's role in tests. |
| `role` | Always `subagent` for every stub. |
| `model` | Use the `{model-identifier}` placeholder, as product agents do. |
| `recommended_tier` | Always `TEST-STUB` for every stub in this directory. **Required, not optional:** this is the sole mechanism by which stubs receive the cheap model at deploy time. The deployment tool derives its tier list purely from `recommended_tier` frontmatter, and a stub that omits or deviates from `TEST-STUB` falls out of the shared cheap-model tier mapping, deploying with an unresolved model placeholder on a run that still reports success — a silent failure with a real cost. |
| `tier_rationale` | One line, per catalogue convention. The deployment tool surfaces it when asking about a tier. |
| `tools` | May be an empty list. |

## Required body structure

At minimum, every definition must contain:

- An `<Identity type="core">` block describing the agent's purpose
- A `<CommunicationProtocol type="managed">` region, so the rendered stub receives the protocol
  the interception layer expects it to speak

## ID assignment

Assign the next available integer starting from 1. These IDs are local to this directory.
Current assignment:

| ID | File |
|----|------|
| 1  | requirements-refinement.md |
| 2  | researcher.md |
| 3  | library-researcher.md |
| 4  | planner.md |
| 5  | codebase-research.md |
| 6  | requirements-review.md |
| 7  | planner-tdd-soft.md |
| 8  | plan-review.md |
| 9  | contracts-designer.md |
| 10 | contracts-review.md |
| 11 | test-writer-tdd.md |
| 12 | tests-review-tdd.md |
| 13 | implementation-tdd.md |
| 14 | implementation-review.md |
| 15 | test-runner.md |
