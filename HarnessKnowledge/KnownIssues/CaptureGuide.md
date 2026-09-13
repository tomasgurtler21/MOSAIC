# Issue Capture Guide

Single reference for the structure, file formats, and conventions of the harness issue knowledge base. The **Harness Issue Hunter** agent (`Catalog/UtilityAgents/harness-issue-hunter.md`) is the primary consumer — it reads this guide for format specs and writes all files in this directory. Other agents and humans may also add or update entries when they discover harness behaviors that affect MOSAIC.

For the canonical harness list and folder naming convention, see `HarnessKnowledge/README.md`.

---

## What Gets Tracked

This knowledge base tracks **any harness behavior that creates operational risk for MOSAIC orchestration** — not just bugs. The source of the behavior (defect, design choice, architectural constraint, model quirk) doesn't determine whether it belongs here; what matters is whether it affects how MOSAIC runs.

Every entry carries a **Classification** that captures the nature of the behavior:

| Classification | Meaning | Fix expectation |
|---------------|---------|-----------------|
| **Bug** | Unintended defect — the harness team would consider this broken | May be fixed in a future release |
| **Limitation** | By-design behavior that constrains MOSAIC — maintainer confirmed intentional, or architectural constraint | No fix expected; MOSAIC must adapt permanently |
| **Quirk** | Behavioral surprise — model-level, undocumented, or inconsistent; not clearly a bug or a limitation | Unclear; may evolve with model or platform updates |

Classification affects expectations, not importance. A Limitation can be HIGH impact. A Bug can be LOW. The distinction tells MOSAIC whether to wait for a fix or build a permanent workaround.

---

## Don't Trust Duplicate/Closure Labels — Verify Yourself

GitHub's "possible duplicate" bot suggestions, a maintainer's "duplicate of #X" closure, and stale-bot auto-closures are **hints, not facts**. Repos with high issue volume (OpenCode is a clear example — hundreds of open issues at any time) lean hard on automation to keep the tracker manageable, and that automation over-clusters. Two issues can share symptoms (same error string, same crash pattern) while having genuinely different root causes, different reproduction conditions, or one containing evidence — a maintainer response, a working repro, a more severe variant — that the "canonical" issue lacks.

**Rule:** Whenever an issue is marked as a duplicate of, or the same root cause as, another issue you're tracking (or considering not tracking), open it and read it yourself — body and comments — before deciding. Ask:
- Does the reproduction actually match, or just the surface symptom?
- Does this issue contain a maintainer acknowledgment, reproduction, or severity detail the "original" doesn't have?
- Is the closure itself real (a fix landed, confirmed by the reporter) or just a stale-bot/needs-info auto-close with no actual resolution?

If it's genuinely the same behavior, don't create a second KB entry — cross-reference it in the existing entry's Notes (citing the additional issue number and what new evidence, if any, it adds). If it's actually distinct — different trigger, different scope, or a more severe variant (e.g., a "cosmetic mismatch" issue and a "someone confirmed an actual bypass" issue on a related topic are NOT the same finding) — give it its own entry even though a bot or maintainer called it a duplicate. When a batch of adjacent/duplicate-flagged issue numbers comes up during a scan (own findings or a prior pass's "suggested follow-up" list), treat verifying them as first-class investigation work, not a formality — this has repeatedly turned up wrongly-bucketed issues that were more severe than the entry they were filed against.

---

## Directory Structure

```
HarnessKnowledge/KnownIssues/
├── CaptureGuide.md              # This file — format specs (source of truth)
├── ClaudeCode/
│   ├── index.md
│   ├── active-issues.md
│   └── resolved-issues.md
├── GHCP-CLI/
│   ├── index.md
│   ├── active-issues.md
│   └── resolved-issues.md
├── OpenCode/
│   ├── index.md
│   ├── active-issues.md
│   └── resolved-issues.md
└── VsCodeGHCP/
    ├── index.md
    ├── active-issues.md
    └── resolved-issues.md
```

Each harness gets exactly **3 files**. No additional files, no per-issue files.

---

## Issue ID Convention

| Harness | Prefix | Example |
|---------|--------|---------|
| Claude Code | `CC-` | CC-001 |
| GitHub Copilot CLI | `GC-` | GC-001 |
| OpenCode | `OC-` | OC-001 |
| VS Code GHCP | `VC-` | VC-001 |

IDs are **permanent and sequential**. When an issue moves from active to resolved, it keeps its original ID. IDs are never reused.

---

## File Format: `index.md`

The quick-reference overview. One table of active issues, one table of resolved issues, and an optional thematic summary for harnesses with 10+ active issues.

```markdown
# {Harness Name} — Issue Index

> Last updated: {YYYY-MM-DD} (run {N})
> Latest platform version: {version} (released {YYYY-MM-DD})

## Active Issues ({count})

| ID | Title | Type | Confidence | Impact | Workaround | Reproduced | Response |
|----|-------|------|------------|--------|------------|------------|----------|
| XX-001 | Short title | Bug | Confirmed | HIGH | Yes | No | Unevaluated |

## Resolved / Retired Issues ({count})

> Resolved entries older than 3 months are automatically removed.

| ID | Title | Fixed In / Status | Resolution Date |
|----|-------|-------------------|-----------------|
| XX-003 | Short title | v1.2.3 | 2026-02-15 |

---

## Quick Summary

{Optional. For harnesses with 10+ active issues, group them thematically — e.g., "Subagent tool isolation failures", "Context compaction issues". 1-2 sentences per theme with issue ID references. This section helps consumers quickly understand the landscape without reading every entry. Rewrite from scratch on each run — this is a snapshot of the current landscape, not a changelog. Do not append per-run sections.}

## Recommended Mitigations

{Optional. Actionable bullet list of the most important cross-cutting workarounds, referencing specific issue IDs.}
```

**Run number:** Tracks how many scan/update passes the agent has completed. Increment on each run.

---

## File Format: `active-issues.md`

Header, then one entry per issue separated by `---`.

### Header

```markdown
# {Harness Name} — Active Issues

> Last updated: {YYYY-MM-DD} (run {N})
```

### Per-Issue Entry

```markdown
---

### {ID}: {Title}

| Field | Value |
|-------|-------|
| **Classification** | {Bug / Limitation / Quirk} |
| **Source** | {GitHub issue URL, or "Direct observation" for non-issue-tracker findings} |
| **Reported** | {YYYY-MM-DD} |
| **Last Activity** | {YYYY-MM-DD} |
| **Confidence** | {Confirmed / Likely / Unverified} |
| **Orchestration Impact** | {HIGH / MEDIUM / LOW} |
| **Reproduced at MOSAIC** | {Yes / No / Partial} |
| **MOSAIC Response** | {Mitigated / Accepted / Pending / Unevaluated} |
| **Version(s) Affected** | {version range or "unknown"} |
| **Latest Platform Version** | {current harness version at time of last update} |
| **Labels** | {relevant GitHub labels, comma-separated; "N/A" for non-issue-tracker sources} |

**Summary:**
{1-3 sentences. Must reflect the CURRENT understood state based on the full comment thread, not just the original report.}

**Impact on Orchestration:**
{How this issue affects agent workflows. Be specific — e.g., "Subagent responses may be truncated, causing the orchestrator to receive incomplete results."}

**Evidence:**
- {Bullet list. What makes this confirmed/likely/unverified — maintainer comments, reproduction reports, upvote count, related issues, direct testing.}

**Workaround(s):**
1. {Best workaround first. Note source and confirmation status. Mark best with star emoji.}
2. {Alternative workaround, if available.}

**Notes:**
{Additional context — version-specific behavior, pending fix PRs, related/duplicate issues, "needs re-verification" flags, cross-references to other issue IDs. For Limitations: note that this is by-design and what that means for MOSAIC's adaptation strategy. When MOSAIC Response is Mitigated or Accepted, describe what was done here (e.g., "Protocol v1.6 status code mapping", "HarnessInjections orchestrator constraint", "Agent template ClosingProcedure block").}
```

---

## File Format: `resolved-issues.md`

Issues that were previously tracked in `active-issues.md` and have since been resolved — fixed, removed, or determined to have no operational relevance.

### Header

```markdown
# {Harness Name} — Resolved Issues

> Last updated: {YYYY-MM-DD} (run {N})
```

### Per-Issue Entry

```markdown
---

### {ID}: {Title}

| Field | Value |
|-------|-------|
| **Source** | {GitHub issue URL} |
| **Fixed In / Status** | {harness version, PR link, `N/A (not relevant)`, or status like `Closed not_planned (reason)`} |
| **Resolution Date** | {YYYY-MM-DD} |
| **Original Orchestration Impact** | {HIGH / MEDIUM / LOW} |

**Summary:**
{Brief description of what the issue was.}

**Resolution:**
{How it was resolved — PR link if available, or brief description. For "not relevant" entries, explain why it was determined to have no operational impact on MOSAIC.}
```

### Rules

- Only move entries here from `active-issues.md` — never create entries directly in resolved.
- **Resolved means the behavior no longer exists or no longer matters.** A Bug that was fixed, a Limitation that the harness team removed, or an entry that turned out to have no operational relevance. A Limitation that is still present and still affects MOSAIC stays in `active-issues.md` — even if MOSAIC has adapted to it (that's what MOSAIC Response: Mitigated is for).
- **Placeholder lines:** When an entry is moved to resolved, remove it from `active-issues.md` entirely. Do not leave placeholder lines — the gap in ID numbers is self-explanatory, and the resolved file is the authoritative record.
- **Retention:** Remove entries older than 3 months past their Resolution Date. The resolution is well-established; the entry has served its purpose.
- Issue IDs are permanent — the same ID in resolved refers to the same issue it did in active.

---

## Classification

| Classification | Meaning | How identified |
|---------------|---------|----------------|
| **Bug** | Unintended defect — the harness team would consider this broken | Maintainer acknowledged as defect, or behavior clearly contradicts docs/expected behavior |
| **Limitation** | By-design behavior that constrains MOSAIC — maintainer confirmed intentional, or architectural constraint | Maintainer said "by design" / "won't fix", or the behavior is a documented architectural property of the harness |
| **Quirk** | Behavioral surprise — model-level, undocumented, or inconsistent; not clearly a bug or a limitation | Behavior doesn't fit neatly into Bug or Limitation; may be model-dependent, intermittent, or undocumented |

When unsure, default to **Bug** — it's the safest assumption. Reclassify to Limitation when a maintainer confirms the behavior is intentional, or to Quirk when investigation reveals it's model-level or inconsistent.

---

## Confidence Levels

Confidence measures how certain we are that the behavior exists and is correctly understood — not whether it's a "real bug."

| Level | Criteria |
|-------|----------|
| **Confirmed** | Maintainer explicitly acknowledged the behavior, OR linked to merged/open PR, OR multiple independent reproductions with evidence, OR directly observed and reproduced at MOSAIC |
| **Likely** | Multiple upvotes (5+) AND repro steps provided, OR several independent reports of same symptoms, but no maintainer confirmation |
| **Unverified** | Single reporter, no confirmations. Add only if highly relevant to orchestration; flag why it was included |

For **Limitations**: Confirmed means a maintainer explicitly stated the behavior is intentional, or the behavior is a documented architectural property. Likely means strong circumstantial evidence of by-design intent. Unverified means we suspect it's by-design but lack confirmation.

---

## Orchestration Impact Levels

| Impact | Criteria |
|--------|----------|
| **HIGH** | Directly affects subagent invocation, agent delegation, primary-subagent communication, context compaction, tool execution failures, or causes data loss. Can break workflows entirely. |
| **MEDIUM** | Affects tool behavior in ways that degrade output quality or reliability but don't break the workflow. Permission quirks, inconsistent behavior, output truncation. |
| **LOW** | Edge cases that rarely trigger during orchestration. Worth tracking for escalation potential. |

---

## Workaround Available

Summary for the index table — derived from the Workaround(s) section in the detailed entry.

| Value | Meaning |
|-------|---------|
| **Yes** | At least one confirmed or community-validated workaround exists |
| **Partial** | Workarounds exist but are incomplete, version-specific, or have significant trade-offs |
| **No** | No known workaround |

---

## Reproduced at MOSAIC

Whether this issue has been personally hit or reproduced in our environment — not whether the GitHub community has confirmed it.

| Value | Meaning |
|-------|---------|
| **Yes** | A MOSAIC user has reproduced this or confirmed they've hit it |
| **No** | Not yet tested by us |
| **Partial** | We've seen symptoms consistent with it but haven't isolated it |

**Ownership:** This field is **user-maintained only**. The Issue Hunter agent always sets this to `No` when adding new entries. Only a human updates it to `Yes` or `Partial` after hands-on testing.

---

## MOSAIC Response

Whether MOSAIC has adapted to this issue — through any means (harness injections, protocol changes, agent template design, deployed sections, workflow design, or a conscious decision that no action is needed).

| Status | Meaning |
|--------|---------|
| **Mitigated** | MOSAIC has adapted — describe how in the Notes section |
| **Accepted** | Evaluated, no action needed (doesn't affect us, risk is tolerable, or N/A for our setup) |
| **Pending** | Needs action but nothing done yet |
| **Unevaluated** | Not yet assessed by the team |

**Ownership:** This field is **user-maintained only**. The Issue Hunter agent always sets this to `Unevaluated` when adding new entries. Only a human updates it after evaluation.

When the status is **Mitigated** or **Accepted**, the Notes section of the issue entry must describe what was done (e.g., "Protocol v1.6 status code mapping", "DeployedSections ClosingProcedure block", "HarnessInjections orchestrator constraint added in v1.2.0").
