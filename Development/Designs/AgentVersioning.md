# Agent Versioning

> **Status:** Design Doc
> **Created:** 2026-02-04
> **Last Updated:** 2026-08-20
> **Supersedes:** firstGeneration `Development/Designs/AgentVersioning.md` (2026-04-23)

## Version Schema

`X.Y.Z` where:

| Component | Meaning | Example |
|-----------|---------|---------|
| **X** | Orchestration-breaking changes | Protocol changes, input/output artifact contract changes, anything affecting agent-orchestrator flow |
| **Y** | Behavioral changes | Instructions, process logic, anything affecting *what* the agent does or *how* |
| **Z** | Cosmetic changes | Typos, clarifications, rewording (no functional change) |

**When in doubt between Y and Z:** Bump Y. Better to over-version than miss behavioral drift.

---

## Version Fields

All MOSAIC version stamps carry the `mosaic_` prefix. MOSAIC does not define, inspect, or constrain any unprefixed frontmatter fields — those belong to the user and the harness. If a user wants their own `version` field, that is their business; MOSAIC will not collide with it.

Version stamps live in one of two places depending on what they track:

- **Frontmatter** — for structural/configuration concerns that have no corresponding content block in the agent body (template version, harness rules, tool mappings).
- **XML tag attributes** — for content blocks physically present in the agent body. The version lives on the tag of the content it versions.

### Generic Source Files

Generic agents in `Catalog/Subagents/` carry a single version:

```yaml
---
version: 1.2.0
---
```

`version` tracks the generic template's evolution. Every change to the template bumps it per the X.Y.Z schema above. On deploy, it is copied into the deployed file as `mosaic_version`.

### Deployed Agent Files

A deployed agent carries MOSAIC version stamps split across frontmatter and content region tags.

#### Frontmatter

```yaml
---
mosaic_version: 1.2.0
mosaic_harness_version: 3.0.0
mosaic_tool_mappings_version: abc123
mosaic_bundle_version: 1.0.0
description: Does things
model: claude-sonnet
---
```

| Field | What it tracks |
|-------|----------------|
| `mosaic_version` | Which generic template version this agent was built from. Copied verbatim from the catalog agent's `version` field at deploy time. |
| `mosaic_harness_version` | Which harness descriptor version was used to transform this agent. Tracks whether the harness transformation rules have changed since the last deploy. |
| `mosaic_tool_mappings_version` | Hash of the effective tool-destination mapping configuration. Detects stale tool mappings without re-diffing tool lists. |
| `mosaic_bundle_version` | Version of the deployed sections bundle (subagents only). The bundle's blocks (`AuthorityHierarchy`, `ClosingProcedure`, etc.) are one versioned unit (`Catalog/DeployedSections.md` `bundle_version`) — a single frontmatter stamp tracks whether they are current, rather than repeating a version on each block's tag. |

#### Content Region Tags

Version stamps for content that is physically injected into the agent body live as `version` attributes on the region's opening XML tag:

```xml
<CommunicationProtocol type="managed" version="1.6">
...protocol content...
</CommunicationProtocol>

<HarnessConstraints type="managed" version="1.1.0">
...platform injection content...
</HarnessConstraints>
```

| Region | What it tracks |
|--------|----------------|
| `<CommunicationProtocol>` | Protocol version. Special status: defines the subagent-orchestrator compatibility contract. |
| Harness injection regions | Platform injection content version. Read from the harness's `HarnessInjections.md` version. |
| Orchestrator injection regions | Orchestrator-specific injection content version (orchestrator only). Read from `HarnessInjectionsOrchestrator.md` version. |

### Where Versions Live — Design Rationale

The rule for placement: **if the version tracks a content block in the agent body, the version belongs on that block's tag. If it tracks something structural with no content block, it stays in frontmatter.**

| Version concern | Has content block? | Location |
|----------------|-------------------|----------|
| Generic template currency | No (it's the whole file) | Frontmatter (`mosaic_version`) |
| Harness transformation rules | No (structural) | Frontmatter (`mosaic_harness_version`) |
| Tool mapping config | No (structural) | Frontmatter (`mosaic_tool_mappings_version`) |
| Deployed sections bundle | Yes, but multiple blocks | Frontmatter (`mosaic_bundle_version`) — exception: one version for many blocks |
| Communication protocol | Yes | Tag attribute on `<CommunicationProtocol>` |
| Harness injections | Yes | Tag attribute on injection region |
| Orchestrator injections | Yes | Tag attribute on orchestrator injection region |

---

## Staleness Detection

The deployment tool uses MOSAIC stamps to detect when a deployed agent is outdated.

### Layer 1: Template Drift

```
If deployed.mosaic_version != Catalog.version → generic template changed since last deploy
```

| Mismatch | Action |
|----------|--------|
| **X differs** | Mandatory re-deploy (orchestration-breaking) |
| **Y differs** | Recommended re-deploy (behavioral drift) |
| **Z differs** | Optional (cosmetic only) |

### Layer 2: Injections Drift

```
If deployed injection region version != HarnessInjections.md version → platform injections changed
```

| Mismatch | Action |
|----------|--------|
| **X differs** | Mandatory re-deploy (constraint contract change) |
| **Y differs** | Recommended re-deploy (constraint content change) |
| **Z differs** | Optional (cosmetic only) |

### Layer 3: Harness Drift

```
If deployed.mosaic_harness_version != HarnessDescriptor.transform_version → harness rules changed
```

Harness drift means the transformation rules (frontmatter mapping, tool resolution, key ordering) have changed. The agent's body text may be identical, but the deployed frontmatter or structural wrapping is outdated.

### Layer 4: Other Stamps

Tool mappings, bundle, protocol, and orchestrator injections versions are each compared independently. Any mismatch triggers an update for that specific concern.

### Independence

All layers are independent. An agent can be current on one layer but outdated on another. The deployment tool reports each stale layer separately so the user understands *why* an update is needed.

### Example Scenario

```
Catalog agent-x:           version: 2.1.3
HarnessInjections.md:      version: 1.1.0
HarnessDescriptor:         transform_version: 3.0.0

Deployed agent-x:
  mosaic_version: 2.0.0                             (behind catalog 2.1.3)
  mosaic_harness_version: 3.0.0                     (current)
  <HarnessConstraints type="managed" version="1.0.0">  (behind injections 1.1.0)

Layer 1: X same, Y different (0→1) → behavioral drift in template
Layer 2: X same, Y different (0→1) → constraint content changed
Layer 3: current
Action: recommend re-deploy (layers 1 and 2 outdated)
```

---

## Current Implementation vs This Design

The current codebase (`agentfields` registry, `staleness.go`, `frontmatter.go`) has three divergences from this design:

### 1. `version` not renamed to `mosaic_version`

The current implementation carries the generic source `version` field through to deployed files unprefixed. It should be renamed to `mosaic_version` on deploy, parallel to how `id` → `mosaic_id` and `role` → `mosaic_role`.

**Migration path:** Add `mosaic_version` to the `agentfields` registry as a rename of the generic `version` field. The read path should accept both `version` and `mosaic_version` (preferring the prefixed form). On next deploy, the field is written as `mosaic_version`. Existing deployed files with unprefixed `version` are read correctly and migrated transparently on their next update.

### 2. `mosaic_transform_version` naming

The current field name `mosaic_transform_version` (legacy: `transform_version`) is confusing — it is too easily conflated with `version`. This design renames it to `mosaic_harness_version` (legacy: `harness_version`) to make the layer it tracks immediately obvious.

**Migration path:** Add `harness_version` / `mosaic_harness_version` as a new entry in the `agentfields` registry. The read path should accept `mosaic_transform_version`, `transform_version`, `mosaic_harness_version`, and `harness_version` (preferring the new prefixed form). On next deploy, the field is written under the new name. A legacy-to-current migration happens transparently.

### 3. Injection versions in frontmatter instead of on region tags

The current implementation writes `mosaic_injections_version` and `mosaic_orchestrator_injections_version` as frontmatter fields. This design moves them to `version` attributes on the corresponding injection region tags, consistent with how the protocol version already works.

**Migration path:** The staleness reader already parses region tag attributes for protocol and workflow versions. Extend that to read injection region version attributes. On next deploy, write the version on the region tag and stop writing the frontmatter field. The read path should check both locations during the transition period.

---

## Version Bump Rules — Quick Reference

### Generic Source Files (`Catalog/`)

| Change | Bump |
|--------|------|
| Protocol contract change | X |
| Artifact contract change (new/renamed input or output) | X |
| Process logic change (steps, conditions, instructions) | Y |
| Scope change (added/removed capabilities) | Y |
| Constraint change (new rule, relaxed rule) | Y |
| Typo fix, rewording, formatting | Z |

### Harness Descriptor `transform_version`

| Change | Bump |
|--------|------|
| Frontmatter key mapping change | Y |
| Tool resolution rule change | Y |
| Key ordering change | Z |
| New field added to descriptor | Y |
