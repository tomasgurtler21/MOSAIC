# 0.5.0 (2026-09-23)

## Tools

### Runner v1.1.0
#### Changes
- Automated test framework (dev mode): `test` subcommand and TUI test flow for selecting and running orchestration test suites, with test catalog discovery, fixture management, expected-result validation, and multi-mode support (auto, auto-review, orchestrated)
- Reworked agent snapshot mechanism for OpenCode and GHCP CLI: agents are now transformed in-place with automatic backup and crash recovery, instead of requiring a separate runner directory
- Less rigid response parsing from LLMs with automatic retry on non-compliant subagent responses (no user consultation needed)

#### Bugfixes
- Fix delayed infrastructure triggers

### Deploy v1.0.2
#### Changes
- New `--infrastructure` flag on `deploy` subcommand for pre-answering infrastructure agent selection in CI/scripted deployments

## Catalog
- **Planner** (v7.3.0): Reworked stage definition and sizing — stages are now the unit the orchestrator dispatches, with explicit guidance on bounded context, uniform approach, internal coherence, and clean checkpoints. Task sizing reframed as progress checkpoints within execution groups, not session boundaries.
- **Plan Review** (v5.3.0): New stage sizing assessment section validates that execution groups fit in a single agent session. PARTIALLY_DONE guidance improved for large inputs.
- **Contracts Designer** (v4.3.0): Now explicitly scoped to comply with the plan rather than silently overriding plan decisions. Tighter boundary between contract specification and implementation detail — private methods, algorithms, and pseudocode excluded to prevent review spirals.
- **Contracts Review** (v4.4.0): Flags overspecification (implementation detail in contracts) for removal. Aggregates all findings before returning to reduce review-creator round-trips.

## Documentation
- GettingStarted guide: clarified how to add workflows to an existing deployment (Update workflows mode)
- Deployment Guide: documented Update workflows, Deploy agents, and Deploy hooks modes; removed stale Utility-infra section
- Runner Guide: updated snapshot strategy and crash recovery documentation

# 0.4.0 (2026-09-13)

All tools now carry their own version number, displayed in TUI headers and embedded in logs/reports.

## Tools

### Deploy v1.0.1
#### Changes
- Unused tool mappings are now removed from agents frontmatter on deploy
- Harness constraints injection version is now added even when empty for a given harness
#### Bugfixes
- Fix MCP server deployment for Claude Code harness
- Fix `orchestrator-scripted` not being updated properly during deploy
- Fix `s`/`S` key collision on TUI screen
- Fix some incorrect TODOs at deployment

### Runner v1.0.0
#### Changes
- Improved logging
#### Bugfixes
- Fix graceful stop not working

### AgentTest v1.0.0
#### Changes
- Summary split into user-facing and internal reports
- Scheduling adjusted to leverage Anthropic cross-process prompt caching (cost reduction)
- Improved cost accuracy, infra errors excluded from summary problem areas and cost calculations

### LogAnalyzer v1.0.0
No changes.

## Orchestration System
- Communication protocol updated to prevent routing around HITL
- Orchestrator instructions updated to prevent routing around HITL

## Catalog
- New standalone agents: `spearhead` (exploration), `presentation-creator`
- `CatalogFilesFormat.md` renamed and updated

## Orchestration Test Results
- Completed remaining test suites for Claude Code with Opus 5
- Added results for Sonnet 5, Haiku 4.5, Fable 5

## Harness Knowledge
- First harness issues capture and evaluation completed for all supported harnesses
- System prompt captures created for each harness

## Documentation
- `CONTRIBUTING.md`, `SECURITY.md` created, README updated with Getting Started section

# 0.3.0 (2026-09-01)
## Tools
### Changes
#### Deploy
- No change to tools field in agents on Update anymore, to preserve user customization of them
- `user-invocable` field added to VS code agents deploy
- Multi-level multi-selection screens now show the active selection.

#### Runner
- At start of new run, tool creates own snapshots directory - copy of agents - enabling it to modify agents as needed for execution via CLI without touching user agents.

### Bugfixes
#### Deploy
- Stop detecting changes in user managed instruction regions as local changes, triggering overwrite dialogs. Tool should now show true changes on update.
- Dialogs for TIER model are now triggered only for TIERs used by deployed agents. 

#### Runner
- Engine will not enter loop anymore at dispatches with HITL true and output artifacts with wildcard. Added general anti-loop mechanism as well.
- At create-review pairs, on route back from review to create, add its original output artifacts to its input, to prevent agent overwriting its previous work without realizing.
- Fix hard crash in some scenarios.
- Give agents correct safe tool permissions matching agents tools frontmatter. For GHCP, it is still possible to switch to yolo mode in TUI dialog.

## Catalog
- Removed all Unicode characters from Catalog, all should be ASCII only now.
- Updated Deployed sections bundle version (subagents HITL execution improved, ASCII only constraints to artifacts)

## Notes
- MOSAIC root contains mosaic-helper agent deployed for all harnesses. Agent aims to provide user help with any questions regarding MOSAIC.
- Finished test tool to check orchestrator behavior in various workflow scenarios. Incomplete results in OrchestrationTestResults, will be completed asap.
- Roadmap created in MOSAIC root.

# 0.2.0 (2026-08-21)
## Tools
- Critical: Orchestrator instructions mess fixed
- Many fixes in deploy tool, higlights:
1. Deploy all skill files
2. Fix OpenCode and GHCP CLI value of mode/user-invocable field
3. `Update workspace` does not create orchestrator anymore
4. `version` field changed to `mosaic_version`, harness injection versioning moved from frontmatter to XML tag
5. Preserve user custom tools and infrastructure agents in orchestrator on agents update

## Catalog
- Two new workflows: `Brownfield PR Fix Workflow` and `Product Comparison Workflow`