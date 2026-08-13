# Interface

Agents that bridge the multi-agent orchestration system with external systems and human communication channels.

## Purpose

Interface agents handle **bidirectional data exchange** between:
- The orchestration system (structured, machine-readable formats)
- External systems (PR comments, issue trackers, chat platforms, etc.)

They are **translators and connectors**, not decision-makers or actors.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 19 | [audit-to-pull-request](./audit-to-pull-request.md) | 3.0.0 | Transforms a single audit artifact into condensed PR-ready comments — filters to PR scope with context zone intelligence, deduplicates against existing PR comments, writes to partial response queue |
| 34 | [audit-response-merger](./audit-response-merger.md) | 1.0.0 | Merges partial PR response queues and transform reports from parallel audit-to-pull-request instances into consolidated artifacts — cross-audit deduplication, source attribution, merge summary |
| 18 | [pull-request-comment-interface](./pull-request-comment-interface.md) | 1.1.2 | Bridges PR comments with orchestration - retrieves human comments for subagents, posts subagent output as PR comments |

## Characteristics

All Interface agents share these traits:
- **Bidirectional:** Can both retrieve (external → system) and post (system → external)
- **Context-preserving:** Maintain links to source locations (file, line, thread)
- **Format-translating:** Convert between human-readable and machine-readable formats
- **Non-interpretive:** Pass data faithfully, don't make decisions about meaning

## Typical Workflow Integration

Interface agents typically run:
- **At workflow start:** Retrieve external input (e.g., PR comments as task requirements)
- **At workflow end:** Post results back to external system
- **Mid-workflow:** When human input is needed or intermediate results should be shared

## Future Agents

Potential additions to this function:
- IssueInterface - Bridge with issue tracking systems (GitHub Issues, Jira, etc.)
- SlackInterface - Bridge with Slack/Teams for notifications and input
- EmailInterface - Bridge with email for async communication
