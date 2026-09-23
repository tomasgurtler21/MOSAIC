# Roadmap

_Last updated: 2026-09-23_

Informal, hand-maintained list of what's next for this project. Order within each section does not indicate priority. Items can be moved between sections at any time.

## Now (ASAP)

- Update Runner tool to produce proper MOSAIC logs.
- Update Runner to use workflows On Finding column so it can run more complex workflows without orchestrator help in Auto mode
- Update all docs, finalize its organization.

## Next (Near Future)

- Improve orchestrator according to finished orchestration tests.
- Add Codex harness.
- Review all harnesses MOSAIC logs for precision, focused on cost (especially Claude Code, seems to be cca 40% off).
- Attempt to filter out potential secrets from MOSAIC logs.
- Improve LogAnalyzer display once logs are more reliable.
- Update External modules at Deploy tool, its functionality is very likely lagging behind current Deploy tool capabilities.

## Later (Distant Future)

- Additional basic test set to check subagent basic compliance with `managed` instructon sections (like BLOCKED status on missing artifacts, skills, response format, etc).
- Add more harnesses. (Cursor, AWS Kiro, M365, etc)
- Look into orchestrator/subagents instructions, try to reduce number of tokens somehow.
- Move System layer (Communication protocol, deployed sections) to Catalog, enabling people to overwrite it. To be considered further, not sure yet.

## Someday (Dreams)


- Extra orchestrations test coverage for orchestrator-script (Runner tool helper). Unlikely to ever have budget for that, but at least one baseline comparison to regular orchestrator would be nice.
- MCP tool for cross-harnesses orchestration.


