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