---
id: agent-template-architecture
type: specification
version: "2.0"
name: "Agent Template Architecture"
description: "The structure of a MOSAIC agent file: frontmatter schema, the four region kinds and their ownership, canonical document order, the per-role region matrix, and what each section must contain."
author: MOSAIC
status: Draft
---

## 1. Overview

### 1.1 Purpose

An agent file is a system prompt with a schema. This document is that schema: what an agent file contains, who owns each part of it, and in what order the parts appear. It is the document to read when fixing an agent, and the document to follow when writing a new one.

**Covered here:**

- The four region kinds and what each one's tag promises (§2.1)
- Canonical document order and the per-role region matrix (§2.3, §2.4)
- Every tool-managed region name and where its content comes from (§2.5)
- Frontmatter fields, their meaning, and their bump rules (§3)
- What each section must contain, section by section (§4)
- How the orchestrator differs, and why (§5, §8)
- The injection catalogue and the rules governing it (§6)
- The conformance rules, what each one's severity is, and who enforces it (§9)

**Specified elsewhere, deliberately:**

| Concern | Owner |
|---|---|
| The orchestration contract's content — message shape, status and error vocabularies, the HITL gate, the artifact provenance stamp | `CommunicationProtocol.md`. This document places the region; that one decides what goes in it. |
| Bundle membership, versioning, staleness, the deploy algorithm | `DeployedSectionsBundle.md` |
| What any canonical block says and why | The five documents under `DeploymentBlocks/` |
| All canonical text itself | `Catalog/DeployedSections.md`, and nowhere else (§7) |
| The measurement and decision that produced the blocks | `Development/Analysis/AgentBodyDrift.md` |
| Infrastructure agent declaration, trigger vocabulary, `Class` | `InfrastructureAgentConcept.md`. An infrastructure agent is an ordinary subagent by this schema; what makes it infrastructure is how the orchestrator reaches it, not how its file is shaped. |
| Utility and standalone agents | §5A. Two roles that live in the catalog and receive harness transformation but are not governed by this schema's structure rules or deployment bundle. |
| Workflow definitions and the orchestration artifact's schema | Their own documents. Both are consumed by an agent this document shapes; neither is shaped by it. |
| Skill and hook-bundle file formats | `SourceFilesFormat.md`. Separate artefact kinds with their own conventions. |

### 1.2 Why the Format Looks Like This

Agent files were originally written by copying a complete template and filling in the blanks, with marked injection points for the parts a project would customise. Everything else was prose an author pasted and then maintained by hand.

That arrangement produced exactly the outcome it was always going to. Forty-two subagent files each held their own copy of the shared body text, four of those fragments had measurably diverged, and three defects rode along with the divergence. None of it was noticed until someone counted. The measurement is `Development/Analysis/AgentBodyDrift.md`.

Three things are different in the format specified here, and together they are why this document specifies a schema rather than supplying a template.

**Ownership is declared in the file.** A region's opening tag says who writes it: `type="core"` is MOSAIC-authored and carried verbatim, `type="managed"` is written by the tool and regenerated every deploy, `type="project"` is declared by MOSAIC and filled by the project, `type="custom"` is invented by the project entirely. A reader knows which regions are theirs without consulting anything.

**Content has moved out of the template into contracts.** The orchestration protocol left in v1.9, and the provenance stamp folded into it. It is now one file deploying into one named region. The template's job is no longer to hold that text but to place the slot it goes into.

**Shared body text follows it.** The same treatment extends to the five fragments that remained hand-copied. After the migration in §10, the only prose in a subagent file that is not single-sourced is prose about *that agent*.

### 1.3 Design Principles

| # | Principle | What it buys |
|---|---|---|
| 1 | **Text repeated across agents is deployed, never copied** | Forty-two copies of a sentence drift; one source cannot. Every fragment identical across agents is either single-sourced or deleted. |
| 2 | **The tag states the owner** | `type="core"`, `type="managed"`, `type="project"`, `type="custom"` answer "may I edit this?" from the file itself. No lookup into code or docs. |
| 3 | **A source file is a valid document on its own** | Empty managed regions and empty project slots are the normal state of a source file, not a degraded one. Nothing is required to be filled for the file to be well-formed. |
| 4 | **One fact, one authority** | The contract states message shape and the stamp; this document states structure. Where they touch, they reference rather than restate — a second copy is always the one that goes stale. |
| 5 | **Structure is mechanically checkable** | Every rule here that can be a validator rule is written to be one (§9). A convention no tool can check is a convention that erodes. |
| 6 | **Agent-specific text earns its place by being agent-specific** | A section survives in an agent file because its content differs per agent. Content that does not differ belongs in the bundle; content already stated by an adjacent managed region belongs nowhere. |
| 7 | **The nearest instruction wins in practice** | A model follows the specific, role-adjacent instruction over the general one three sections up. So the nearby instruction must be correct, not merely present — this is why the HITL defect mattered far more than its count suggested. |
| 8 | **A check that flags legitimate work gets switched off** | Principle 5's counterweight. A validator that errors on an unusual but working agent teaches its user to ignore it, and an ignored validator enforces nothing. Severity is therefore part of every rule: a rule errors only when violating it breaks the tool, breaks interop, or destroys the user's own content. Everything else warns or advises (§9). |
| 9 | **Not every rule is a tool's to enforce** | A specification states more than a regex can check, and that is not a defect. Each rule names its enforcement mechanism — tool, review, or guidance — so that advice is never mistaken for a broken check, and so that authors are not pushed into writing only what a validator can read (§9). |
| 10 | **Structure comes from the source; the deployed file contributes content, never shape** | On update, the output structure is entirely the source file's — section order, nesting, which regions exist and where. The deployed file is read only for a flat map of injection/custom names to content bytes; no positional information is kept. This is what makes schema reordering safe: there is no second opinion about structure to reconcile against. |

---

## 2. File Anatomy

An agent file has two parts: a YAML frontmatter block (§3) and a body composed of boundary-delimited regions.

### 2.1 The Four Region Kinds

| Tag | Written by | On deploy | On update |
|--------|-----------|-----------|-----------|
| `<Name type="core">` … `</Name>` | MOSAIC source authors | Carried from source byte-identically | Carried from source byte-identically |
| `<Name type="managed">` … `</Name>` | The deployment tool | Body generated from a canonical source | Regenerated wholesale; prior content discarded |
| `<Name type="project">` … `</Name>` | MOSAIC source authors | Carried from source; if non-empty, content is default (§2.1.1) | Preserved byte-identically |
| `<Name type="custom">` … `</Name>` | Project authors | N/A — never in source | Preserved byte-identically |

**`type="project"` vs `type="custom"`:** Both hold project-authored content preserved byte-identically on update. The difference is provenance. A `type="project"` region is **declared in the source file** by MOSAIC — the source defines where it sits, and on schema reorder the content follows the source's new position automatically (principle 10). A `type="custom"` region is **invented by the project** and exists only in the deployed file — it has no source anchor, so on schema reorder the tool parks it at end of file and emits a TODO for the user to reposition it (§6.4).

Four consequences follow and are worth stating outright:

**Never author content inside a managed region.** It is discarded on the next update without warning. In a source file these regions are always empty; the deployed file is the only place they have content.

**A user-owned region (project or custom) may be nested inside a managed region.** The tool preserves nested user-owned regions when regenerating the managed parent — it writes the new canonical text around them. This is the natural placement for project extensions of deployed content (e.g. custom error handling inside `ErrorHandlingCommon`), and it gives the extension an anchor that survives schema reorder.

**Region replacement precedes injection resolution.** Reversed, the tool would resolve an injection and then overwrite the region beside it, discarding content it had just placed.

#### 2.1.1 Project Regions with Default Content

A project region in a source file is usually empty. It may instead carry **default content** — text MOSAIC provides as a starting point that the project is free to replace.

The rule is **deploy once, then forget:**

1. **On initial deploy** (region does not yet exist in the deployed file): the source content — empty or default — is carried into the deployed file. If non-empty, the region is listed in `TODO.md` as reviewable rather than as requiring filling.
2. **On every subsequent update** (region already exists in the deployed file): the deployed content is preserved byte-identically. The source's default content is ignored, even if MOSAIC has changed it since the first deploy.

The consequence is that default content bootstraps the region and then ownership transfers to the project fully. A MOSAIC author updating a default in a source file is updating the experience for *new* deployments only; existing deployments are untouched.

**What counts as default content.** Any non-empty project region in a source file. No attribute or marker is needed — the act of putting content inside a `type="project"` region in a source file is the declaration. The severity table in validation agents (§6.3) is the primary case.

**Principle 3 still holds.** Empty remains the normal state of most project regions. Default content is the exception, used where an agent's instructions reference the region's content and need it to say something coherent on first deploy. A project region whose parent section works fine with it empty should stay empty.

### 2.2 Syntax and Nesting

A region is bounded by an opening tag line and a closing tag line.

**Opening tag form:**

```
<Name type="core">
<Name type="managed" version="1.10">
<Workflow type="core" name="quick-fix">
<Workflow type="core" name="quick-fix" version="3.0">
```

- The tag name is the region name, in PascalCase.
- The `type` attribute carries the ownership kind: `core`, `managed`, `project`, or `custom`. Attribute values are double-quoted.
- A `name` attribute carries the id of a compound/enumerable name (see below).
- A `version` attribute appears when the region carries a version; absent otherwise.
- Canonical attribute order is `type`, then `name` (when present), then `version` (when present).
- The line carries no other content, before or after the tag.

**Closing tag form:**

```
</Name>
```

No attributes, no kind, no id. The tag name alone. Same own-line rule.

**Tag-line matching rule:** A line is a MOSAIC region boundary if and only if the trimmed line is exactly an opening or closing tag and nothing else, **and** the opening tag carries a `type` attribute whose value is one of `core`, `managed`, `project`, or `custom`. A tag without a valid `type` attribute — including any foreign XML-like tag appearing in region content — is inert content, never a boundary. This makes `type` the load-bearing discriminator: for `type="custom"` regions, whose names are user-invented and unknown to the tool, it is the *only* discriminator.

**No self-closing tags.** An empty region uses a standard open/close pair with no body:

```
<CodebaseContext type="project">
</CodebaseContext>
```

Self-closing form (`<CodebaseContext type="project" />`) is not used. An open/close pair lets a human paste content between two lines without editing the tag itself.

**Nesting rules:**

- Core regions (`type="core"`) appear at body top level, except for the orchestrator's template blocks (§5.4).
- Managed (`type="managed"`), project (`type="project"`), and custom (`type="custom"`) regions appear either nested inside a core region or at body top level. Managed-region parents are named in §2.5; project-region parents in §6.2. Custom regions have no source-declared parent — their position is the project author's choice. An empty required parent (`""`) means top level.
- Each boundary name appears at most once per file.

**Compound/enumerable names:** A compound name puts the prefix in the tag name and the id in a `name` attribute:

```
<Workflow type="core" name="quick-fix">
...
</Workflow>
```

Workflows, infrastructure agent declarations, and the canonical blocks in the bundle all use this form, so a tool can enumerate by prefix without knowing the members ahead of time. `Node.Name()` reassembles `TagName + ":" + nameAttr` when a `name` attribute is present, preserving the compound `Prefix:id` form that all downstream consumers depend on. The closing tag uses only the prefix: `</Workflow>`.

**Version attribute:** Where a region carries a version, it appears as a `version` attribute on the opening tag:

```
<CommunicationProtocol type="managed" version="1.10">
```

The value's source of truth is unchanged (the source file's YAML frontmatter `version` field, or the infrastructure agent's declared version). Regions carrying no version do not gain one. The attribute is optional; its absence is not an error at the parser level.

### 2.3 Canonical Document Order

A file's top-level boundaries appear in this order. Every entry is a core region except slot 2, which is a top-level managed region.

| # | Name | Kind | Required for |
|---|------|------|--------------|
| 1 | `Identity` | core | subagent, orchestrator |
| 2 | `CommunicationProtocol` | managed, top level | subagent, orchestrator |
| 3 | `Capabilities` | core | subagent, orchestrator |
| 4 | `Constraints` | core | subagent, orchestrator |
| 5 | `ErrorHandling` | core | subagent, orchestrator |
| 6 | `ExecutionPhilosophy` | core | subagent, orchestrator |

Six slots. There is no separate provenance slot: the artifact provenance stamp is part of the orchestration contract and ships inside slot 2. §16 records the transitional state, since the files have not been migrated yet.

**The rule is "not out of order", not "exactly these six".** A file's top-level boundaries must form a *subsequence* of the list above. Concretely:

- A section may be **absent**. Whether that absence matters is §2.4's question, not this one.
- A file may carry **additional** top-level sections of its own, under any name. They are skipped by this check.
- Any two canonical sections that are both present must appear in the listed relative order. `Constraints` before `Capabilities` is a violation whether or not anything else is missing.

**Why order is fixed but membership is not.** A model attends unevenly across a long prompt, so the position of an instruction is part of the instruction — identity first, then the contract it operates under, then what it can do, then what it must not, then how to fail, then the working posture. That argument justifies the sequence. It does not justify demanding all seven: an agent that genuinely has nothing to say under a heading is better off without it than padding one, and an author with a real need for an eighth section is not writing a broken file. The check stays a single walk over two lists either way.

### 2.4 Role Matrix

| Region | Subagent | Orchestrator |
|--------|----------|--------------|
| `Identity` core region | Required | Required |
| `<ClosingProcedure type="managed">` | Required | Absent (§8) |
| `<AuthorityHierarchy type="managed">` | Required | Absent (§8) |
| `<AvailableWorkflows type="managed">` | Absent | Required |
| `<InfrastructureAgents type="managed">` | Absent | Required |
| `<CommunicationProtocol type="managed">` | Required | Required |
| `Capabilities` core region | Required | Required |
| `Constraints` core region | Required | Required |
| `<ProtocolConstraints type="managed">` | Required | Absent (§8) |
| `<HarnessConstraints type="managed">` | Required | Required |
| `ErrorHandling` core region | Required | Required |
| `<ErrorHandlingCommon type="managed">` | Required | Absent (§8) |
| `ExecutionPhilosophy` core region | Required | Required |
| `<ExecutionPhilosophyCommon type="managed">` | Required | Absent (§8) |

"Required" above means *required by this schema for a well-formed MOSAIC agent*. It does not mean every absence is an error. What an absence costs varies enormously between regions, and §2.4.1 grades it.

A region present with no canonical block matching the file's role **is** an error in every case — the file has asked for text the tool has none to give, and deploying it empty would ship an agent that appears complete and instructs nothing.

#### 2.4.1 What an Absent Managed Region Costs

| Tier | Regions | Absence is | Because |
|------|---------|-----------|---------|
| **Contract** | `CommunicationProtocol` | **Error** | The agent receives no message shape, no status vocabulary, no HITL gate. It cannot produce a response the orchestrator can parse. This is interop, not style. |
| **Conduct** | `ClosingProcedure`, `AuthorityHierarchy`, `ProtocolConstraints`, `ErrorHandlingCommon`, `ExecutionPhilosophyCommon` | **Warning** | The agent still speaks the contract and still works. It works *worse*: no artifact-access imperatives, no retry rule, no ranking when the harness contradicts MOSAIC. Degradation an author should be told about and may knowingly accept. |
| **Deployment** | `HarnessConstraints`, `AvailableWorkflows`, `InfrastructureAgents` | **Silent** | Content comes from a deployment's own selections. Most deployments select nothing for most of them, so absence is the ordinary case and flagging it is noise. |

The tiers are about the *agent's* absence, not the block's. A bundle block with no matching region in a given agent remains normal (`DeployedSectionsBundle.md` §4).

### 2.5 Tool-Managed Names

Every managed-region name, its required parent, and where its content comes from.

| Name | Parent | Content comes from | Specified in |
|------|--------|--------------------|--------------|
| `CommunicationProtocol` | `""` (top level, slot 2) | The contract's own source, role-selected | `CommunicationProtocol.md` |
| `AuthorityHierarchy` | `Identity` | Bundle | `DeploymentBlocks/AuthorityHierarchy.md` |
| `ClosingProcedure` | `Identity` | Bundle | `DeploymentBlocks/ClosingProcedure.md` |
| `AvailableWorkflows` | `Identity` | Assembled from selected workflow files | Workflow schema |
| `InfrastructureAgents` | `Identity` | Assembled from selected infrastructure declarations | `InfrastructureAgentConcept.md` |
| `ProtocolConstraints` | `Constraints` | Bundle | `DeploymentBlocks/ProtocolConstraints.md` |
| `HarnessConstraints` | `Constraints` | The selected harness module | Harness module |
| `ErrorHandlingCommon` | `ErrorHandling` | Bundle | `DeploymentBlocks/ErrorHandlingCommon.md` |
| `ExecutionPhilosophyCommon` | `ExecutionPhilosophy` | Bundle | `DeploymentBlocks/ExecutionPhilosophyCommon.md` |

Nine names. Three sources, and the distinction matters:

- **Bundle** — `Catalog/DeployedSections.md`, the five blocks whose text is identical across every agent of a role and which are not contracts. Governed by `DeployedSectionsBundle.md`.
- **Contract source** — `CommunicationProtocol.md` ships its own text. It is a contract, so it is deliberately not in the bundle and carries its own version.
- **Assembled** — no canonical text exists; the content is built from a deployment's own selections. `AvailableWorkflows` and `InfrastructureAgents` are assembled from what the deployment selected; `HarnessConstraints` comes from the one harness module in play.

#### 2.5.1 Every Managed Name Names Its Generator

**A name belongs in this table only if an implemented generator fills it.** The `Content comes from` column is not a description of intent — it is the requirement, and a name whose generator does not exist does not belong in `CanonicalDeployed`.

This rule exists because its absence caused a real defect. v1.3 and earlier listed `LanguagePatterns` and `CustomConstraints` with the source *"Deployment configuration"* and an em-dash under `Specified in`. No such mechanism was ever built, and no document specified one. The implementation had to route them somewhere, so they fell through a permissive `default:` branch and were classified as harness-sourced — content wiped and refilled from the harness descriptor on every deploy. Nothing in the harness layer had any business supplying either one.

The rest followed mechanically. §2.4 required present managed regions to have a content source, so all four harnesses had to declare `<LanguagePatterns type="managed">` empty purely to satisfy the check. The rationale written into those files records the contradiction in plain words — *"no harness-level content is appropriate here regardless of harness. Declared empty to satisfy the canonical injection list."* An author documented that the classification was wrong and satisfied the validator instead of fixing it, because this document gave them no name for what was wrong.

Two consequences are binding:

- **An unrecognised or unclassified managed name is an error, never a fallback.** A classifier that assigns a default class to a name nobody classified deliberately will assign the wrong one silently, and the closed-set check will then propagate it into every harness and every agent. There is no safe default here; the safe behaviour is to refuse.
- **A name with nothing to fill it is not a managed name.** If content is authored per project, it is a project or custom region (§6.1). If it is authored per agent, it is body text. Neither is a managed region, and giving one a region that no generator fills produces a permanently empty block that every agent carries and no tool can populate.

---

## 3. Frontmatter

### 3.1 Fields

Two groups. Identity fields describe the agent; deployment fields tell the deployment tool how to realise it.

| Field | Type | Required | Group | Description |
|-------|------|----------|-------|-------------|
| `id` | integer string | Subagents only | identity | Stable numeric identifier, assigned once and never changed (§3.3). |
| `version` | semver string | Yes | identity | The agent's own version. Bumped on any change to identity or hand-authored body content (§3.4). |
| `name` | string | Yes | identity | The agent's slug, matching the file's base name. This is the name workflow tables reference and the name that forms the `{AgentName}` half of an `agent_instance_id`. |
| `description` | string | Yes | identity | One line, shown to users during deployment and in agent listings. |
| `role` | enum | Yes | identity | `subagent` or `orchestrator` (§3.2). |
| `model` | string | Yes | deployment | `{model-identifier}` in source; a concrete model id in a deployed file. |
| `tools` | flow list or placeholder | Yes | deployment | Generic tool vocabulary, mapped to harness-specific names at deploy time. `{tool-permissions}` where the deployment supplies the whole list. |
| `recommended_tier` | string | Source only | deployment | The capability tier this agent wants. Open string — no fixed vocabulary. Presented to the user during model selection and used as the key for tier-to-model mapping. Values in current source: `LOW`, `LOW-MEDIUM`, `MEDIUM`, `MEDIUM-HIGH`, `HIGH`. |
| `tier_rationale` | string | Source only | deployment | Why that tier. Shown to the user alongside it, so a person choosing a model has the reasoning and not just the label. |
| `required_skills` | flow list | Source only | deployment | Skill keys this agent's instructions explicitly tell it to load. `[]` when it loads none. Values are folder names under `Catalog/Skills/`. Never inferred from context — a skill appears here only because a Process step names it. |

**"Source only"** means the field is required in source files under `Catalog/` and is consumed by the deployment tool during transformation (for model selection, skill shipping, etc.), but is stripped from deployed output by every harness descriptor's `drop` list. It does not appear in deployed agent files. The three source-only fields — `recommended_tier`, `tier_rationale`, `required_skills` — serve the deployment pipeline, not the agent at runtime.

A deployed file additionally carries `bundle_version`, written by the tool. It is not a source field; see `DeployedSectionsBundle.md` §3.2.

Key order in source is `id`, `version`, `name`, `description`, `role`, then the deployment group. The tool respects source order for keys it does not rewrite; the order matters for round-trip fidelity and for a human scanning a directory of files, not for parsing.

### 3.2 `role`

`role` states what the agent is. Four values:

| Value | What it means | Schema governed | Bundle deployed |
|---|---|---|---|
| `subagent` | An orchestration subagent | Yes — full §2–§4 | Yes |
| `orchestrator` | The orchestrator | Yes — §5 deviations | No (§8) |
| `utility` | A MOSAIC meta-agent (subagent creator, workflow creator, etc.) | No — §5A | No |
| `standalone` | A user-authored agent outside the orchestration system | No — §5A | No |

`subagent` and `orchestrator` are the two values the canonical blocks use in their `applies_to` fields and are the two values this schema fully governs. `utility` and `standalone` are recognised by the deployment tool for harness transformation but are outside this schema's structure rules and deployment bundle (§5A).

**Why it is declared rather than inferred.** Every block the tool selects is keyed on role. Today the tool derives role from the file's path — `Catalog/Orchestrator/` means orchestrator, everything else means not. That works exactly as long as the layout does, which makes a reorganisation of the agent folders a silent change to which contract text ships. Moving a file should not be able to change what an agent is.

There is a second reason, smaller but sharper: a role in frontmatter is checkable against the file's own regions. A file declaring `role: subagent` with no `ErrorHandling` section is detectably wrong (§9). With role inferred from path, the same file is merely unusual.

Infrastructure agents declare `role: subagent`. They are ordinary subagents by file shape; their class and triggers are declared in the orchestrator, not in their own frontmatter.

### 3.3 `id`

A flat integer, assigned when an agent is first created, and thereafter immutable and unique across the agent registry. It survives renames and folder moves, which is what makes it a stable join key between a generic agent and anything that refers back to it.

The orchestrator carries no `id`: there is one of it, it is referenced by role rather than by number, and a sequence number for a set of one informs nobody.

The honest position is that no current consumer has been found for this field — matching is done by `name` everywhere the tooling was inspected. It is retained rather than retired because retiring it is a change to forty-two files and a round-trip contract for no functional gain, and because a stable identifier that survives a rename is cheap to keep and expensive to reintroduce. §15 records the question rather than closing it.

### 3.4 Version Bump Rules

Bump `version` when the agent's hand-authored content changes in a way that alters the guidance it receives.

- **Patch** (`x.y.Z`) — wording, clarification, a corrected example.
- **Minor** (`x.Y.0`) — a new capability, constraint, or process step; a new project or managed region added to the file.
- **Major** (`X.0.0`) — a change to the agent's scope, goal, or litmus test; anything that changes what the agent is for.

**Managed region content does not bump the agent version.** When a canonical block changes, forty-two agents receive new text and none of them change version — their own source did not change, and a bump would claim an authorship that did not happen. The bundle's own version records that change, and staleness is detected against it.

### 3.5 What Frontmatter Does Not Carry

**No `function` or `category` field.** The agent's function is its folder — `Research/`, `Planning/`, `Validation/`, `Creation/`, `Execution/`, `Interface/`, `Audit/`, `Infrastructure/`. A frontmatter copy would be a second statement of one fact, free to disagree with the first, and the disagreement would be invisible: a file in `Validation/` declaring `function: research` is not detectably wrong to anything, because nothing has grounds to prefer one over the other. Where a consumer eventually needs function in machine-readable form, deriving it from the path is exact and cannot drift.

**No `agent_class`.** Class is an infrastructure concept — `checkpoint`, `commit`, `restore`, `review` — declared in the orchestrator's `InfrastructureAgents` region, which is the runtime contract the executor actually reads. Putting it in agent frontmatter would create a second declaration that no executor consults, and the two would be free to disagree about what an agent is.

**No restatement of the contract's vocabularies.** Status codes, error codes, and stamp field names are stated in the canonical block that ships. A frontmatter copy would be the less authoritative one, so a conformance check reading it would pass in exactly the case it exists to catch.

The general test for a proposed field: name a consumer that cannot do its job without it, and confirm the fact is not already recoverable from the file's path, its regions, or a canonical block.

---

## 4. Section Specifications — Subagent

Each subsection states what the section is for, what an author writes, and what is deployed into it. Regions are listed in the order they appear.

### 4.1 Identity

**Purpose:** who the agent is and what it does. This is the section a model attends to hardest, and the only one that is almost entirely agent-specific.

**Author writes:**

```markdown
# {Agent Display Name}

You are the **{Agent Display Name}** agent in a multi-agent orchestration system.

**Goal:** {One sentence. The agent's single responsibility, stated so that a reader can tell whether a given task is in scope.}

**Scope:**
- You DO: {each in-scope activity, one per line}
- You DO NOT: {each out-of-scope activity, one per line, framed as another agent's job}

**Litmus Test:** If it involves {X} → you handle it. If it involves {Y} → other agents handle it.

### Process
1. {First work step}
2. {…}
N. {Write results to output artifacts and files}
```

**Rules for the Process list:**

- It contains **work steps only**. It does not end with a HITL step or a "return JSON" step — both are deployed by `ClosingProcedure` immediately below it, and an agent-authored version of either is a defect.
- Where the agent loads a skill, that is step 1, it names the skill, and it states what to do if loading fails. The skill also appears in `required_skills`.

**Managed regions, in order after the Process list:**

| Region | Block |
|--------|-------|
| `<ClosingProcedure type="managed">` | `ClosingProcedure:Subagent` |
| `<AuthorityHierarchy type="managed">` | `AuthorityHierarchy:Subagent` |

The order is not arbitrary: the closing procedure continues the Process list and must read as its final steps, so nothing goes between them.

**Why Scope is stated positively and negatively.** The DO list alone defines a centre without an edge, and an agent asked to do something adjacent will reason its way in. The DO NOT list is where the single-responsibility architecture is actually enforced, and it works best when each entry names the other agent's job rather than a prohibition: "requirements are defined elsewhere" is followed more reliably than "do not define requirements", because the first tells the agent what to do with the request.

### 4.2 CommunicationProtocol

Top-level managed region `<CommunicationProtocol type="managed">`, slot 2. Empty in source. Content is the role-matched block from `CommunicationProtocol.md`, which is the whole orchestration contract: message shape, status and error vocabularies, the HITL gate, and the artifact provenance stamp.

Nothing is authored here, and no project injection sits inside it — the region is regenerated wholesale, so anything nested would be destroyed on the next deploy (§2.1). A project needing to extend the protocol uses `<ProtocolExtension type="custom">` as a top-level sibling of this region; §6.2.1 covers what belongs there and what does not.

### 4.3 Capabilities

**Purpose:** what the agent can do, and the shape of what it produces.

**Author writes:**

```markdown
## Capabilities

### Core Capabilities
- {Capability, phrased as an ability rather than an instruction}
- {…}

### {Agent-specific subsections as needed}
{Output artifact templates, method-specific principles, what the agent creates,
what it treats as authoritative input — whatever this agent's work requires.}

### Agent-Specific Artifact Behavior
{Only where the agent's artifact handling goes beyond the contract's access rules.
Examples: "preserve existing content, do not delete prior findings"; "track progress
in the output artifact". Omit the subsection entirely where there is nothing to say.}
```

Capabilities is the least constrained section by design. It is where an agent's actual expertise lives, and expertise does not have a common shape — a research agent needs an output template, an implementation agent needs its method's principles, an interface agent may need a detailed artifact schema that lives entirely in core prose because it defines an inter-agent contract rather than a project-customisable output shape (§6.5).

**Managed:** none. The section carries no tool-managed region.

**Project injections:** `<CodebaseContext type="project">`, `<OutputArtifactTemplate type="project">`, and for validation agents `<SeverityThresholds type="project">` and `<SeverityDefinitions type="project">` (§6.3). `<LanguagePatterns type="custom">` is available to a project with language-specific patterns to state — it is a custom region, not declared in any source file (§6.1).

**Tool access is not described here.** It is the `tools` frontmatter field. A prose list of tools in the body is a second statement that goes stale the first time the field changes.

### 4.4 Constraints

**Purpose:** what the agent must not do, and why.

**Managed first:** `<ProtocolConstraints type="managed">`, at the top of the section.

**Author writes** the agent's own constraints after it. Two rules:

- **Every constraint carries its justification.** "Do NOT make assumptions about technology choices — document options instead, because downstream agents need unbiased options to evaluate against broader context." A bare prohibition invites a model to find the edge of it; a prohibition with a reason gives it grounds to generalise correctly to a case the rule did not anticipate.
- **Do not restate the contract.** Artifact access, status code discipline, and JSON response discipline are already stated twice above — once in the contract region, once in `ProtocolConstraints`. A third copy in the agent's own list is the copy that drifts.

**Managed last:** `<HarnessConstraints type="managed">`.

### 4.5 ErrorHandling

**Purpose:** which status code this agent returns, in which situation.

**Managed first:** `<ErrorHandlingCommon type="managed">`.

**Author writes** the agent's own status mapping after it — and this is the part that must be genuinely agent-specific, because it is the only place the six generic codes are grounded in one agent's work:

```markdown
- **Return CAPABILITY_EXCEEDED** if {what exceeding capability looks like for this agent}
- **Return NEEDS_CLARIFICATION** if {what ambiguity looks like for this agent}
- **Return COMPLETED_NEEDS_ACTION** if {what a finding looks like for this agent, or state
  that this agent rarely returns it}
- **Return SUCCESS** when {what complete means for this agent}
- **Return PARTIALLY_DONE** if {what a deliberate stop looks like for this agent}
```

A mapping that could be pasted into any agent unchanged is a mapping that has not been written. Its usefulness is entirely in the specificity — `COMPLETED_NEEDS_ACTION` is the expected outcome for a review agent and a rare one for a research agent, and only this section can say so.

An agent may add a subsection where one distinction is repeatedly got wrong in practice, as several do for `NEEDS_CLARIFICATION` versus `BLOCKED`.

### 4.6 ExecutionPhilosophy

**Purpose:** the working posture — how the agent should think about context, quality, and its own limits.

**Managed first:** `<ExecutionPhilosophyCommon type="managed">`.

**Then** `<ContextLimits type="project">`.

**Then the agent's own bullets** — the mindset specific to this work. "Investigation only: report observations, not assessments." "Escalate, don't fight: if tests seem wrong, return `NEEDS_CLARIFICATION` rather than working around them." These are the section's reason to exist, and they belong to the agent.

---

## 5. Orchestrator Deviations

The orchestrator uses the same frontmatter schema, the same region kinds, and the same section names. Its body content is almost entirely its own.

### 5.1 Sections whose content is orchestrator-specific

`Identity`, `Capabilities`, `Constraints`, `ErrorHandling`, and `ExecutionPhilosophy` are all present and share nothing but their names with the subagent versions. The orchestrator's authority hierarchy ranks workflow configuration and subagent responses, not its own scope boundaries. Its constraints are about context discipline and append-only state. Its error handling is a tiered retry-and-escalate strategy for codes it receives rather than a mapping for codes it returns. None of that is a variant of subagent text; it is different text that happens to live under the same heading.

### 5.2 Regions unique to it

`<AvailableWorkflows type="managed">` and `<InfrastructureAgents type="managed">`, both inside `Identity`, both assembled at deploy time from selected workflow and infrastructure declarations.

### 5.3 Nested template sections

The orchestrator's `Capabilities` contains nested core regions — `<ExecutionLog type="core">`, `<Artifacts type="core">`, `<WorkflowNotes type="core">` — holding the templates it writes into the orchestration artifact. These are the one sanctioned use of nested core regions, and they are nested because they are content *about* a capability rather than a document-level slot.

---

## 5A. Utility and Standalone Agents

Two roles live in the catalog but are not governed by this schema's structure rules or deployment bundle.

| | `utility` | `standalone` |
|---|---|---|
| **Lives in** | `Catalog/UtilityAgents/` | `Catalog/StandaloneAgents/` |
| **What it is** | A MOSAIC meta-agent — tooling for the orchestration system itself (subagent creator, workflow creator, orchestration architect, etc.) | A user-authored agent outside the orchestration system entirely |
| **Boundary tags** | May or may not use them — author's choice | May or may not use them — author's choice |
| **Template structure** | May or may not follow §4 — author's choice | May or may not follow §4 — author's choice |

**What the tool does with them:** harness transformation — frontmatter mapping, model substitution, tool name mapping. The same mechanical transforms that turn a source file into a harness-specific deployed file. It processes what it finds in the file and does not require or report anything that is not there.

**What the tool does not do:**

- No bundle deployment. No managed regions are filled, no `bundle_version` is stamped. If a utility agent happens to carry a managed region, the tool leaves it as-is.
- No structural validation. No check for canonical section order, required regions, or role-matrix compliance. These agents are not promised to conform, so checking for conformance is noise.
- No missing-region reporting. A utility agent with three sections instead of seven is not degraded — it is a three-section agent.

**Why they are in the catalog at all.** The deployment tool needs a source file to transform. Keeping these agents in the catalog alongside subagents means one tool, one pipeline, one set of harness descriptors. The alternative — a separate transformation mechanism for non-orchestration agents — would duplicate machinery for a difference that is entirely about what gets skipped.

The precise scope of tool behavior for these roles is expected to evolve with experience and is not fully specified here.

---

## 6. Injection and Custom Points

### 6.1 Two Kinds of Project-Owned Region

Both project (`type="project"`) and custom (`type="custom"`) regions hold project-authored content preserved byte-identically on update. They differ in provenance and in how the tool handles them on schema changes:

| | `type="project"` | `type="custom"` |
|---|---|---|
| **Declared in** | MOSAIC source files | Deployed files only (project-invented) |
| **Position on update** | Follows the source — if the source moves it, content moves with it | No source anchor — position is wherever the project put it |
| **On schema reorder** | Automatically repositioned per the new source (principle 10) | Parked at end of file with a TODO to reposition (§6.4) |
| **Name set** | Catalogued below; most declared empty in source | Fully open — any name the project invents |

**The practical rule:** if MOSAIC defines the slot, it is a project region. If the project invents the slot, it is a custom region.

### 6.2 Source Injection Catalogue

The names below are the project regions MOSAIC declares in source files. They are preserved by name-matching between deployed and source on update — a name the author put in the source is a name that will be there next time.

Most are carried by MOSAIC's own sources, declared empty so a project can see the slot exists. `LanguagePatterns` is the exception and is deliberately **not** declared anywhere in `Catalog/`: a language pattern is meaningful only once a project has a language, so pre-declaring it in every agent would ship an empty region and a `TODO.md` line to fifty-odd files on the chance that one project fills them. A project that wants it uses `<LanguagePatterns type="custom">` in their deployed file.

| Name | Usual parent | Purpose |
|------|--------------|---------|
| `CodebaseContext` | `Capabilities` | Knowledge of the project's codebase |
| `OutputArtifactTemplate` | `Capabilities` | The project's expected structure for this agent's output artifact |
| `SeverityThresholds` | `Capabilities` | Which issue severities require rework (validation agents) |
| `SeverityDefinitions` | `Capabilities` | What each severity means in this project (validation agents) |
| `ContextLimits` | `ExecutionPhilosophy` | Context window thresholds and guidance |

"Usual parent" is where the region belongs when the agent is otherwise conventional, and where MOSAIC's own agents put it. Placing one elsewhere is an author's call, not a validation failure. The single hard rule about placement is §6.5's: never inside a managed region.

#### 6.2.2 Stability Obligations

Two development disciplines protect deployed agents from avoidable rework when this catalogue changes:

**Declare injection points upfront.** An agent's source injection set should be declared in the agent's first version. Adding a new project region to a source file that is already deployed into projects creates a name-collision risk: if any project has a custom region with the same name, the deploy tool aborts until the project renames their custom region. Injection points are rare enough that getting the set right once is feasible; adding them later is never free.

**Rename through the rename table, never by hand.** Renaming a source injection without adding a corresponding entry to the rename table (§10.1) strands every project's content under the old name — it appears as an orphan in `TODO.md` while the new name deploys empty. The rename table exists to make this automatic. Omitting the entry is a development error, not a design gap, and the orphaned content must be rescued manually from `TODO.md` for every affected deployed agent. The table entry is part of the rename, not an afterthought.

#### 6.2.1 Protocol Extension

`ProtocolExtension` is not a catalogued injection name. A project needing to extend the protocol uses `<ProtocolExtension type="custom">` as a top-level sibling of `<CommunicationProtocol type="managed">` — never inside it, since the contract region is regenerated wholesale and would take the extension with it. Because custom regions are project-invented, no source declaration is needed; the project adds the region directly to their deployed files.

The use case is unchanged: a deployment can have real business extending the protocol without altering it. A project whose subagents run behind network endpoints while the orchestrator runs locally needs to say *how a message is delivered*; nothing in the contract covers transport, and that project's alternative is forking the contract, which is strictly worse for everyone.

**The guidance, which is guidance and not a check:** extend the mechanics — transport, delivery, environment-specific handling. Do not restate or redefine what the contract already fixes: message shape, the status and error vocabularies, the HITL gate, the provenance stamp. An extension contradicting the contract leaves the agent with two answers and no basis to choose between them, and it will choose the nearer one (principle 7). That is a real hazard, and it is the project's to own — the same as any other custom region.

**`ArtifactProvenanceExtension`** is retired rather than forbidden. The stamp it extended has folded into the orchestration contract, so the region no longer has a counterpart to extend; a project needing extra artifact frontmatter uses `<OutputArtifactTemplate type="project">`, which governs artifact content rather than the contract. A file still carrying the region is stale, not invalid, and its content is preserved like any other project injection.

### 6.3 Severity Injections

Validation agents carry two additional project injections. `SeverityThresholds` states which issue severities require rework; `SeverityDefinitions` optionally states what each severity level means in the project's terms.

`SeverityThresholds` carries default content (§2.1.1): a threshold table ships in the agent's source file, deploys on first deploy, and is the project's property from that moment on. Subsequent source updates to the default do not touch a deployed version.

**The default text itself lives in the validation agents' source files and nowhere else.** This document places the region and fixes its semantics; what a given agent's table actually says is that agent's decision, tuned to the cost of a missed issue in the artifact it reviews. A copy of the table here would be a second authority over a value that is expected to vary between agents and to change with experience — exactly the drift principle 4 exists to prevent.

What the region resolves, whatever numbers a project puts in it, is the agent's status code: any issue at a severity the table marks as requiring rework means `COMPLETED_NEEDS_ACTION`; otherwise `SUCCESS` with the issues recorded in the report. That rule is the contract between the table and the agent's `ErrorHandling` section (§4.5), and it holds for every threshold table anyone might write. CRITICAL requiring rework is not configurable — a validation agent that can be configured to pass critical findings is not a validation agent.

**In source files, the threshold table and its accompanying status-code logic sit inside the `<SeverityThresholds type="project">` region**, not after it. Content outside the region is core text and would not be preserved as the project's on update — the severity guidance must be inside the region it belongs to.

### 6.4 Custom Regions and Schema Reorder

Custom regions exist only in deployed files. The project author writes them directly; the source has no knowledge of them.

**On normal update (no structural change):** custom regions are preserved byte-identically in their current position, exactly like project regions. The tool reads them into the name->content map and writes them back where they were.

**On schema reorder** (section order in deployed file differs from section order in source):

1. Custom regions nested inside a source section or managed region that still exists **stay anchored to that parent** and move with it — same behavior as a source injection.
2. Custom regions whose parent context disappeared or is no longer identifiable are **parked at the end of the deployed file**, after all sections.
3. The tool emits a **TODO** entry: "Schema reorder detected. These custom regions were preserved at end of file — move them to the correct section."

The tool does not need to distinguish "parked" from "always at end of file". On the reorder update, it parks and notifies. On every subsequent update, end-of-file custom regions are just regions — name->content in, name->content out. The tool is stateless about parking.

### 6.5 Rules

One is enforced. The rest are guidance, and marked as such.

**Nesting inside managed regions is permitted.** Project and custom regions may be nested inside a managed region. The tool preserves them when regenerating the managed parent — it writes the new canonical content and keeps nested user-owned regions intact. This replaces the former rule 13 ban, which existed because the tool previously destroyed nested content on regeneration.

**Guidance:**

- **A project region earns its place when both conditions hold: it is generally applicable (most projects would have something to put there) and providing it improves agent performance.** `CodebaseContext` passes: every project has a codebase, and agents that know the repo layout, tech stack, and conventions navigate, search, and generate code measurably better. `IdentityExtension` fails: "domain expertise" is too abstract for most projects to act on, and its effect on agent behavior is negligible. A region that fails either condition is noise — an empty slot every project author must read and dismiss in their `TODO.md`. A project with a genuine need the catalogue does not cover uses `type="custom"` in their deployed file.
- **An artifact template that is an inter-agent contract stays in core prose, not in `OutputArtifactTemplate`.** When an output artifact's schema is consumed by other agents whose core instructions reference its field names, types, or structure, the template defines a contract between tightly coupled workflow participants. Making it project-customisable via `OutputArtifactTemplate` would let one side of the contract change without the other — the producing agent would write a new shape while every consuming agent's core text still hardcodes the old one. The template belongs as ordinary core prose inside `Capabilities`, where it cannot be modified independently of the instructions that depend on it.
- **A template must work with every injection empty or at its default.** That is the state of every source file and of every fresh deployment; an agent that only makes sense once a project has filled something in is broken by default. Where a project region carries default content (§2.1.1), "works" means the default is coherent — the agent's instructions must not contradict or depend on content the default does not provide.
- **Injections extend; they do not contradict.** An injection redefining a rule stated in a managed region leaves the agent two answers with no basis to choose, and it will follow the nearer one (principle 7). The tag itself does not convey this, so the `TODO.md` text for each injection should.

**No injection or custom region is ever required to be filled.** Empty is the normal state, not a degraded one (principle 3), and unfilled regions belong in the `TODO.md` checklist rather than in a validator's output. This matters most where a parent section offers several alternative child injections: filling all of them is not the goal, and a rule demanding it would be demanding contradictory content. Which of them a project uses is the project's decision and nobody else's.

---

## 7. The Canonical Blocks

Five fragments of shared body text are deployed into every subagent. Their text lives in `Catalog/DeployedSections.md` and nowhere else; their reasoning lives one document per block.

| Block | Fills region | Parent section | Reasoning |
|-------|--------------|----------------|-----------|
| `AuthorityHierarchy:Subagent` | `<AuthorityHierarchy type="managed">` | `Identity` | `DeploymentBlocks/AuthorityHierarchy.md` |
| `ClosingProcedure:Subagent` | `<ClosingProcedure type="managed">` | `Identity` | `DeploymentBlocks/ClosingProcedure.md` |
| `ProtocolConstraints:Subagent` | `<ProtocolConstraints type="managed">` | `Constraints` | `DeploymentBlocks/ProtocolConstraints.md` |
| `ErrorHandlingCommon:Subagent` | `<ErrorHandlingCommon type="managed">` | `ErrorHandling` | `DeploymentBlocks/ErrorHandlingCommon.md` |
| `ExecutionPhilosophyCommon:Subagent` | `<ExecutionPhilosophyCommon type="managed">` | `ExecutionPhilosophy` | `DeploymentBlocks/ExecutionPhilosophyCommon.md` |

All five are `applies_to: subagent`; §8 says why the orchestrator carries none. Membership, versioning, and deployment mechanics are in `DeployedSectionsBundle.md`. The measurement that identified these five, and the decision that produced them, is `Development/Analysis/AgentBodyDrift.md`.

**This document holds none of their text**, and neither does any other document. That rule is what keeps the payload and the rationale from becoming two copies of the same thing; rule 23 of §9 is its mechanical form.

**What remains hand-authored.** After the migration, a subagent source file contains agent-specific prose and nothing else: the Identity block through the Process list, the Capabilities section, the agent's own constraints, its status mapping, its `status_message` and `error_code` table, and its philosophy bullets. Every one of those differs meaningfully between agents. If a future measurement finds a line identical across all agents in that set, it is a candidate for the bundle by the same argument.

---

## 8. Why the Orchestrator Carries No Canonical Body Regions

All five blocks are subagent-only, and the orchestrator's equivalents stay hand-authored in its own file. Three reasons, in increasing order of weight.

**There is one orchestrator.** Single-sourcing exists to stop forty-two copies from diverging. One copy cannot diverge from itself. Deploying the orchestrator's own text into the orchestrator's own file from a third file would add a hop and a staleness surface to buy nothing.

**Four of the five are not variants of the subagent text.** `ClosingProcedure` has no counterpart at all: the orchestrator returns no protocol response and closes no task. `ErrorHandlingCommon` inverts — the orchestrator's error handling is a tiered retry-and-escalate strategy for codes it *receives*, not a mapping for codes it returns. `ProtocolConstraints` and `ExecutionPhilosophyCommon` are about context discipline and append-only state rather than artifact access and status selection. Writing orchestrator versions would mean four new blocks sharing no sentence with their counterparts, maintained in a file the orchestrator's author does not otherwise open.

**Provenance is the clearest case.** The orchestrator writes one file, the orchestration artifact, whose schema already sets its own frontmatter obligations — and `created_by` has no value it could carry, since instance ids are minted *by* the orchestrator *for* subagents. This is why the contract's orchestrator-role block differs from its subagent-role block rather than the orchestrator simply carrying less.

**The one that does not fit this argument, and the cost of including it anyway.** `AuthorityHierarchy` *is* a genuine variant — the orchestrator's version shares three of five ranks and its closing rationale with the subagent block. The two state one ranking principle for two readers, so the risk is not two copies drifting by accident but the principle being amended in one role and not the other. That is not hypothetical: the harness went unranked in both, was fixed for subagents when the fragment was single-sourced, and was fixed for the orchestrator only because someone noticed the connection by hand. Deploying it was considered and rejected on cost (`DeploymentBlocks/AuthorityHierarchy.md` §6), which leaves a standing review obligation rather than a mechanism: **an amendment to either hierarchy is checked against the other.**

The consequence for the tool: none of the five blocks declares `applies_to: orchestrator`, the orchestrator source carries none of the five regions, and neither fact is an error (§2.4).

---

## 9. Conformance Rules

### 9.1 How a Rule Is Enforced

MOSAIC is a recipe for people who have not written agents before, and it has to stay usable by people who have. Those two audiences pull in opposite directions, and the way to serve both is not a weaker set of rules but a rule set that says, of each entry, **what happens when you break it.**

Two axes do that. Every rule carries both.

**Severity — how loudly.**

| | When |
|---|---|
| **Error** | The tool cannot proceed, interop breaks, or the user's own content is destroyed. Never a matter of taste. |
| **Warning** | The agent works and is worse for it. An author should hear this and may knowingly accept it. |
| **Advice** | Reported where it helps, ignorable without consequence. |

The bar for *error* is deliberately high. An unusual but working agent must pass, because a validator that flags legitimate work is one its user learns to switch off (principle 8).

**Mechanism — by whom.**

| | Who |
|---|---|
| **Tool** | The validator. |
| **Review** | A person or a validation subagent, working from a checklist. Not a lesser form of enforcement — MOSAIC has agents, so a review rule is something that actually runs. |
| **Guidance** | Nothing enforces it. It is here to make an author's judgement better. |

Splitting the mechanism out matters more than it looks. Without it a specification drifts towards containing only what a regex can read, and §4's best material — *"a status mapping that could be pasted into any agent is a mapping that has not been written"* — is precisely what no regex can read (principle 9).

### 9.2 Strict Mode

The same rules, two appetites, selected by which tree is being checked.

| Tree | Behaviour |
|---|---|
| **MOSAIC's own sources** (`Catalog/**`) — strict | Every rule is an error, warnings included. CI fails. |
| **Anyone else's agents** — lenient | Errors are errors. Warnings and advice are reported, and reported into `TODO.md` and the deployment summary rather than only to a console, because the author who most needs them is the one least likely to read stderr. |

The asymmetry is the point rather than a compromise. MOSAIC's forty-two agents *are* the recipe, so they are held to all of it; a project's own agent is held to what would otherwise break. A newcomer who fills in the blanks passes either way, and an expert writing something the template did not anticipate is never blocked.

### 9.3 The Rules

`Tools/Common/docformat/validate.go` is the only validator today and inspects boundary structure only. The Implemented column records that; **this document is the authority and the tool is expected to follow it**, so a rule reading *no* is a tool to change, not a rule to weaken.

**Frontmatter**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 1 | All source fields present, `id` present exactly when `role: subagent` | Error | Tool | No |
| 2 | `role` is `subagent` or `orchestrator` | Error | Tool | No |
| 3 | `name` matches the file's base name | Error | Tool | No |
| 4 | `version` parses as semver | Error | Tool | No |
| 5 | `required_skills` entries name existing folders under `Catalog/Skills/` | Error | Tool | No |
| 6 | `id` is unique across the agent registry | Error | Tool | No |

All errors, and none of them can flag a creative agent: each is a field the tool reads, and a value it cannot use is a value that fails.

**Structure**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 7 | Top-level boundaries form a subsequence of the canonical order (§2.3) | Error | Tool | Partly — currently rejects unknown top-level names |
| 8a | `<CommunicationProtocol type="managed">` present (§2.4.1) | Error | Tool | No |
| 8b | The five conduct regions present for `role: subagent` (§2.4.1) | Warning | Tool | No |
| 8c | No region present that the role forbids (§2.4) | Warning | Tool | No |
| 9a | Every managed region sits under the parent named in §2.5 | Error | Tool | Yes |
| 9b | Every project region sits under its usual parent (§6.2) | Advice | Tool | Yes — should be downgraded |
| 10 | No boundary name appears twice in one file | Error | Tool | Yes |
| 11 | Every opened boundary is closed, with matching name | Error | Tool | Yes |
| 12 | In a source file, every managed region is empty | Error | Tool | No |
| 13 | Project and custom regions nested inside managed regions are preserved on regeneration | — | Tool | No — tool currently destroys them |
| 14 | Every managed name is one the tool has a source for (§2.5) | Error | Tool | Yes |

Rule 14 replaces the former "`ProtocolExtension` does not appear". Project-region names are open and unlisted names are preserved, not orphaned (§6.2); managed names stay closed, because an unrecognised one leaves the tool with a slot and no content.

Rule 9b is the clearest case of a check to relax: it currently errors on a project injection placed somewhere unusual, which harms nobody and destroys nothing.

**Content**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 15 | `Identity` contains `**Goal:**`, `**Scope:**` with a DO and a DO NOT, `**Litmus Test:**`, and a `### Process` list | Warning | Tool | No |
| 16 | No section contains a JSON object with a `status_code` key — the contract region owns the envelope | Warning | Tool | No |
| 17 | *(Removed in v2.0 — `OutputFormat` section deleted)* | — | — | — |
| 18 | No Process step mentions `human_in_the_loop` — `ClosingProcedure` deploys that | Warning | Tool | No |
| 19 | Every skill named in a Process step appears in `required_skills`, and vice versa | Error | Tool | No |
| 15r | Every constraint carries its justification (§4.4) | Advice | Review | — |
| 16r | The status mapping is specific to this agent, not paste-able into another (§4.5) | Advice | Review | — |
| 17r | *(Removed in v2.0 — `OutputFormat` section deleted; status mapping lives in ErrorHandling §4.5)* | — | — | — |
| 18r | The agent's scope does not overlap an existing agent's | Advice | Review | — |

Rule 19 is an error despite sitting under Content: a skill an agent is told to load but does not declare is a skill the deployment does not ship, and the agent fails at step 1. That is a broken deployment, not a style lapse.

Rules 15–16 and 18 warn in a project tree and error in MOSAIC's own.

The `r` rules are what §4 already asks for in prose. They are listed here so the specification stops implying that its unenforceable half does not count — and because MOSAIC can hand them to a validation subagent as a checklist.

**Bundle**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 20 | Every bundle block declares a valid `target`, `applies_to`, and an existing `specified_in` | Error | Tool | No |
| 21 | Every deployed agent's `bundle_version` equals the bundle's, and all agents in one deployment agree | Warning | Tool | No |
| 22 | Every bundle-sourced managed region's body equals its block byte-for-byte | Warning | Tool | No |
| 23 | No document outside the bundle contains a block's opening or closing content line | Warning | Review | No |

21 and 22 warn because a stale or hand-edited deployment still runs — the user is being told to redeploy, not stopped. 22 catches a hand-edited deployed file; 23 catches a design document that started quoting what it was only supposed to explain. Elaborated in `DeployedSectionsBundle.md` §9.

### 9.4 Cheapest First

Rules 1–4 and 14 are a morning's work and are pure integrity. Rules 16–18 are a regex over one section each and cover the defect class that has cost the most so far. Rule 9b's downgrade and rule 7's relaxation are subtractions. None of it is blocked on the bundle work.

---

## 10. Migration

The migration touches forty-two subagent files. It is mechanical apart from one review step.

1. **Add `role: subagent`** to all forty-two, `role: orchestrator` to the orchestrator.
2. **Replace the Authority Hierarchy block** with an empty `<AuthorityHierarchy type="managed">` region.
3. **Delete the trailing HITL and return-JSON steps** from every Process list; add an empty `<ClosingProcedure type="managed">` region after the list, before the authority hierarchy region.
4. **Replace the five contract-restating constraint bullets** at the top of `Constraints` with an empty `<ProtocolConstraints type="managed">` region, keeping every agent-specific constraint below it.
5. **Replace the retry bullet** at the top of `ErrorHandling` with an empty `<ErrorHandlingCommon type="managed">` region, keeping the agent's status mapping. Delete any error-code recall bullet outright — the contract's region carries the full table.
6. **Replace the Context Management, Memory via Artifacts, and Quality over Completeness bullets** with an empty `<ExecutionPhilosophyCommon type="managed">` region at the top of `ExecutionPhilosophy`, ahead of `<ContextLimits type="project">`.
7. ~~**Rewrite `OutputFormat`**~~ **Superseded.** The `OutputFormat` section was removed entirely in v2.0 rather than rewritten. The `status_message` examples and `error_code` choices it would have carried were deleted from all forty-seven agents: the Communication Protocol already supplies the contract, `ErrorHandling` already carries the agent-specific status mapping, and the worked examples were actively causing verbose message parroting. If experience shows agents need more status guidance, `ErrorHandling` (§4.5) is the place to expand.
8. **Update the three vocabulary files** together (§10.1).
9. **Bump each agent's `version`** — minor, since regions were added and hand-authored content was removed. The bundle version does not move: its blocks did not change, only their destinations came into existence.

Steps 2–6 are verifiable two ways: after deployment, each region must be byte-identical across all forty-two files and to its block in the bundle (uniformity), and everything outside the touched regions must be byte-identical to the pre-migration file (isolation). Step 7 is not mechanically verifiable and needs review; rule 17 confirms only that no envelope survived.

**Before migrating any fragment,** diff its full block across all forty-two files. The counts in the drift analysis each test one representative line and are a triage signal, not a verification.

**Ordering against the provenance merge.** Folding provenance into the orchestration contract removes a top-level region from the same forty-two files. The two changes touch disjoint regions and may land in either order, but not concurrently — both rewrite the same files, and the isolation check of each requires the other not to be running.

### 10.1 Vocabulary tables

Three files hold the boundary vocabulary in machine-readable form: `Catalog/SourceFilesFormat.md`, `Tools/Common/docformat/vocabulary.go`, and `Tools/OldAgentsTransform/boundary_constants.py`. This document is their specification; they are copies of it, and a partial update leaves them disagreeing about what is valid.

Changes this document makes to them:

- `CanonicalDeployed` gains `AuthorityHierarchy`, `ClosingProcedure`, `ProtocolConstraints`, `ErrorHandlingCommon`, `ExecutionPhilosophyCommon`. It remains a closed set (rule 14).
- `CanonicalDeployed` loses `ArtifactProvenance`, and `CanonicalSections` does not gain it — the region ceases to exist on the provenance merge.
- `CanonicalDeployed` loses `LanguagePatterns` and `CustomConstraints` (§2.5.1), leaving nine names. `DeployedParent` loses the same two entries. `LanguagePatterns` becomes a catalogued injection name (§6.1); `CustomConstraints` ceases to exist in any vocabulary.
- The managed-name classifier's `default:` branch stops resolving to the harness class. `HarnessConstraints` becomes an explicit case, and an unclassified name is an error (§2.5.1).
- `DeployedParent` gains the five new names with the parents in §2.5.
- `CanonicalOrder` becomes six slots (seven before v2.0 removed `OutputFormat`), of which slot 2 is managed, and is consumed as a subsequence rather than an equality check (§2.3).
- `InjectionParent` **stops being an allowlist.** Project-region names are open (§6.2): an unlisted name is preserved like any other. What remains is a table of usual parents for the suggested names, consulted for advice-level reporting only. `ArtifactProvenanceExtension` leaves it; `ProtocolExtension` is absent — projects use `<ProtocolExtension type="custom">` instead of a project injection, following the rule that MOSAIC defines project slots in source and projects invent custom regions in deployed files.

`Catalog/SourceFilesFormat.md` should be reduced to a pointer at this document plus the skill and hook-bundle conventions it uniquely covers, rather than restating the agent format alongside it (§16).

---

## 11. Non-Goals

- **A copy-paste template.** A complete agent file offered for copying is how forty-two copies of shared text came to exist. §4 specifies each section; the bundle supplies the shared text at deploy time. Neither is a thing to paste.
- **Contract wording.** Placed here, specified in `CommunicationProtocol.md`.
- **Canonical block wording.** Placed here, reasoned about in `DeploymentBlocks/`, and held only in the bundle.
- **Utility and standalone agent tool behavior.** §5A states what the tool does and does not do at a principle level. The precise scope is expected to evolve with experience and is not fully specified here.
- **Infrastructure agent declaration.** Class, triggers, and failure policy are declared in the orchestrator, not in agent frontmatter (§3.5).
- **Harness-specific content.** What goes into `HarnessConstraints` and how a harness maps generic tool names are the harness modules' business.
- **Agent quality.** Whether an agent's scope is well-chosen, whether its constraints are justified, and whether it overlaps another agent are review questions. This document specifies the container.
- **Skill and hook-bundle formats.** Different artefact kinds.
- **Enforcing that deployed text is read.** Structure gets the right words in front of a model. Nothing here makes it follow them.

---

## 12. Glossary

| Term | Meaning |
|------|---------|
| **Agent file** | A markdown file with frontmatter and boundary-delimited regions, deployed as an agent's system prompt. |
| **Source file** | The MOSAIC-maintained agent file under `Catalog/`. Managed regions are empty; project injections are empty. |
| **Deployed file** | The output of running the deployment tool over a source file. Managed regions are filled; project injections carry project content. |
| **Region** | A named span of a file bounded by a matched pair of tags. |
| **Core region** | A `type="core"` region — MOSAIC-authored, carried verbatim. |
| **Managed region** | A `type="managed"` region — written by the tool, regenerated on every deploy. |
| **Project injection** | A `type="project"` region — declared in source, project-filled, preserved byte-identically. Follows source position on reorder. |
| **Custom region** | A `type="custom"` region — project-invented, exists only in deployed files, preserved byte-identically. Parked at end of file on schema reorder. |
| **Canonical block** | Deployed text maintained in one place and copied into every agent carrying the matching region. |
| **Bundle** | `Catalog/DeployedSections.md` — the file holding every canonical block. A payload, not a specification. |
| **Bundle version** | The bundle's semver, stamped into every deployed file's frontmatter. Governed by `DeployedSectionsBundle.md`. |
| **Contract** | Text defining something two parties must agree on. There is one: the orchestration contract in `CommunicationProtocol.md`. It carries its own version and is not in the bundle. |
| **Specifying document** | The design document holding a block's rationale and changelog, named in the block's `specified_in`. Holds no copy of the block. |
| **Canonical order** | The fixed sequence of seven top-level boundaries every agent file follows (§2.3). |
| **Role** | `subagent`, `orchestrator`, `utility`, or `standalone`. Declared in frontmatter. `subagent` and `orchestrator` are schema-governed and select canonical text; `utility` and `standalone` receive harness transformation only (§5A). |
| **Default content** | Non-empty content in a `type="project"` region in a source file. Deployed once on initial deploy; thereafter owned by the project and never overwritten (§2.1.1). |
| **Drift** | Divergence between copies of text that was once identical and has no single source. |

---

## 13. Change Propagation

A change to this schema is never local. The table below lists what must be checked or updated when each category of change is made. Omitting a downstream update leaves the system in disagreement about what is valid — and the disagreement is silent until a deployment fails or a validator flags it.

| Change | Must update |
|--------|------------|
| Canonical section order (§2.3) | `Catalog/SourceFilesFormat.md`, `vocabulary.go` (`CanonicalOrder`), `boundary_constants.py`, all source agent files, test fixtures. Deployed agents with custom regions may require repositioning after parking (§6.4). |
| Injection name added (§6.2) | Source agent files carrying the injection, `Catalog/SourceFilesFormat.md` injection catalogue. Adding to an already-deployed agent risks a Shape B collision if a project uses the same name as a custom region — see §6.2.2. |
| Injection name renamed (§6.2) | Rename table entry (§6.2.2, §10.1), source agent files, `Catalog/SourceFilesFormat.md` injection catalogue. Without the table entry, project content is stranded. |
| Injection name removed (§6.2) | Source agent files. Project content is orphaned to `TODO.md` on the first update — §6.2.2 states the discipline that makes removal rare. |
| Managed region name added or removed (§2.5) | `vocabulary.go` (`CanonicalDeployed`, `DeployedParent`), `boundary_constants.py`, classifier, role matrix (§2.4), `Catalog/SourceFilesFormat.md`. If bundle-sourced, the bundle gains a block and `DeployedSectionsBundle.md` is updated. |
| Bundle block content changed | `bundle_version` bump, specifying document changelog, all deployed agents (redeploy). If the changed region has nested user regions, the deploy tool should emit a TODO advising the project to review their content against the new text. |
| Communication Protocol content changed | Protocol version bump in `CommunicationProtocol.md`. Projects with `<ProtocolExtension type="custom">` must review their content against the new protocol. |
| Frontmatter field added or renamed (§3) | `SourceFilesFormat.md`, harness descriptor `frontmatter_plan` (filter/add/remove lists), `agentfields` registry, all source agent files. |
| Validator rule severity changed (§9) | `validate.go`. A promotion from warning to error may cause previously passing project agents to fail — this is intentional and motivated. |
| Generic tool name changed in source `tools` list | Harness module tool mappings (internal). Projects with custom `tool_destinations` must update their configuration. |
| Role matrix changed (§2.4) | Bundle block `applies_to` fields, classifier, validator, source agent files for the affected role. |
| Skill folder renamed (`Catalog/Skills/`) | Source agent `required_skills` fields, agent body text referencing the skill path. |

---

## 14. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 2.1 | 2026-08-15 | **`IdentityExtension` and `ErrorHandlingExtension` removed from catalogue and all agents.** Neither passed the inclusion test: `IdentityExtension` (47→0 agents) offered ambient "domain expertise" no instruction consumed; `ErrorHandlingExtension` (46→0) offered "project recovery guidance" no project would know how to fill. §6.5's inclusion guidance rewritten from the vague "could use" to a two-part test: the region must be generally applicable (most projects have something to put there) AND improve agent performance when provided. §4.1 and §4.5 project-injection lines removed. §6.2 catalogue drops from seven names to five. `SourceFilesFormat.md` updated. Analysis in `SubagentProjectRegions-Review.md` §4.5 and §4.9. |
| 2.0 | 2026-08-15 | **`OutputFormat` section removed.** The section carried `status_message` examples and `error_code` choices per agent — two things the Communication Protocol's deployed region and `ErrorHandling` (§4.5) already cover between them. The examples were actively causing verbose message parroting rather than helping. Canonical order drops from seven slots to six; §2.3, §2.4, §5.1 updated. §4.6 deleted, §4.7 renumbered to §4.6. Conformance rules 17 and 17r retired; rule 16 simplified. Migration step 7 marked superseded. §15's rename item moved to Rejected. All forty-seven subagent files already migrated — no `OutputFormat` region remains in the catalog. Analysis in `SubagentProjectRegions-Review.md` §4.10. |
| 1.9 | 2026-08-15 | **§6.3 stops specifying the severity default table.** The document held a concrete four-row threshold table as *the* default, while the nine validation agent sources shipped a different one (MAJOR requiring rework, changed on operational experience). A per-agent value that is expected to vary and to be retuned does not belong in the schema; stating it twice guaranteed one copy would be wrong, and it was. §6.3 now places the region, fixes the status-code rule the table feeds, fixes CRITICAL as non-configurable, and points at the source files for the text. §16's severity open item updated: the validation agents are done, the audit agents' core-text severity tables are the remaining gap. |
| 1.8 | 2026-08-14 | **Utility and standalone roles added; project regions may carry default content.** Two new `role` values — `utility` and `standalone` — recognised by the deployment tool for harness transformation but outside this schema's structure rules and deployment bundle. New §5A covers both. `role` vocabulary in §3.2 expanded from two values to four, with a table stating what each receives. Project regions may now carry default content in source files (§2.1.1): non-empty content in a source `type="project"` region deploys on first deploy and is thereafter the project's property, never overwritten on update. §6.3 updated: the severity table belongs inside the `SeverityThresholds` region as default content, not after it. §6.5 guidance, §11, §12 glossary updated accordingly. |
| 1.7 | 2026-08-14 | **Tag syntax migrated from `[[KIND:Name]]` to `<Name type="value">`.** The four region kinds (SECTION, DEPLOYED, INJECTION, CUSTOM) now use name-first XML tags with a `type` attribute carrying the ownership value (`core`, `managed`, `project`, `custom`). Closing tags are standard XML `</Name>` with no attributes. Empty regions use an open/close pair (no self-closing). Compound/enumerable names carry the id in a `name` attribute: `<Workflow type="core" name="quick-fix">` closes with `</Workflow>`. Region versions move from HTML comments inside region content to a `version` attribute on the opening tag. Tag-line matching rule updated: a tag is a MOSAIC boundary only when its trimmed line is exactly the tag and it carries a valid `type` attribute — foreign XML tags without `type` are inert content. All examples, tables, rule descriptions, glossary entries, and migration steps updated to the new syntax. The four kinds, their semantics, the canonical order, the role matrix, and the nine tool-managed names are unchanged in substance. |
| 1.6 | 2026-08-12 | **`ProtocolExtension` removed from the injection catalogue.** The name is no longer an entry in `InjectionParent`; projects needing to extend the protocol use `<ProtocolExtension type="custom">` instead of a declared project injection. The name remains legal as an open injection name and is preserved byte-identically if used, but carries no advisory parent. This applies the project vs. custom provenance rule consistently: MOSAIC defines project slots in source; projects invent custom regions in deployed files. §4.2, §6.2, and §6.2.1 updated; §10.1 and §13 reconciled; `vocabulary.go` and `SourceFilesFormat.md` updated. Prior discussion (the v1.1 removal and v1.2 reinstatement) is preserved in §15. |
| 1.5 | 2026-08-11 | **Custom region kind introduced; source-driven invariant stated; nesting inside managed regions permitted.** New fourth region kind `type="custom"` for project-invented regions that exist only in deployed files, distinct from `type="project"` which is declared in source. On schema reorder, source injections follow the source automatically; custom regions are parked at end of file with a TODO. Principle 10 added to §1.3 stating the source-driven structure invariant that makes reordering safe. `LanguagePatterns` removed from the injection catalogue — it is not declared in any source file and is a custom region if a project wants it. Rule 13 reversed: project and custom regions may now nest inside managed regions — the tool preserves them on regeneration. §6 restructured: §6.1 distinguishes the two kinds, §6.2 is the injection catalogue, §6.4 specifies custom region behavior on reorder, §6.5 is rules. Analysis in `SchemaEvolution-Problems.md`. |
| 1.4 | 2026-08-08 | **`LanguagePatterns` and `CustomConstraints` removed from the managed vocabulary.** Both were listed in §2.5 with the content source *"Deployment configuration"* — a category this document named and never specified, and that no tool ever implemented. Lacking a generator, both fell through the classifier's permissive `default:` branch to the harness class, which meant every harness had to declare them empty solely to satisfy §2.4's content-source check; the rationale text in those files records an author noting the classification was wrong and complying anyway. `CanonicalDeployed` drops from eleven names to nine and the phantom source disappears, leaving the three §2.5 actually describes. `LanguagePatterns` becomes a catalogued injection name (§6.1), deliberately not declared in MOSAIC's own sources. `CustomConstraints` is deleted outright — it had no definition, no specification, and served as an escape hatch for content that belonged in `HarnessConstraints`. New §2.5.1 makes the underlying rule explicit: a managed name must name an implemented generator, and an unclassified name is an error rather than a default. Role matrix, tier table, §4.3, §4.4 and §10.1 follow. |
| 1.3 | 2026-08-05 | **`OutputFormat` corrected to two varying fields.** v1.0 reduced the section to `status_message` on the premise that nothing else in the response envelope varied between agents. A survey of the forty-two sources disproves it: `error_code` varies too — most agents return `E101`, but `test-runner` returns `E501`, `pull-request-comment-interface` `E502`, `requirements-refinement` `E503`, and `audit-to-pull-request` `E401`. The contract region supplies the code vocabulary and can never supply an agent's choice from it, so §4.6's table gains an `error_code` column and permits multiple `BLOCKED` rows. Migration step 7 amended accordingly — as written it would have discarded every one of those choices in its least verifiable step. Rule 17r extended to review the code choice alongside the message. The envelope deletion itself is unchanged and rule 17 is untouched: the widened table contains no JSON fence. |
| 1.0 | 2026-08-05 | **Initial specification.** Established the agent file schema: frontmatter fields including a declared `role`, the three region kinds and their ownership semantics, canonical document order, a per-role region matrix, and a section-by-section content specification. Settled the canonical-fragment question: five fragments become managed regions, one is deleted. Rewrote `OutputFormat` as a `status_message` table with no JSON envelope. Corrected three live defects structurally. |
| 1.2 | 2026-08-05 | **Conformance made proportionate.** §9 rewritten: every rule now carries a severity (error / warning / advice) and an enforcement mechanism (tool / review / guidance), and a strict/lenient mode holds MOSAIC's own sources to all of it while a project's agents are blocked only by what would otherwise break. Injection names opened — no allowlist, all injections preserved whatever they are called, none ever required to be filled; the catalogue is a suggestion. `ProtocolExtension` un-banned and specified as a top-level sibling of the contract region, since extending protocol *mechanics* is a legitimate deployment need. Canonical order relaxed from an equality check to a subsequence: absent sections and additional ones are both fine, relative order is not. Managed-region absence graded into contract / conduct / deployment tiers. Two principles added: a check that flags legitimate work gets switched off, and not every rule is a tool's to enforce. |
| 1.1 | 2026-08-05 | **Split into schema only.** Bundle mechanics moved to `DeployedSectionsBundle.md`; per-block reasoning moved to five documents under `DeploymentBlocks/`; the drift measurement and its Option A/B/C decision moved to `Development/Analysis/AgentBodyDrift.md`. Applied the settled decisions that post-dated v1.0: the bundle holds **no contracts**, so `CommunicationProtocol` deploys from its own source; the artifact provenance stamp folds into the orchestration contract, removing canonical slot 3, its role-matrix row, and its section spec, and taking the canonical order from eight slots to seven; the `bundle_version` stamp moved from a region-body comment to deployed-file frontmatter. `ArtifactProvenanceExtension` removed from the injection catalogue: with the stamp inside the contract, it dies by the argument that killed `ProtocolExtension`. |

---

## 15. Open Ideas / Dead Ends

**Under consideration**

- **Retiring `id`.** No consumer was found — matching is by `name` throughout the inspected tooling (§3.3). Retiring it is a forty-two-file change and a round-trip contract change for no functional gain, so it stays. Worth reopening if a registry or an external reference ever needs a rename-stable key, and worth closing outright if a survey confirms nothing reads it.

**Rejected**

- **A required structure for `Capabilities`.** It is the one section with no specified shape (§4.3), which is right for expertise that genuinely varies. Considered requiring `### Core Capabilities` as a minimum; decided against. The template already shows it, every agent follows it, and promoting a convention to a rule buys nothing. If content validation lands it would be a warning at most, alongside rule 15.
- **A closed vocabulary of injection names.** Held until v1.2. Its stated purpose was to stop a user's content being orphaned on update — but preservation matches names between the deployed file and the source file, so a name the author wrote into their own source is preserved with or without a list. The list bought nothing and cost an author the ability to name a region after their own project's concept (§6.2). Superseded in v1.5 by a different solution: project-invented regions use `type="custom"` instead of `type="project"`, making the distinction visible in the file. Managed names are the opposite case and stay closed: there the tool must find *content* for the name.
- **`ProtocolExtension` as a catalogued injection.** Added to the catalogue in v1.2: a deployment with real transport needs should not be forced to fork the contract. Removed again in v1.6: project injections are declared in MOSAIC source files; protocol extension is a project invention that belongs in `<ProtocolExtension type="custom">`. The change does not forbid the use case — it routes it correctly. Projects that already use a project-typed `ProtocolExtension` find their content preserved as an open, uncatalogued injection name; new ones use `type="custom"`. The v1.1 removal for a different reason (the stamp folded into the contract and `ArtifactProvenanceExtension` died by the same argument) and the v1.2 reinstatement are both recorded in the changelog; this is the third entry in that sequence.
- **`IdentityExtension` as a catalogued injection.** Present in all 47 agents from the start. Removed in v2.1: "project or domain expertise" is too abstract for most projects to act on, no agent instruction references it, and its effect on agent behavior is negligible. Fails both parts of the inclusion test (§6.5). A project with a genuine domain-expertise need uses `<DomainExpertise type="custom">` in their deployed file.
- **`ErrorHandlingExtension` as a catalogued injection.** Present in 46 agents from the start. Removed in v2.1: "project-specific recovery guidance" is unclear enough that no project would know what to write, and no agent instruction references it. A project with recovery needs specific enough to state uses a custom region.
- **One uniform strictness for every tree.** Would mean either blocking legitimate project agents or letting MOSAIC's own sources off the rules they exist to demonstrate. The strict/lenient split (§9.2) is what lets the recipe be enforced on the recipe without being enforced on its readers.
- **Requiring every injection in a file to be filled.** Empty is the normal state of a source file and of a fresh deployment (principle 3), and where a section offers alternative child injections the rule would demand contradictory content (§6.5).
- **A `function` or `category` frontmatter field.** The folder already states it, and a second copy is undetectably wrong when the two disagree (§3.5).
- **`agent_class` in agent frontmatter.** Would create a second declaration of a fact the orchestrator's declaration region owns and the executor actually reads (§3.5).
- **Single-sourcing everything repeated, or deleting every contract echo.** Both pure options were rejected in favour of a per-fragment split. Reasoning in `Development/Analysis/AgentBodyDrift.md` §4.
- **Orchestrator variants of the five canonical blocks.** Four would share no sentence with their subagent counterparts, maintained in a file the orchestrator's author does not otherwise open, to protect a single copy from diverging from itself. `AuthorityHierarchy` is the close call — it genuinely is a variant — and was rejected on cost, leaving a review obligation instead (§8).
- **Keeping worked JSON response examples per agent.** Outside `status_message` and `error_code` they were identical, and thirty-five of forty-two had gone stale against the contract they shipped beside. Both varying fields survived as columns of the `OutputFormat` table until v2.0 removed the section entirely.
- **Reducing `OutputFormat` to `status_message` alone.** Held until v1.3, on the premise that it was the only field varying between agents. It was not: `error_code` varies too. Moot since v2.0 removed the section.
- **Renaming `OutputFormat` to `StatusReporting`.** Under consideration from v1.3 through v1.9: the section no longer specified a format, just `status_message` examples and `error_code` choices. Deferred because the rename cost the canonical order, three vocabulary files, and forty-seven files for clarity alone. Mooted in v2.0 by removing the section entirely — the rename's cost was the strongest argument for keeping a section whose value was already doubtful.
- **Keeping `OutputFormat` as a section.** Removed in v2.0. The Communication Protocol deployed region already carries the full JSON envelope, status code table, error code table, and key rules 11–16 mapping when to use each status. `ErrorHandling` (§4.5) already carries the agent-specific status mapping. `OutputFormat` added only two things: example `status_message` strings and the agent's `error_code` choice. The examples were actively harmful — agents parroted the template phrasing instead of describing their actual outcome, producing unnecessarily verbose messages. The error code choice is a single line that fits naturally into `ErrorHandling`'s existing per-agent bullets. Surveyed in `SubagentProjectRegions-Review.md` §4.10.
- **A managed region for the Process list itself.** Every Process list is agent-specific; only its closing steps were shared. Deploying the whole list would have meant no list at all.
- **"Deployment configuration" as a content source.** Held until v1.4 as the source for `LanguagePatterns` and `CustomConstraints`. It described no mechanism, had no specification behind it, and was never implemented — the `Specified in` column carried an em-dash for both rows, which is this document admitting in its own table that nobody had written the thing down. A source that exists only as a phrase in a table is worse than an acknowledged gap: the closed-set rule forced every harness to satisfy it with empty declarations, so the fiction propagated into four harness files and every deployed agent. §2.5.1 now requires a named, implemented generator.
- **A per-agent `LanguagePatterns` region in MOSAIC's own catalog sources.** Considered when reclassifying it as an injection in v1.4, on the precedent of `CodebaseContext`, which is declared empty in thirty-seven sources. Rejected: `CodebaseContext` describes a codebase every project has, while language patterns are meaningful only to a project that has settled on a language and wants its agents constrained by it. Declaring it everywhere would ship fifty-odd empty regions and `TODO.md` lines to make one project's case marginally easier. As of v1.5, `LanguagePatterns` is a custom region if a project wants it.
- **A separate `ArtifactProvenance` canonical slot.** Held slot 3 in v1.0. Removed: the stamp is verified by the orchestrator, which makes it hard interop rather than an audit convenience, and a contract with two version numbers in two regions is a contract that can disagree with itself.

---

## 16. Open Items

- **Most of §9 is unimplemented, and the implemented part is now wrong in three places.** `docformat/validate.go` checks boundary structure only — no frontmatter conformance, no section content, no bundle comparison. On top of that backlog, three live checks no longer match this document: `unknown-injection` must go entirely (§6.2), `out-of-order-section` must become a subsequence test rather than rejecting unknown top-level names (§2.3), and project-injection parent placement must drop from error to advice (rule 9b). The three subtractions are the fastest way to stop the validator flagging legitimate work.
- **Severity and mode are specified but the validator has one of each.** Everything it reports is an error, and it behaves identically on MOSAIC's tree and a user's. §9.1's two axes and §9.2's strict/lenient split both need building, along with routing warnings into `TODO.md` and the deployment summary rather than only to a console.
- **No review-mechanism rules run.** Rules 15r–18r are specified with `Review` as their mechanism and nothing performs them. The intended vehicle is a validation subagent given them as a checklist; that agent does not exist.
- **The provenance merge is specified but not executed.** The design layer is done — `CommunicationProtocol.md` v1.10 owns the stamp, and `ArtifactProvenance.md` is a tombstone. What remains is mechanical: forty-two agent files still carry older provenance regions and injections, and the three vocabulary copies plus the `Tools/Common/testdata/boundary/` fixtures still encode eight-slot ordering. Until that lands, deployed agents do not match §2.3.
- **The HITL obligation is stated twice** in every subagent file: once in the contract's managed region, once in `ClosingProcedure`. Both single-sourced, so they cannot drift accidentally. Options and the argument are in `DeploymentBlocks/ClosingProcedure.md` §7.
- **The deployment tool does not read the bundle at all.** Until it does, the five blocks are deployed nowhere and the migration in §10 cannot complete. Tracked in `DeployedSectionsBundle.md` §10.
- **The tool's role enum is partially aligned.** `domain.AgentRole` now has `subagent` / `orchestrator` / `utility`. The new `standalone` value (§3.2) is not yet represented in the code. `ParseAgentRole` accepts only `subagent` and `orchestrator`; it needs to accept `utility` and `standalone` as well.
- **Role is still inferred from path in the tool.** The frontmatter field is specified here but nothing reads it. Until it does, `role` is documentation, and a file moved between folders still changes what it is.
- **`Catalog/SourceFilesFormat.md` and this document overlap.** That file states the agent format from the tool's side, with no rationale, and has already drifted. It should be reduced to a pointer plus the skill and hook conventions it uniquely covers (§10.1).
- **`InfrastructureAgentConcept.md` §3.1 shows an old project-injection marker for `InfrastructureAgents`.** The vocabulary has it as a managed region, and the orchestrator source uses the managed form. The design document is the one that is wrong, and it is the document a reader would trust. `Catalog/Subagents/Infrastructure/README.md` line 7 carries the same error and should be corrected with it.
- **The orchestrator's `ErrorHandling` section extends far past its injection.** The project injection closes around line 493 and the section continues to line 715 with the Core Orchestration Loop, which is neither error handling nor an extension of it. The current arrangement means a project injection lands in the middle of unrelated material.
- **Severity default content is correct in the validation agents and absent everywhere else.** The nine validation agents carrying `SeverityThresholds` now hold the table and status-code logic inside the region, as §6.3 requires. The audit agents are the remaining gap: each states a "Severity Levels" definition table as core text with no `SeverityDefinitions` region beside it, so a project cannot restate what a severity means to them without editing core prose the next deploy overwrites. Two validation agents (`build-review`, `verification-answer-validator`) carry neither severity region, and `test-scenario-review` carries thresholds without definitions; whether those are deliberate is unexamined. Surveyed in `SubagentProjectRegions-Review.md`.
- **The `MosaicTest` agents were not surveyed.** Three agents under `MosaicTestCatalog/Agents/MosaicTest/` exist to exercise the harness rather than to do work. Whether they conform to this schema, and whether they should, is unexamined.
