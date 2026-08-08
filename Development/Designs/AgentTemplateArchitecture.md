---
id: agent-template-architecture
type: specification
version: "1.4"
name: "Agent Template Architecture"
description: "The structure of a MOSAIC agent file: frontmatter schema, the three region kinds and their ownership, canonical document order, the per-role region matrix, and what each section must contain."
author: MOSAIC
status: Draft
---

## 1. Overview

### 1.1 Purpose

An agent file is a system prompt with a schema. This document is that schema: what an agent file contains, who owns each part of it, and in what order the parts appear. It is the document to read when fixing an agent, and the document to follow when writing a new one.

**Covered here:**

- The three region kinds and what each one's marker promises (§2.1)
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
| All canonical text itself | `Agents/Generic/DeployedSections.md`, and nowhere else (§7) |
| The measurement and decision that produced the blocks | `Development/Analysis/AgentBodyDrift.md` |
| Infrastructure agent declaration, trigger vocabulary, `Class` | `InfrastructureAgentConcept.md`. An infrastructure agent is an ordinary subagent by this schema; what makes it infrastructure is how the orchestrator reaches it, not how its file is shaped. |
| Utility agents | Nothing. They carry no boundary tags, are never deployed into a run, and are outside this schema entirely. |
| Workflow definitions and the orchestration artifact's schema | Their own documents. Both are consumed by an agent this document shapes; neither is shaped by it. |
| Skill and hook-bundle file formats | `SourceFilesFormat.md`. Separate artefact kinds with their own conventions. |

### 1.2 Why the Format Looks Like This

Agent files were originally written by copying a complete template and filling in the blanks, with marked injection points for the parts a project would customise. Everything else was prose an author pasted and then maintained by hand.

That arrangement produced exactly the outcome it was always going to. Forty-two subagent files each held their own copy of the shared body text, four of those fragments had measurably diverged, and three defects rode along with the divergence. None of it was noticed until someone counted. The measurement is `Development/Analysis/AgentBodyDrift.md`.

Three things are different in the format specified here, and together they are why this document specifies a schema rather than supplying a template.

**Ownership is declared in the file.** A region's marker says who writes it: `[[SECTION:]]` is MOSAIC-authored and carried verbatim, `[[DEPLOYED:]]` is written by the tool and regenerated every deploy, `[[INJECTION:]]` is the project's and preserved byte-identically. A reader knows which regions are theirs without consulting anything.

**Content has moved out of the template into contracts.** The orchestration protocol left in v1.9, and the provenance stamp folded into it. It is now one file deploying into one named region. The template's job is no longer to hold that text but to place the slot it goes into.

**Shared body text follows it.** The same treatment extends to the five fragments that remained hand-copied. After the migration in §10, the only prose in a subagent file that is not single-sourced is prose about *that agent*.

### 1.3 Design Principles

| # | Principle | What it buys |
|---|---|---|
| 1 | **Text repeated across agents is deployed, never copied** | Forty-two copies of a sentence drift; one source cannot. Every fragment identical across agents is either single-sourced or deleted. |
| 2 | **The marker states the owner** | `[[SECTION:]]`, `[[DEPLOYED:]]`, `[[INJECTION:]]` answer "may I edit this?" from the file itself. No lookup into code or docs. |
| 3 | **A source file is a valid document on its own** | Empty deployed regions and empty injections are the normal state of a source file, not a degraded one. Nothing is required to be filled for the file to be well-formed. |
| 4 | **One fact, one authority** | The contract states message shape and the stamp; this document states structure. Where they touch, they reference rather than restate — a second copy is always the one that goes stale. |
| 5 | **Structure is mechanically checkable** | Every rule here that can be a validator rule is written to be one (§9). A convention no tool can check is a convention that erodes. |
| 6 | **Agent-specific text earns its place by being agent-specific** | A section survives in an agent file because its content differs per agent. Content that does not differ belongs in the bundle; content already stated by an adjacent deployed region belongs nowhere. |
| 7 | **The nearest instruction wins in practice** | A model follows the specific, role-adjacent instruction over the general one three sections up. So the nearby instruction must be correct, not merely present — this is why the HITL defect mattered far more than its count suggested. |
| 8 | **A check that flags legitimate work gets switched off** | Principle 5's counterweight. A validator that errors on an unusual but working agent teaches its user to ignore it, and an ignored validator enforces nothing. Severity is therefore part of every rule: a rule errors only when violating it breaks the tool, breaks interop, or destroys the user's own content. Everything else warns or advises (§9). |
| 9 | **Not every rule is a tool's to enforce** | A specification states more than a regex can check, and that is not a defect. Each rule names its enforcement mechanism — tool, review, or guidance — so that advice is never mistaken for a broken check, and so that authors are not pushed into writing only what a validator can read (§9). |

---

## 2. File Anatomy

An agent file has two parts: a YAML frontmatter block (§3) and a body composed of boundary-delimited regions.

### 2.1 The Three Region Kinds

| Marker | Written by | On deploy | On update |
|--------|-----------|-----------|-----------|
| `[[SECTION:Name]]` … `[[/SECTION:Name]]` | MOSAIC source authors | Carried from source byte-identically | Carried from source byte-identically |
| `[[DEPLOYED:Name]]` … `[[/DEPLOYED:Name]]` | The deployment tool | Body generated from a canonical source | Regenerated wholesale; prior content discarded |
| `[[INJECTION:Name]]` … `[[/INJECTION:Name]]` | Project authors | Left empty, listed in `TODO.md` | Preserved byte-identically |

Three consequences follow and are worth stating outright:

**Never author content inside a `[[DEPLOYED:]]` region.** It is discarded on the next update without warning. In a source file these regions are always empty; the deployed file is the only place they have content.

**A user-owned injection is never nested inside a `[[DEPLOYED:]]` region.** The parent is regenerated wholesale, which would destroy the child. Where an injection accompanies a deployed region, it is a *sibling* at the same level.

**Region replacement precedes injection resolution.** Reversed, the tool would resolve an injection and then overwrite the region beside it, discarding content it had just placed.

### 2.2 Syntax and Nesting

- A tag line matches when the trimmed line is exactly `[[` + marker body + `]]`. Nothing else on the line.
- Each boundary name appears at most once per file.
- `[[SECTION:]]` regions appear at body top level, except for the orchestrator's template blocks (§5.4).
- `[[DEPLOYED:]]` and `[[INJECTION:]]` regions appear either nested inside a section or at body top level, according to the parent named in §2.5 and §6.1. An empty required parent (`""`) means top level.
- A compound name of the form `[[SECTION:Prefix:id]]` marks an enumerable item — workflows, infrastructure agent declarations, and the canonical blocks in the bundle all use this form, so a tool can enumerate by prefix without knowing the members ahead of time.

### 2.3 Canonical Document Order

A file's top-level boundaries appear in this order. Every entry is a `[[SECTION:]]` except slot 2, which is a top-level `[[DEPLOYED:]]` region.

| # | Name | Kind | Required for |
|---|------|------|--------------|
| 1 | `Identity` | section | subagent, orchestrator |
| 2 | `CommunicationProtocol` | deployed, top level | subagent, orchestrator |
| 3 | `Capabilities` | section | subagent, orchestrator |
| 4 | `Constraints` | section | subagent, orchestrator |
| 5 | `ErrorHandling` | section | subagent, orchestrator |
| 6 | `OutputFormat` | section | subagent |
| 7 | `ExecutionPhilosophy` | section | subagent, orchestrator |

Seven slots. There is no separate provenance slot: the artifact provenance stamp is part of the orchestration contract and ships inside slot 2. §15 records the transitional state, since the files have not been migrated yet.

**The rule is "not out of order", not "exactly these seven".** A file's top-level boundaries must form a *subsequence* of the list above. Concretely:

- A section may be **absent**. Whether that absence matters is §2.4's question, not this one.
- A file may carry **additional** top-level sections of its own, under any name. They are skipped by this check.
- Any two canonical sections that are both present must appear in the listed relative order. `Constraints` before `Capabilities` is a violation whether or not anything else is missing.

**Why order is fixed but membership is not.** A model attends unevenly across a long prompt, so the position of an instruction is part of the instruction — identity first, then the contract it operates under, then what it can do, then what it must not, then how to fail, then how to report. That argument justifies the sequence. It does not justify demanding all seven: an agent that genuinely has nothing to say under a heading is better off without it than padding one, and an author with a real need for an eighth section is not writing a broken file. The check stays a single walk over two lists either way.

### 2.4 Role Matrix

| Region | Subagent | Orchestrator |
|--------|----------|--------------|
| `Identity` section | Required | Required |
| `[[DEPLOYED:ClosingProcedure]]` | Required | Absent (§8) |
| `[[DEPLOYED:AuthorityHierarchy]]` | Required | Absent (§8) |
| `[[DEPLOYED:AvailableWorkflows]]` | Absent | Required |
| `[[DEPLOYED:InfrastructureAgents]]` | Absent | Required |
| `[[DEPLOYED:CommunicationProtocol]]` | Required | Required |
| `Capabilities` section | Required | Required |
| `Constraints` section | Required | Required |
| `[[DEPLOYED:ProtocolConstraints]]` | Required | Absent (§8) |
| `[[DEPLOYED:HarnessConstraints]]` | Required | Required |
| `ErrorHandling` section | Required | Required |
| `[[DEPLOYED:ErrorHandlingCommon]]` | Required | Absent (§8) |
| `OutputFormat` section | Required | Absent |
| `ExecutionPhilosophy` section | Required | Required |
| `[[DEPLOYED:ExecutionPhilosophyCommon]]` | Required | Absent (§8) |

"Required" above means *required by this schema for a well-formed MOSAIC agent*. It does not mean every absence is an error. What an absence costs varies enormously between regions, and §2.4.1 grades it.

A region present with no canonical block matching the file's role **is** an error in every case — the file has asked for text the tool has none to give, and deploying it empty would ship an agent that appears complete and instructs nothing.

#### 2.4.1 What an Absent Deployed Region Costs

| Tier | Regions | Absence is | Because |
|------|---------|-----------|---------|
| **Contract** | `CommunicationProtocol` | **Error** | The agent receives no message shape, no status vocabulary, no HITL gate. It cannot produce a response the orchestrator can parse. This is interop, not style. |
| **Conduct** | `ClosingProcedure`, `AuthorityHierarchy`, `ProtocolConstraints`, `ErrorHandlingCommon`, `ExecutionPhilosophyCommon` | **Warning** | The agent still speaks the contract and still works. It works *worse*: no artifact-access imperatives, no retry rule, no ranking when the harness contradicts MOSAIC. Degradation an author should be told about and may knowingly accept. |
| **Deployment** | `HarnessConstraints`, `AvailableWorkflows`, `InfrastructureAgents` | **Silent** | Content comes from a deployment's own selections. Most deployments select nothing for most of them, so absence is the ordinary case and flagging it is noise. |

The tiers are about the *agent's* absence, not the block's. A bundle block with no matching region in a given agent remains normal (`DeployedSectionsBundle.md` §4).

### 2.5 Tool-Managed Names

Every `[[DEPLOYED:]]` name, its required parent, and where its content comes from.

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

- **Bundle** — `Agents/Generic/DeployedSections.md`, the five blocks whose text is identical across every agent of a role and which are not contracts. Governed by `DeployedSectionsBundle.md`.
- **Contract source** — `CommunicationProtocol.md` ships its own text. It is a contract, so it is deliberately not in the bundle and carries its own version.
- **Assembled** — no canonical text exists; the content is built from a deployment's own selections. `AvailableWorkflows` and `InfrastructureAgents` are assembled from what the deployment selected; `HarnessConstraints` comes from the one harness module in play.

#### 2.5.1 Every Deployed Name Names Its Generator

**A name belongs in this table only if an implemented generator fills it.** The `Content comes from` column is not a description of intent — it is the requirement, and a name whose generator does not exist does not belong in `CanonicalDeployed`.

This rule exists because its absence caused a real defect. v1.3 and earlier listed `LanguagePatterns` and `CustomConstraints` with the source *"Deployment configuration"* and an em-dash under `Specified in`. No such mechanism was ever built, and no document specified one. The implementation had to route them somewhere, so they fell through a permissive `default:` branch and were classified as harness-sourced — content wiped and refilled from the harness descriptor on every deploy. Nothing in the harness layer had any business supplying either one.

The rest followed mechanically. §2.4 required present deployed regions to have a content source, so all four harnesses had to declare `[[DEPLOYED:LanguagePatterns]]` empty purely to satisfy the check. The rationale written into those files records the contradiction in plain words — *"no harness-level content is appropriate here regardless of harness. Declared empty to satisfy the canonical injection list."* An author documented that the classification was wrong and satisfied the validator instead of fixing it, because this document gave them no name for what was wrong.

Two consequences are binding:

- **An unrecognised or unclassified deployed name is an error, never a fallback.** A classifier that assigns a default class to a name nobody classified deliberately will assign the wrong one silently, and the closed-set check will then propagate it into every harness and every agent. There is no safe default here; the safe behaviour is to refuse.
- **A name with nothing to fill it is not a deployed name.** If content is authored per project, it is an injection (§6.1). If it is authored per agent, it is body text. Neither is a `[[DEPLOYED:]]` region, and giving one a region that no generator fills produces a permanently empty block that every agent carries and no tool can populate.

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
| `recommended_tier` | string | Yes | deployment | The capability tier this agent wants. Open string — no fixed vocabulary. Presented to the user during model selection and used as the key for tier-to-model mapping. Values in current source: `LOW`, `LOW-MEDIUM`, `MEDIUM`, `MEDIUM-HIGH`, `HIGH`. |
| `tier_rationale` | string | Yes | deployment | Why that tier. Shown to the user alongside it, so a person choosing a model has the reasoning and not just the label. |
| `required_skills` | flow list | Yes | deployment | Skill keys this agent's instructions explicitly tell it to load. `[]` when it loads none. Values are folder names under `Agents/Generic/Skills/`. Never inferred from context — a skill appears here only because a Process step names it. |

A deployed file additionally carries `bundle_version`, written by the tool. It is not a source field; see `DeployedSectionsBundle.md` §3.2.

Key order in source is `id`, `version`, `name`, `description`, `role`, then the deployment group. The tool respects source order for keys it does not rewrite; the order matters for round-trip fidelity and for a human scanning a directory of files, not for parsing.

### 3.2 `role`

`role` states what the agent is, in the same vocabulary the canonical blocks use in their `applies_to` fields: `subagent` or `orchestrator`.

**Why it is declared rather than inferred.** Every block the tool selects is keyed on role. Today the tool derives role from the file's path — `Agents/Generic/Orchestrator/` means orchestrator, everything else means not. That works exactly as long as the layout does, which makes a reorganisation of the agent folders a silent change to which contract text ships. Moving a file should not be able to change what an agent is.

There is a second reason, smaller but sharper: a role in frontmatter is checkable against the file's own regions. A file declaring `role: subagent` with no `OutputFormat` section is detectably wrong (§9). With role inferred from path, the same file is merely unusual.

**On the vocabulary.** The deployment tool's internal role enum currently reads `worker` / `orchestrator` / `utility`, while every design document says `subagent`. The frontmatter field uses `subagent`, because that is the word the contract and this document both use, and because `worker` names nothing else in the system. Reconciling the code enum is recorded in §15.

`utility` is not a value here. Utility agents carry no boundary tags, are never deployed into a run, and are outside this schema entirely (§1.1).

Infrastructure agents declare `role: subagent`. They are ordinary subagents by file shape; their class and triggers are declared in the orchestrator, not in their own frontmatter.

### 3.3 `id`

A flat integer, assigned when an agent is first created, and thereafter immutable and unique across the agent registry. It survives renames and folder moves, which is what makes it a stable join key between a generic agent and anything that refers back to it.

The orchestrator carries no `id`: there is one of it, it is referenced by role rather than by number, and a sequence number for a set of one informs nobody.

The honest position is that no current consumer has been found for this field — matching is done by `name` everywhere the tooling was inspected. It is retained rather than retired because retiring it is a change to forty-two files and a round-trip contract for no functional gain, and because a stable identifier that survives a rename is cheap to keep and expensive to reintroduce. §14 records the question rather than closing it.

### 3.4 Version Bump Rules

Bump `version` when the agent's hand-authored content changes in a way that alters the guidance it receives.

- **Patch** (`x.y.Z`) — wording, clarification, a corrected example.
- **Minor** (`x.Y.0`) — a new capability, constraint, or process step; a new injection or deployed region added to the file.
- **Major** (`X.0.0`) — a change to the agent's scope, goal, or litmus test; anything that changes what the agent is for.

**Deployed region content does not bump the agent version.** When a canonical block changes, forty-two agents receive new text and none of them change version — their own source did not change, and a bump would claim an authorship that did not happen. The bundle's own version records that change, and staleness is detected against it.

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

**Deployed regions, in order after the Process list:**

| Region | Block |
|--------|-------|
| `[[DEPLOYED:ClosingProcedure]]` | `ClosingProcedure:Subagent` |
| `[[DEPLOYED:AuthorityHierarchy]]` | `AuthorityHierarchy:Subagent` |

The order is not arbitrary: the closing procedure continues the Process list and must read as its final steps, so nothing goes between them.

**Injection:** `[[INJECTION:IdentityExtension]]`, last in the section — project-specific domain or language expertise.

**Why Scope is stated positively and negatively.** The DO list alone defines a centre without an edge, and an agent asked to do something adjacent will reason its way in. The DO NOT list is where the single-responsibility architecture is actually enforced, and it works best when each entry names the other agent's job rather than a prohibition: "requirements are defined elsewhere" is followed more reliably than "do not define requirements", because the first tells the agent what to do with the request.

### 4.2 CommunicationProtocol

Top-level `[[DEPLOYED:CommunicationProtocol]]`, slot 2. Empty in source. Content is the role-matched block from `CommunicationProtocol.md`, which is the whole orchestration contract: message shape, status and error vocabularies, the HITL gate, and the artifact provenance stamp.

Nothing is authored here, and no injection sits inside it — the region is regenerated wholesale, so anything nested would be destroyed on the next deploy (§2.1). A project needing to extend the protocol uses `[[INJECTION:ProtocolExtension]]` as a top-level sibling of this region; §6.1.1 covers what belongs there and what does not.

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

Capabilities is the least constrained section by design. It is where an agent's actual expertise lives, and expertise does not have a common shape — a research agent needs an output template, an implementation agent needs its method's principles, an interface agent needs neither.

**Deployed:** none. The section carries no tool-managed region.

**Injections:** `[[INJECTION:CodebaseContext]]`, `[[INJECTION:OutputArtifactTemplate]]`, and for validation agents `[[INJECTION:SeverityThresholds]]` and `[[INJECTION:SeverityDefinitions]]` (§6.2). `[[INJECTION:LanguagePatterns]]` is available to a project with language-specific patterns to state, and is not carried by MOSAIC's own sources (§6.1).

**Tool access is not described here.** It is the `tools` frontmatter field. A prose list of tools in the body is a second statement that goes stale the first time the field changes.

### 4.4 Constraints

**Purpose:** what the agent must not do, and why.

**Deployed first:** `[[DEPLOYED:ProtocolConstraints]]`, at the top of the section.

**Author writes** the agent's own constraints after it. Two rules:

- **Every constraint carries its justification.** "Do NOT make assumptions about technology choices — document options instead, because downstream agents need unbiased options to evaluate against broader context." A bare prohibition invites a model to find the edge of it; a prohibition with a reason gives it grounds to generalise correctly to a case the rule did not anticipate.
- **Do not restate the contract.** Artifact access, status code discipline, and JSON response discipline are already stated twice above — once in the contract region, once in `ProtocolConstraints`. A third copy in the agent's own list is the copy that drifts.

**Deployed last:** `[[DEPLOYED:HarnessConstraints]]`.

### 4.5 ErrorHandling

**Purpose:** which status code this agent returns, in which situation.

**Deployed first:** `[[DEPLOYED:ErrorHandlingCommon]]`.

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

**Injection:** `[[INJECTION:ErrorHandlingExtension]]`, last.

### 4.6 OutputFormat

**Purpose:** what this agent reports, in the two response fields whose content is the agent's own.

**The JSON envelope is never restated here.** The contract region, a few inches up the same file, carries the full response schema for every status. This section carries only the parts that vary by agent.

**Author writes:**

```markdown
## Output Format

Your entire response is the JSON object the Communication Protocol defines. This section
specifies only what your `status_message` should say, and which `error_code` you return.

| Status | `error_code` | Example `status_message` |
|--------|--------------|--------------------------|
| `SUCCESS` | — | "{Concrete example naming real outputs and real counts.}" |
| `COMPLETED_NEEDS_ACTION` | — | "{…}" |
| `PARTIALLY_DONE` | — | "{What was done, why it stopped, what remains, where continuation context lives.}" |
| `BLOCKED` | `{E1nn}` | "{What is missing, stated so the orchestrator can act on it.}" |
```

Only the statuses this agent actually returns need rows. The `error_code` column is `—` on every row but `BLOCKED`, since the contract permits the field nowhere else. An agent with several distinct blockers gets **several `BLOCKED` rows**, one per code.

**Why this replaced worked JSON examples.** Every subagent used to carry three or four full JSON response objects. Outside `status_message` and `error_code` the envelope around them was identical — and thirty-five of forty-two had gone stale against the contract, showing responses with no `run_id`, several naming an `agent_instance_id` whose agent name was not the agent's own, and some still using pre-run-scoped artifact paths. Those agents were shown a non-conforming template immediately after being handed the contract that forbids it.

Reducing the section to the varying part fixes that permanently rather than thirty-five times. The envelope now exists in exactly one place per file and cannot drift from itself.

**Why the section survives at all**, and why it carries two columns rather than one:

`status_message` is the field agents write worst and the field the orchestrator routes and reports on. A concrete example of a good one, phrased in this agent's own vocabulary, is real guidance no generic text can supply.

`error_code` is the same case and was nearly lost to the same reasoning. The contract region supplies the *vocabulary* — the five codes and their meanings, rendered in the skeleton as the union placeholder `"E101|E401|E501|E502|E503"`. What it cannot supply is **this agent picking one**. That choice is agent-specific and demonstrably so: most agents block on a missing input and return `E101`, but `test-runner` returns `E501` when its runner is unavailable, `pull-request-comment-interface` returns `E502`, `requirements-refinement` returns `E503`, and `audit-to-pull-request` returns `E401`. Deleting the column would have replaced forty-two considered answers with a five-way menu, in the section a model reads immediately before it has to choose.

`result_data` is the field that did not survive this test: it appears in no agent's examples at all, its presence is governed entirely by an input flag, and there is nothing agent-specific to state. The varying set is exactly two fields, and the table has exactly two columns.

### 4.7 ExecutionPhilosophy

**Purpose:** the working posture — how the agent should think about context, quality, and its own limits.

**Deployed first:** `[[DEPLOYED:ExecutionPhilosophyCommon]]`.

**Then** `[[INJECTION:ContextLimits]]`.

**Then the agent's own bullets** — the mindset specific to this work. "Investigation only: report observations, not assessments." "Escalate, don't fight: if tests seem wrong, return `NEEDS_CLARIFICATION` rather than working around them." These are the section's reason to exist, and they belong to the agent.

---

## 5. Orchestrator Deviations

The orchestrator uses the same frontmatter schema, the same region kinds, and the same section names. Its body content is almost entirely its own.

### 5.1 Sections it does not carry

`OutputFormat` is absent. The orchestrator returns no protocol response — it *receives* them.

### 5.2 Sections whose content is orchestrator-specific

`Identity`, `Capabilities`, `Constraints`, `ErrorHandling`, and `ExecutionPhilosophy` are all present and share nothing but their names with the subagent versions. The orchestrator's authority hierarchy ranks workflow configuration and subagent responses, not its own scope boundaries. Its constraints are about context discipline and append-only state. Its error handling is a tiered retry-and-escalate strategy for codes it receives rather than a mapping for codes it returns. None of that is a variant of subagent text; it is different text that happens to live under the same heading.

### 5.3 Regions unique to it

`[[DEPLOYED:AvailableWorkflows]]` and `[[DEPLOYED:InfrastructureAgents]]`, both inside `Identity`, both assembled at deploy time from selected workflow and infrastructure declarations.

### 5.4 Nested template sections

The orchestrator's `Capabilities` contains nested `[[SECTION:]]` regions — `ExecutionLog`, `Artifacts`, `WorkflowNotes` — holding the templates it writes into the orchestration artifact. These are the one sanctioned use of nested sections, and they are nested because they are content *about* a capability rather than a document-level slot.

---

## 6. Injection Points

### 6.1 The Injection Name Set Is Open

**Every `[[INJECTION:]]` region is preserved, whatever it is called.** There is no allowlist of injection names, and an unrecognised name is not an error, a warning, or a reason to orphan content into `TODO.md`.

Preservation does not need a list. On update the tool matches injection names between the deployed file and the source file; a name the author put in their source is a name that will be there next time. A closed vocabulary would add nothing to that and would cost an author the ability to name a region after their own project's concept. `[[DEPLOYED:]]` names are the opposite case and stay closed (§2.5) — those the tool must find *content* for, and a name it does not recognise has no source.

**The catalogue below is a suggestion, not a constraint.** These are the names that mean the same thing across projects and that MOSAIC ships `TODO.md` guidance for. An author with a use for one should prefer it over inventing a synonym; an author with a genuine need for something else should invent it.

Most are also carried by MOSAIC's own sources, declared empty so a project can see the slot exists. `LanguagePatterns` is the exception and is deliberately **not** declared anywhere in `Agents/Generic/`: a language pattern is meaningful only once a project has a language, so pre-declaring it in every agent would ship an empty region and a `TODO.md` line to fifty-odd files on the chance that one project fills them. A project that wants it adds the region to its own sources like any other name it invents. Being catalogued costs nothing; being declared costs every agent.

| Name | Usual parent | Purpose |
|------|--------------|---------|
| `IdentityExtension` | `Identity` | Project or domain expertise added to the agent's identity |
| `CodebaseContext` | `Capabilities` | Knowledge of the project's codebase |
| `LanguagePatterns` | `Capabilities` | Language-specific coding patterns the project expects this agent to follow |
| `OutputArtifactTemplate` | `Capabilities` | The project's expected structure for this agent's output artifact |
| `SeverityThresholds` | `Capabilities` | Which issue severities require rework (validation agents) |
| `SeverityDefinitions` | `Capabilities` | What each severity means in this project (validation agents) |
| `ErrorHandlingExtension` | `ErrorHandling` | Project-specific recovery guidance |
| `ContextLimits` | `ExecutionPhilosophy` | Context window thresholds and guidance |
| `ProtocolExtension` | body top level | Deployment-specific protocol mechanics (§6.1.1) |

"Usual parent" is where the region belongs when the agent is otherwise conventional, and where MOSAIC's own agents put it. Placing one elsewhere is an author's call, not a validation failure. The single hard rule about placement is §6.3's: never inside a `[[DEPLOYED:]]` region.

#### 6.1.1 `ProtocolExtension`

Permitted, and a sibling of `[[DEPLOYED:CommunicationProtocol]]` at body top level — never inside it, since the contract region is regenerated wholesale and would take the extension with it.

It exists because a deployment can have real business extending the protocol without altering it. A project whose subagents run behind network endpoints while the orchestrator runs locally needs to say *how a message is delivered*; nothing in the contract covers transport, and that project's alternative to an extension region is forking the contract, which is strictly worse for everyone.

**The guidance, which is guidance and not a check:** extend the mechanics — transport, delivery, environment-specific handling. Do not restate or redefine what the contract already fixes: message shape, the status and error vocabularies, the HITL gate, the provenance stamp. An extension contradicting the contract leaves the agent with two answers and no basis to choose between them, and it will choose the nearer one (principle 7). That is a real hazard, and it is the project's to own — the same as any other injection.

**`ArtifactProvenanceExtension`** is retired rather than forbidden. The stamp it extended has folded into the orchestration contract, so the region no longer has a counterpart to extend; a project needing extra artifact frontmatter uses `OutputArtifactTemplate`, which governs artifact content rather than the contract. A file still carrying the region is stale, not invalid, and its content is preserved like any other injection.

### 6.2 Severity Injections

Validation agents carry two additional injections. `SeverityThresholds` is followed in source by a default table stating which severities require rework, which a project overrides:

| Severity | Requires Rework |
|----------|-----------------|
| CRITICAL | Always — not configurable |
| MAJOR | No by default |
| MINOR | No by default |
| SUGGESTION | No by default |

The status rule follows from it: any issue at a severity marked as requiring rework means `COMPLETED_NEEDS_ACTION`; otherwise `SUCCESS` with the issues recorded in the report. `SeverityDefinitions` optionally states what each level means in the project's terms.

### 6.3 Rules

One is enforced. The rest are guidance, and marked as such.

**Enforced — never nest an injection inside a `[[DEPLOYED:]]` region.** The parent is regenerated wholesale on every deploy and would take the child's content with it, silently. This is the only injection rule that errors, and it errors because violating it destroys the user's own work rather than because it offends the schema (rule 13).

**Guidance:**

- **An agent carries only the injections its instructions could use.** An interface agent that transports data between systems has no use for `CodebaseContext` or `OutputArtifactTemplate`, and including them produces empty regions a project author must read and dismiss.
- **A template must work with every injection empty.** That is the state of every source file and of every fresh deployment; an agent that only makes sense once a project has filled something in is broken by default.
- **Injections extend; they do not contradict.** An injection redefining a rule stated in a deployed region leaves the agent two answers with no basis to choose, and it will follow the nearer one (principle 7). The tag itself does not convey this, so the `TODO.md` text for each injection should.

**No injection is ever required to be filled.** Empty is the normal state, not a degraded one (principle 3), and unfilled regions belong in the `TODO.md` checklist rather than in a validator's output. This matters most where a parent section offers several alternative child injections: filling all of them is not the goal, and a rule demanding it would be demanding contradictory content. Which of them a project uses is the project's decision and nobody else's.

---

## 7. The Canonical Blocks

Five fragments of shared body text are deployed into every subagent. Their text lives in `Agents/Generic/DeployedSections.md` and nowhere else; their reasoning lives one document per block.

| Block | Fills region | Parent section | Reasoning |
|-------|--------------|----------------|-----------|
| `AuthorityHierarchy:Subagent` | `[[DEPLOYED:AuthorityHierarchy]]` | `Identity` | `DeploymentBlocks/AuthorityHierarchy.md` |
| `ClosingProcedure:Subagent` | `[[DEPLOYED:ClosingProcedure]]` | `Identity` | `DeploymentBlocks/ClosingProcedure.md` |
| `ProtocolConstraints:Subagent` | `[[DEPLOYED:ProtocolConstraints]]` | `Constraints` | `DeploymentBlocks/ProtocolConstraints.md` |
| `ErrorHandlingCommon:Subagent` | `[[DEPLOYED:ErrorHandlingCommon]]` | `ErrorHandling` | `DeploymentBlocks/ErrorHandlingCommon.md` |
| `ExecutionPhilosophyCommon:Subagent` | `[[DEPLOYED:ExecutionPhilosophyCommon]]` | `ExecutionPhilosophy` | `DeploymentBlocks/ExecutionPhilosophyCommon.md` |

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
| **MOSAIC's own sources** (`Agents/Generic/**`) — strict | Every rule is an error, warnings included. CI fails. |
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
| 5 | `required_skills` entries name existing folders under `Agents/Generic/Skills/` | Error | Tool | No |
| 6 | `id` is unique across the agent registry | Error | Tool | No |

All errors, and none of them can flag a creative agent: each is a field the tool reads, and a value it cannot use is a value that fails.

**Structure**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 7 | Top-level boundaries form a subsequence of the canonical order (§2.3) | Error | Tool | Partly — currently rejects unknown top-level names |
| 8a | `[[DEPLOYED:CommunicationProtocol]]` present (§2.4.1) | Error | Tool | No |
| 8b | The five conduct regions present for `role: subagent` (§2.4.1) | Warning | Tool | No |
| 8c | No region present that the role forbids (§2.4) | Warning | Tool | No |
| 9a | Every `[[DEPLOYED:]]` region sits under the parent named in §2.5 | Error | Tool | Yes |
| 9b | Every `[[INJECTION:]]` region sits under its usual parent (§6.1) | Advice | Tool | Yes — should be downgraded |
| 10 | No boundary name appears twice in one file | Error | Tool | Yes |
| 11 | Every opened boundary is closed, with matching name | Error | Tool | Yes |
| 12 | In a source file, every `[[DEPLOYED:]]` region is empty | Error | Tool | No |
| 13 | No `[[INJECTION:]]` region nested inside a `[[DEPLOYED:]]` region | Error | Tool | Yes |
| 14 | Every `[[DEPLOYED:]]` name is one the tool has a source for (§2.5) | Error | Tool | Yes |

Rule 14 replaces the former "`ProtocolExtension` does not appear". Injection names are open and unlisted names are preserved, not orphaned (§6.1); deployed names stay closed, because an unrecognised one leaves the tool with a slot and no content.

Rule 9b is the clearest case of a check to relax: it currently errors on an injection placed somewhere unusual, which harms nobody and destroys nothing.

**Content**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 15 | `Identity` contains `**Goal:**`, `**Scope:**` with a DO and a DO NOT, `**Litmus Test:**`, and a `### Process` list | Warning | Tool | No |
| 16 | No section outside `OutputFormat` contains a JSON object with a `status_code` key | Warning | Tool | No |
| 17 | `OutputFormat` contains no JSON code fence (§4.6) | Warning | Tool | No |
| 18 | No Process step mentions `human_in_the_loop` — `ClosingProcedure` deploys that | Warning | Tool | No |
| 19 | Every skill named in a Process step appears in `required_skills`, and vice versa | Error | Tool | No |
| 15r | Every constraint carries its justification (§4.4) | Advice | Review | — |
| 16r | The status mapping is specific to this agent, not paste-able into another (§4.5) | Advice | Review | — |
| 17r | `status_message` examples name real outputs and real counts, and each `BLOCKED` row's `error_code` is the one that agent's own failure mode produces (§4.6) | Advice | Review | — |
| 18r | The agent's scope does not overlap an existing agent's | Advice | Review | — |

Rule 19 is an error despite sitting under Content: a skill an agent is told to load but does not declare is a skill the deployment does not ship, and the agent fails at step 1. That is a broken deployment, not a style lapse.

Rules 15–18 warn in a project tree and error in MOSAIC's own. They caught nothing when they were unimplemented errors, and all three defects in `AgentBodyDrift.md` §5 sit in their scope.

The `r` rules are what §4 already asks for in prose. They are listed here so the specification stops implying that its unenforceable half does not count — and because MOSAIC can hand them to a validation subagent as a checklist.

**Bundle**

| # | Rule | Severity | Mechanism | Implemented |
|---|---|---|---|---|
| 20 | Every bundle block declares a valid `target`, `applies_to`, and an existing `specified_in` | Error | Tool | No |
| 21 | Every deployed agent's `bundle_version` equals the bundle's, and all agents in one deployment agree | Warning | Tool | No |
| 22 | Every bundle-sourced deployed region's body equals its block byte-for-byte | Warning | Tool | No |
| 23 | No document outside the bundle contains a block's opening or closing content line | Warning | Review | No |

21 and 22 warn because a stale or hand-edited deployment still runs — the user is being told to redeploy, not stopped. 22 catches a hand-edited deployed file; 23 catches a design document that started quoting what it was only supposed to explain. Elaborated in `DeployedSectionsBundle.md` §9.

### 9.4 Cheapest First

Rules 1–4 and 14 are a morning's work and are pure integrity. Rules 16–18 are a regex over one section each and cover the defect class that has cost the most so far. Rule 9b's downgrade and rule 7's relaxation are subtractions. None of it is blocked on the bundle work.

---

## 10. Migration

The migration touches forty-two subagent files. It is mechanical apart from one review step.

1. **Add `role: subagent`** to all forty-two, `role: orchestrator` to the orchestrator.
2. **Replace the Authority Hierarchy block** with an empty `[[DEPLOYED:AuthorityHierarchy]]` region.
3. **Delete the trailing HITL and return-JSON steps** from every Process list; add an empty `[[DEPLOYED:ClosingProcedure]]` region after the list, before the authority hierarchy region.
4. **Replace the five contract-restating constraint bullets** at the top of `Constraints` with an empty `[[DEPLOYED:ProtocolConstraints]]` region, keeping every agent-specific constraint below it.
5. **Replace the retry bullet** at the top of `ErrorHandling` with an empty `[[DEPLOYED:ErrorHandlingCommon]]` region, keeping the agent's status mapping. Delete any error-code recall bullet outright — the contract's region carries the full table.
6. **Replace the Context Management, Memory via Artifacts, and Quality over Completeness bullets** with an empty `[[DEPLOYED:ExecutionPhilosophyCommon]]` region at the top of `ExecutionPhilosophy`, ahead of `[[INJECTION:ContextLimits]]`.
7. **Rewrite `OutputFormat`** as the two-column table of §4.6, carrying across **both** varying fields of each existing example — its `status_message` text and, on `BLOCKED` examples, its `error_code` — and discarding the envelope around them. Where a file has several `BLOCKED` examples with different codes, each becomes its own row. **This is the one step requiring judgement per file** — the existing messages and code choices are good content in a stale wrapper, and they should survive.

   The instruction to carry `error_code` is not a detail. An earlier draft of this step named `status_message` alone, on the mistaken premise that nothing else varied; run that way, it would have silently discarded forty-two considered error-code choices — including every non-`E101` one — in the step that is least verifiable afterwards. Losing hand-authored content to a mechanical sweep is the failure mode this whole migration exists to end.
8. **Update the three vocabulary files** together (§10.1).
9. **Bump each agent's `version`** — minor, since regions were added and hand-authored content was removed. The bundle version does not move: its blocks did not change, only their destinations came into existence.

Steps 2–6 are verifiable two ways: after deployment, each region must be byte-identical across all forty-two files and to its block in the bundle (uniformity), and everything outside the touched regions must be byte-identical to the pre-migration file (isolation). Step 7 is not mechanically verifiable and needs review; rule 17 confirms only that no envelope survived.

**Before migrating any fragment,** diff its full block across all forty-two files. The counts in the drift analysis each test one representative line and are a triage signal, not a verification.

**Ordering against the provenance merge.** Folding provenance into the orchestration contract removes a top-level region from the same forty-two files. The two changes touch disjoint regions and may land in either order, but not concurrently — both rewrite the same files, and the isolation check of each requires the other not to be running.

### 10.1 Vocabulary tables

Three files hold the boundary vocabulary in machine-readable form: `Agents/Generic/SourceFilesFormat.md`, `Tools/Common/docformat/vocabulary.go`, and `Tools/OldAgentsTransform/boundary_constants.py`. This document is their specification; they are copies of it, and a partial update leaves them disagreeing about what is valid.

Changes this document makes to them:

- `CanonicalDeployed` gains `AuthorityHierarchy`, `ClosingProcedure`, `ProtocolConstraints`, `ErrorHandlingCommon`, `ExecutionPhilosophyCommon`. It remains a closed set (rule 14).
- `CanonicalDeployed` loses `ArtifactProvenance`, and `CanonicalSections` does not gain it — the region ceases to exist on the provenance merge.
- `CanonicalDeployed` loses `LanguagePatterns` and `CustomConstraints` (§2.5.1), leaving nine names. `DeployedParent` loses the same two entries. `LanguagePatterns` becomes a catalogued injection name (§6.1); `CustomConstraints` ceases to exist in any vocabulary.
- The deployed-name classifier's `default:` branch stops resolving to the harness class. `HarnessConstraints` becomes an explicit case, and an unclassified name is an error (§2.5.1).
- `DeployedParent` gains the five new names with the parents in §2.5.
- `CanonicalOrder` becomes seven slots, of which slot 2 is deployed, and is consumed as a subsequence rather than an equality check (§2.3).
- `InjectionParent` **stops being an allowlist.** Injection names are open (§6.1): an unlisted name is preserved like any other. What remains is a table of usual parents for the suggested names, consulted for advice-level reporting only. `ArtifactProvenanceExtension` leaves it, `ProtocolExtension` enters it with an empty parent (top level).

`SourceFilesFormat.md` should be reduced to a pointer at this document plus the skill and hook-bundle conventions it uniquely covers, rather than restating the agent format alongside it (§15).

---

## 11. Non-Goals

- **A copy-paste template.** A complete agent file offered for copying is how forty-two copies of shared text came to exist. §4 specifies each section; the bundle supplies the shared text at deploy time. Neither is a thing to paste.
- **Contract wording.** Placed here, specified in `CommunicationProtocol.md`.
- **Canonical block wording.** Placed here, reasoned about in `DeploymentBlocks/`, and held only in the bundle.
- **Utility agents.** No boundary tags, never deployed into a run, outside the schema.
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
| **Source file** | The MOSAIC-maintained agent file under `Agents/Generic/`. Deployed regions are empty; injections are empty. |
| **Deployed file** | The output of running the deployment tool over a source file. Deployed regions are filled; injections carry project content. |
| **Region** | A named span of a file bounded by a matched pair of markers. |
| **Section** | A `[[SECTION:]]` region — MOSAIC-authored, carried verbatim. |
| **Deployed region** | A `[[DEPLOYED:]]` region — written by the tool, regenerated on every deploy. |
| **Injection** | An `[[INJECTION:]]` region — project-authored, preserved byte-identically. |
| **Canonical block** | Deployed text maintained in one place and copied into every agent carrying the matching region. |
| **Bundle** | `Agents/Generic/DeployedSections.md` — the file holding every canonical block. A payload, not a specification. |
| **Bundle version** | The bundle's semver, stamped into every deployed file's frontmatter. Governed by `DeployedSectionsBundle.md`. |
| **Contract** | Text defining something two parties must agree on. There is one: the orchestration contract in `CommunicationProtocol.md`. It carries its own version and is not in the bundle. |
| **Specifying document** | The design document holding a block's rationale and changelog, named in the block's `specified_in`. Holds no copy of the block. |
| **Canonical order** | The fixed sequence of seven top-level boundaries every agent file follows (§2.3). |
| **Role** | `subagent` or `orchestrator`. Declared in frontmatter; selects which canonical text a file receives. |
| **Drift** | Divergence between copies of text that was once identical and has no single source. |

---

## 13. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 1.4 | 2026-08-08 | **`LanguagePatterns` and `CustomConstraints` removed from the deployed vocabulary.** Both were listed in §2.5 with the content source *"Deployment configuration"* — a category this document named and never specified, and that no tool ever implemented. Lacking a generator, both fell through the classifier's permissive `default:` branch to the harness class, which meant every harness had to declare them empty solely to satisfy §2.4's content-source check; the rationale text in those files records an author noting the classification was wrong and complying anyway. `CanonicalDeployed` drops from eleven names to nine and the phantom source disappears, leaving the three §2.5 actually describes. `LanguagePatterns` becomes a catalogued injection name (§6.1), deliberately not declared in MOSAIC's own sources. `CustomConstraints` is deleted outright — it had no definition, no specification, and served as an escape hatch for content that belonged in `HarnessConstraints`. New §2.5.1 makes the underlying rule explicit: a deployed name must name an implemented generator, and an unclassified name is an error rather than a default. Role matrix, tier table, §4.3, §4.4 and §10.1 follow. |
| 1.3 | 2026-08-05 | **`OutputFormat` corrected to two varying fields.** v1.0 reduced the section to `status_message` on the premise that nothing else in the response envelope varied between agents. A survey of the forty-two sources disproves it: `error_code` varies too — most agents return `E101`, but `test-runner` returns `E501`, `pull-request-comment-interface` `E502`, `requirements-refinement` `E503`, and `audit-to-pull-request` `E401`. The contract region supplies the code vocabulary and can never supply an agent's choice from it, so §4.6's table gains an `error_code` column and permits multiple `BLOCKED` rows. Migration step 7 amended accordingly — as written it would have discarded every one of those choices in its least verifiable step. Rule 17r extended to review the code choice alongside the message. The envelope deletion itself is unchanged and rule 17 is untouched: the widened table contains no JSON fence. |
| 1.0 | 2026-08-05 | **Initial specification.** Established the agent file schema: frontmatter fields including a declared `role`, the three region kinds and their ownership semantics, canonical document order, a per-role region matrix, and a section-by-section content specification. Settled the canonical-fragment question: five fragments become `[[DEPLOYED:]]` regions, one is deleted. Rewrote `OutputFormat` as a `status_message` table with no JSON envelope. Corrected three live defects structurally. |
| 1.2 | 2026-08-05 | **Conformance made proportionate.** §9 rewritten: every rule now carries a severity (error / warning / advice) and an enforcement mechanism (tool / review / guidance), and a strict/lenient mode holds MOSAIC's own sources to all of it while a project's agents are blocked only by what would otherwise break. Injection names opened — no allowlist, all injections preserved whatever they are called, none ever required to be filled; the catalogue is a suggestion. `ProtocolExtension` un-banned and specified as a top-level sibling of the contract region, since extending protocol *mechanics* is a legitimate deployment need. Canonical order relaxed from an equality check to a subsequence: absent sections and additional ones are both fine, relative order is not. Deployed-region absence graded into contract / conduct / deployment tiers. Two principles added: a check that flags legitimate work gets switched off, and not every rule is a tool's to enforce. | 
| 1.1 | 2026-08-05 | **Split into schema only.** Bundle mechanics moved to `DeployedSectionsBundle.md`; per-block reasoning moved to five documents under `DeploymentBlocks/`; the drift measurement and its Option A/B/C decision moved to `Development/Analysis/AgentBodyDrift.md`. Applied the settled decisions that post-dated v1.0: the bundle holds **no contracts**, so `CommunicationProtocol` deploys from its own source; the artifact provenance stamp folds into the orchestration contract, removing canonical slot 3, its role-matrix row, and its section spec, and taking the canonical order from eight slots to seven; the `bundle_version` stamp moved from a region-body comment to deployed-file frontmatter. `ArtifactProvenanceExtension` removed from the injection catalogue: with the stamp inside the contract, it dies by the argument that killed `ProtocolExtension`. |

---

## 14. Open Ideas / Dead Ends

**Under consideration**

- **Retiring `id`.** No consumer was found — matching is by `name` throughout the inspected tooling (§3.3). Retiring it is a forty-two-file change and a round-trip contract change for no functional gain, so it stays. Worth reopening if a registry or an external reference ever needs a rename-stable key, and worth closing outright if a survey confirms nothing reads it.
- **Renaming `OutputFormat`.** The section no longer specifies a format; it supplies this agent's `status_message` examples and `error_code` choices. `StatusReporting` would describe it. Deferred because a rename costs the canonical order, three vocabulary files, and forty-two files, for clarity alone.

**Rejected**

- **A required structure for `Capabilities`.** It is the one section with no specified shape (§4.3), which is right for expertise that genuinely varies. Considered requiring `### Core Capabilities` as a minimum; decided against. The template already shows it, every agent follows it, and promoting a convention to a rule buys nothing. If content validation lands it would be a warning at most, alongside rule 15.
- **A closed vocabulary of injection names.** Held until v1.2. Its stated purpose was to stop a user's content being orphaned on update — but preservation matches names between the deployed file and the source file, so a name the author wrote into their own source is preserved with or without a list. The list bought nothing and cost an author the ability to name a region after their own project's concept (§6.1). Deployed names are the opposite case and stay closed: there the tool must find *content* for the name.
- **Banning `ProtocolExtension`.** Held until v1.2 on the argument that a project able to append to a contract is able to contradict it. True, and insufficient. A deployment whose subagents sit behind network endpoints has to state how a message is delivered, which the contract does not cover; denying it a region does not prevent the change, it forces a fork of the contract instead. Contradicting the contract remains a real hazard and is now stated as guidance the project owns (§6.1.1).
- **One uniform strictness for every tree.** Would mean either blocking legitimate project agents or letting MOSAIC's own sources off the rules they exist to demonstrate. The strict/lenient split (§9.2) is what lets the recipe be enforced on the recipe without being enforced on its readers.
- **Requiring every injection in a file to be filled.** Empty is the normal state of a source file and of a fresh deployment (principle 3), and where a section offers alternative child injections the rule would demand contradictory content (§6.3).
- **A `function` or `category` frontmatter field.** The folder already states it, and a second copy is undetectably wrong when the two disagree (§3.5).
- **`agent_class` in agent frontmatter.** Would create a second declaration of a fact the orchestrator's declaration region owns and the executor actually reads (§3.5).
- **Single-sourcing everything repeated, or deleting every contract echo.** Both pure options were rejected in favour of a per-fragment split. Reasoning in `Development/Analysis/AgentBodyDrift.md` §4.
- **Orchestrator variants of the five canonical blocks.** Four would share no sentence with their subagent counterparts, maintained in a file the orchestrator's author does not otherwise open, to protect a single copy from diverging from itself. `AuthorityHierarchy` is the close call — it genuinely is a variant — and was rejected on cost, leaving a review obligation instead (§8).
- **Keeping worked JSON response examples per agent.** Outside `status_message` and `error_code` they were identical, and thirty-five of forty-two had gone stale against the contract they shipped beside. Both varying fields survive as columns of the §4.6 table; the envelope does not.
- **Reducing §4.6 to `status_message` alone.** Held until v1.3, on the premise that it was the only field varying between agents. It was not: `error_code` varies too, and the contract region can only supply the five-code vocabulary, never this agent's choice from it (§4.6). The premise also reached into migration step 7, where acting on it would have destroyed the choices rather than merely omitted them.
- **A `[[DEPLOYED:]]` region for the Process list itself.** Every Process list is agent-specific; only its closing steps were shared. Deploying the whole list would have meant no list at all.
- **"Deployment configuration" as a content source.** Held until v1.4 as the source for `LanguagePatterns` and `CustomConstraints`. It described no mechanism, had no specification behind it, and was never implemented — the `Specified in` column carried an em-dash for both rows, which is this document admitting in its own table that nobody had written the thing down. A source that exists only as a phrase in a table is worse than an acknowledged gap: the closed-set rule forced every harness to satisfy it with empty declarations, so the fiction propagated into four harness files and every deployed agent. §2.5.1 now requires a named, implemented generator.
- **A per-agent `LanguagePatterns` region in MOSAIC's own sources.** Considered when reclassifying it as an injection in v1.4, on the precedent of `CodebaseContext`, which is declared empty in thirty-seven sources. Rejected: `CodebaseContext` describes a codebase every project has, while language patterns are meaningful only to a project that has settled on a language and wants its agents constrained by it. Declaring it everywhere would ship fifty-odd empty regions and `TODO.md` lines to make one project's case marginally easier. It stays catalogued and undeclared (§6.1).
- **A separate `ArtifactProvenance` canonical slot.** Held slot 3 in v1.0. Removed: the stamp is verified by the orchestrator, which makes it hard interop rather than an audit convenience, and a contract with two version numbers in two regions is a contract that can disagree with itself.

---

## 15. Open Items

- **Most of §9 is unimplemented, and the implemented part is now wrong in three places.** `docformat/validate.go` checks boundary structure only — no frontmatter conformance, no section content, no bundle comparison. On top of that backlog, three live checks no longer match this document: `unknown-injection` must go entirely (§6.1), `out-of-order-section` must become a subsequence test rather than rejecting unknown top-level names (§2.3), and injection parent placement must drop from error to advice (rule 9b). The three subtractions are the fastest way to stop the validator flagging legitimate work.
- **Severity and mode are specified but the validator has one of each.** Everything it reports is an error, and it behaves identically on MOSAIC's tree and a user's. §9.1's two axes and §9.2's strict/lenient split both need building, along with routing warnings into `TODO.md` and the deployment summary rather than only to a console.
- **No review-mechanism rules run.** Rules 15r–18r are specified with `Review` as their mechanism and nothing performs them. The intended vehicle is a validation subagent given them as a checklist; that agent does not exist.
- **The provenance merge is specified but not executed.** The design layer is done — `CommunicationProtocol.md` v1.10 owns the stamp, and `ArtifactProvenance.md` is a tombstone. What remains is mechanical: forty-two agent files still carry `[[DEPLOYED:ArtifactProvenance]]` and `[[INJECTION:ArtifactProvenanceExtension]]`, and the three vocabulary copies plus the `Tools/Common/testdata/boundary/` fixtures still encode eight-slot ordering. Until that lands, deployed agents do not match §2.3.
- **The HITL obligation is stated twice** in every subagent file: once in the contract's deployed region, once in `ClosingProcedure`. Both single-sourced, so they cannot drift accidentally. Options and the argument are in `DeploymentBlocks/ClosingProcedure.md` §7.
- **The deployment tool does not read the bundle at all.** Until it does, the five blocks are deployed nowhere and the migration in §10 cannot complete. Tracked in `DeployedSectionsBundle.md` §10.
- **The tool's role enum says `worker`; every design document says `subagent`.** `domain.AgentRole` is `worker` / `orchestrator` / `utility`. The frontmatter field uses `subagent` (§3.2); the code should follow, or map explicitly at the boundary.
- **Role is still inferred from path in the tool.** The frontmatter field is specified here but nothing reads it. Until it does, `role` is documentation, and a file moved between folders still changes what it is.
- **`SourceFilesFormat.md` and this document overlap.** That file states the agent format from the tool's side, with no rationale, and has already drifted. It should be reduced to a pointer plus the skill and hook conventions it uniquely covers (§10.1).
- **`InfrastructureAgentConcept.md` §3.1 shows `[[INJECTION:InfrastructureAgents]]`.** The vocabulary has it as `[[DEPLOYED:]]`, and the orchestrator source uses `[[DEPLOYED:]]`. The design document is the one that is wrong, and it is the document a reader would trust. `Agents/Generic/Agents/Infrastructure/README.md` line 7 carries the same error and should be corrected with it.
- **The orchestrator's `ErrorHandling` section extends far past its injection.** `[[INJECTION:ErrorHandlingExtension]]` closes around line 493 and the section continues to line 715 with the Core Orchestration Loop, which is neither error handling nor an extension of it. The current arrangement means a project injection lands in the middle of unrelated material.
- **The `MosaicTest` agents were not surveyed.** Three agents under `Agents/Generic/Agents/MosaicTest/` exist to exercise the harness rather than to do work. Whether they conform to this schema, and whether they should, is unexamined.
