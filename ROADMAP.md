# Roadmap

_Last updated: 2026-08-31_

Informal, hand-maintained list of what's next for this project. Order within each section does not indicate priority. Items can be moved between sections at any time.

## Now (ASAP)

- Test and fix Runner tool for all harnesses, most likely expanding Runner tests in the process.
- Update Runner tool to produce proper MOSAIC logs.
- Add capture of harness system prompt, list of known bugs in harnesses, recommended workarounds.
- Double check all orchestration test suites execution, some seems to be suspiciously expensive in comparison to others.
- Update all docs, finalize its organization.

## Next (Near Future)

- Finish baseline orchestrations tests of latest version of orchestrator. After that, start improving orchestrator according to test revelations.
- Add Codex harness.
- Review all harnesses MOSAIC logs for precision, focused on cost (especially Claude Code, seems to be cca 40% off).
- Improve LogAnalyzer display once logs are more reliable.
- Update External modules at Deploy tool, its functionality is very likely lagging behind current Deploy tool capabilities.

## Later (Distant Future)

- Improvement of workflows, subagents, possibly new ones.
- Additional basic test set to check subagent basic compliance with `managed` instructon sections (like BLOCKED status on missing artifacts, skills, response format, etc).
- Add more harnesses.
- Look into orchestrator/subagents instructions, try to reduce number of tokens somehow.

## Someday (Dreams)


- Extra orchestrations test coverage for orchestrator-script (Runner tool helper). Unlikely to ever have budget for that, but at least one baseline comparison to regular orchestrator would be nice.
- MCP tool for cross-harnesses orchestration.


