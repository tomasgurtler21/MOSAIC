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