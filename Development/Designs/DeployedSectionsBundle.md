---
id: deployed-sections-bundle
type: specification
version: "1.2"
name: "Deployed Sections Bundle"
description: "The design of the canonical block bundle: what qualifies as a block, why contracts are excluded, how bundle_version works, how the tool deploys blocks, and how staleness is detected."
author: MOSAIC
status: Draft
---

## 1. What This Specifies

`Catalog/DeployedSections.md` is the single file holding every piece of text MOSAIC deploys verbatim into agent files. This document specifies it: what qualifies for membership, how it is versioned, how the deployment tool consumes it, and how a stale deployment is detected.

The bundle is a **payload**. This document, and the per-block documents under `Development/Designs/DeploymentBlocks/`, are the **rationale**. The split is deliberate and one rule keeps it honest:

> **No document reproduces a block from the bundle.**

Design documents discuss canonical text, name its fields, and explain why it says what it says. They never hold a second copy — a copy is always the one that goes stale and always the one a reader finds first. §9 states the mechanical form of this rule.

The current membership, and where each block's reasoning lives:

| Block | Fills region | Reasoning |
|-------|--------------|-----------|
| `AuthorityHierarchy:Subagent` | `<AuthorityHierarchy type="managed">` | `DeploymentBlocks/AuthorityHierarchy.md` |
| `ClosingProcedure:Subagent` | `<ClosingProcedure type="managed">` | `DeploymentBlocks/ClosingProcedure.md` |
| `ProtocolConstraints:Subagent` | `<ProtocolConstraints type="managed">` | `DeploymentBlocks/ProtocolConstraints.md` |
| `ErrorHandlingCommon:Subagent` | `<ErrorHandlingCommon type="managed">` | `DeploymentBlocks/ErrorHandlingCommon.md` |
| `ExecutionPhilosophyCommon:Subagent` | `<ExecutionPhilosophyCommon type="managed">` | `DeploymentBlocks/ExecutionPhilosophyCommon.md` |

Where each region sits in an agent file is `AgentTemplateArchitecture.md` §2.5, which owns that fact; this document does not restate it.

All five are `applies_to: subagent`. The orchestrator carries none of them, and `AgentTemplateArchitecture.md` §8 says why.

**Outside this document:** which regions exist and where they sit in an agent file (`AgentTemplateArchitecture.md`), what any individual block says and why (the block documents above), and the orchestration contract's own content and versioning (`CommunicationProtocol.md`).

---

## 2. Membership

A block belongs in the bundle when all three hold:

1. Its text is **identical across every agent of a role** — no per-agent, per-project, or per-harness variation.
2. It is **authored by MOSAIC** — not assembled from a deployment's selections, and not written by a project.
3. **It is not a contract.** Nothing in it defines something two parties must agree on.

The first two are obvious. The third is the one that decides hard cases, and it is decidable rather than a matter of judgement. Ask what breaks when an orchestrator and a subagent carry different versions:

| Mismatch | Breaks? |
|---|---|
| Subagent speaks orchestration contract 1.7, orchestrator 1.9 | **Yes.** No `run_id` in responses; the orchestrator's provenance check finds nothing to read. A wire disagreement. |
| Subagent carries bundle 2.0.0, orchestrator carries 3.0.0 | **No.** The retry wording differs. Messages still parse, artifacts still stamp, routing is unaffected. |

Everything in the bundle **restates** a contract or states agent-local behaviour. `ProtocolConstraints` compresses artifact-access rules into imperatives. `ExecutionPhilosophyCommon` recalls the status codes. `ErrorHandlingCommon` states a subagent-side retry rule the contract deliberately leaves to policy. If any of them drifted, an agent would behave worse — no contract would be violated, because the contract is stated elsewhere in the same file and the agent still speaks it.

That is why `bundle_version` is never a compatibility number, and why an agent at one bundle version interoperates perfectly with an orchestrator at another.

### 2.1 What is excluded

**The orchestration contract.** Message shape, the status and error vocabularies, the human-in-the-loop gate, and the artifact provenance stamp are one contract with one version, deployed from its own source into `<CommunicationProtocol type="managed">`. It is exactly what orchestrator and subagent must agree on, and a version bump there means compatibility may have changed.

Mixing it into the bundle would make every wording fix in an unrelated block look like a contract change. That is not a cosmetic problem: everything downstream that reasons about protocol compatibility would be reasoning about noise, and a contract version that moves without the contract moving is worse than no version at all.

**Per-deployment assembled regions.** `AvailableWorkflows`, `InfrastructureAgents`, and `HarnessConstraints` are managed-type regions too, but their content comes from a deployment's own selections. There is no canonical text for the bundle to hold. The full list of deployed names and their sources is in `AgentTemplateArchitecture.md` §2.5.

---

## 3. Versioning

`bundle_version` covers the whole file. Any change to any block bumps it, and every deployed agent becomes stale at once.

There is no partial deploy: the tool regenerates every deployed region on every run, so it is not possible to update one region and leave another. A per-block version would therefore describe a granularity nothing can act on — and it would restore the per-source marker proliferation the single bundle exists to end.

Semver, with tiers describing **what redeploying would change for an agent**:

| Bump | Meaning |
|------|---------|
| **Major** | An agent's obligations change. A new rule, a changed rule, a removed one. |
| **Minor** | Guidance is clarified or expanded without changing what an agent must do. |
| **Patch** | Wording, formatting, typography. No change in meaning. |

**Staleness is not tiered.** A deployed agent whose `bundle_version` differs from the bundle's is out of date, whatever the tier. The tier tells a user how much to care about redeploying now rather than later — which is the same decision agent versions already serve with semver, and the reason a plain integer was rejected despite being more honest about there being no wire semantics.

**No block carries a version of its own.** The five blocks are shared instructions, not agreements between parties, so there is nothing for a separate number to track. Contract versions belong to contracts, which are not in the bundle.

### 3.1 The one-version invariant

**Every agent file in one deployment carries the same `bundle_version`.**

This is what catches a partial deploy, an interrupted run, or an agent added by hand from an older source. It is checkable across a deployment folder without reading a single region body, and it is the cheapest integrity check the system has.

**Why this is a non-issue in practice.** The deployment tool updates all agents in a folder together — a single `deploy` or `update` command processes every agent in the deployment. Whole-folder updates satisfy the invariant mechanically: every agent receives the same bundle version in the same run, so there is no window in which versions disagree. A future change to per-agent updates would need to preserve this property, either by stamping the same version or by checking the folder-wide invariant after the single-agent write.

### 3.2 Where the stamp goes

`bundle_version` is written into the **deployed file's frontmatter**. Not into region bodies.

A version comment repeated once per region, in every file, costs context-window tokens in every agent's prompt to serve a reader that is not the agent. Frontmatter is already parsed by the tool, so the stamp goes there: one field, read by the tool, invisible to the model.

The agent's own `version` field is untouched by any of this. When a block changes, forty-two agents receive new text and none of them change version — their own source did not change, and a bump would claim an authorship that did not happen. That separation is the point of single-sourcing: a canonical text change is one version bump in one file, not forty-two.

---

## 4. How the Tool Uses the Bundle

For each agent being deployed:

1. Read the agent's `role` from frontmatter.
2. For each `<{Target} type="managed">` region present in the file, find the bundle block whose `target` matches and whose `applies_to` matches the role. Regions not sourced from the bundle are resolved from the contract's own source or from the deployment's selections (`AgentTemplateArchitecture.md` §2.5).
3. Replace the region's **entire** body with the block's content, verbatim.
4. Write `bundle_version` into the deployed file's frontmatter.
5. Resolve project-type regions (`<Name type="project">`) — preserving existing content on update, leaving them empty and listing them in `TODO.md` on create.

**Step 5 comes last, always.** A region regenerated after an adjacent injection is resolved would discard content the tool had just placed.

Three edge obligations:

- **An absent region is graded, not automatically an error.** `AgentTemplateArchitecture.md` §2.4.1 sorts deployed regions into three tiers: a missing `CommunicationProtocol` is an error, a missing conduct region — which is what all five bundle blocks fill — is a warning, and a missing per-deployment region is silent. A block with no matching region in a given agent is likewise normal, and never an error.
- **A region present with no block matching the role is a hard error.** Deploying it empty ships an agent that appears complete and instructs nothing.
- **A managed-type region (`<Name type="managed">`) with content in a *source* file is an error.** Source regions are empty by definition; content there is either a hand-edit about to be silently discarded, or a deployed file mistakenly committed as source.

---

## 5. Changing a Block

1. Edit the block in the bundle.
2. Bump `bundle_version` at the tier the change warrants (§3).
3. Add one row to the bundle's manifest.
4. Record the reasoning in the document named by the block's `specified_in`, including a changelog entry there.
5. Redeploy.

Step 4 is not optional bookkeeping. A block whose text changed and whose specifying document did not is a block whose stated reasoning no longer explains it, and that is how a future editor reverts a deliberate decision.

---

## 6. Staleness Detection

One string comparison per agent: the deployed file's `bundle_version` against the bundle's. No prose parsing, no dependence on the wording that follows, and no per-block bookkeeping.

What it does not catch is a **local edit that preserves the version** — someone hand-editing a deployed region in place. Consolidating to one bundle narrows that gap, since there is now one number to forge rather than three, without closing it. The interim mitigation is validator rule 22 in `AgentTemplateArchitecture.md` §9, which compares each deployed region's body against its block byte-for-byte. A recorded per-block hash would catch both and is still open (§10).

---

## 7. Why a Bundle Rather Than Per-Document Blocks

The alternative — and the arrangement the orchestration contract itself still uses — is for each specifying document to hold its own deployable text in its first section, with the rationale below it. Rejected on three counts.

**A tool wanting the text must parse a thousand-line design document to extract two blocks**, and must first know which documents to look in. There is no enumeration; the list of places to look is itself hand-maintained.

**Every source adds a version marker to every agent file.** A staleness check then costs one comparison per source rather than one per agent, and the markers accumulate in files whose whole point is to be read by a model.

**Nobody can answer "what text do our agents actually receive?"** without opening every design document and mentally filtering out the prose. That is the question a reviewer asks most often, and the bundle answers it in one file.

The cost of the bundle is the no-reproduction rule (§1, §9) — a discipline, enforced mechanically, that the rationale documents never quote what they explain.

---

## 8. Why Not Also Deploy the Orchestrator's Shared Text

Single-sourcing exists to stop forty-two copies from diverging. **One copy cannot diverge from itself.** Deploying the orchestrator's own text into the orchestrator's own file from a third file adds a hop and a staleness surface to buy nothing.

For four of the five blocks there is also no shared text to hold: the orchestrator's equivalents share no sentence with their subagent counterparts. `AuthorityHierarchy` is the exception — it is a genuine variant, and excluding it costs a review obligation rather than nothing. The full argument is in `AgentTemplateArchitecture.md` §8.

The general form of the question, should it come up for a future block: **do the two texts state one rule for two readers?** Where they do, the risk is not two copies diverging by accident but the rule being amended in one role and not the other, and that risk is unaffected by there being a single orchestrator. That is what makes such a case arguable at all — and, so far, still not worth a second block.

---

## 9. Checkable Rules

These are the bundle's share of the validator rules; they are numbered as they appear in `AgentTemplateArchitecture.md` §9.

Severities and enforcement mechanisms are assigned there; they are repeated here so the rules read as they will be applied.

20. **Error, tool.** Every bundle block declares a `target` that is a recognised managed-type region name, an `applies_to` that is a recognised role, and a `specified_in` naming a file that exists.
21. **Warning, tool.** Every deployed agent's frontmatter `bundle_version` equals the bundle's `bundle_version`, and all agents in one deployment agree (§3.1).
22. **Warning, tool.** Every deployed region's body sourced from the bundle equals its block byte-for-byte.
23. **Warning, review.** No document outside the bundle contains a block's opening or closing content line. This is the mechanical form of the no-reproduction rule, and it is what stops the split between payload and rationale from quietly reverting.

Rule 22 catches a hand-edited deployed file. Rule 23 catches a design document that started quoting what it was only supposed to explain.

21 and 22 warn rather than error because a stale or hand-edited deployment still runs. Nothing is broken on the wire — `bundle_version` is never a compatibility number (§2) — so the user is being told to redeploy, not stopped from working.

---

## 10. Open Items

- **The deployment tool does not read the bundle at all.** It deploys the orchestration contract from that contract's own file, which is the mechanism this document replaces. Until the bundle is wired in, the five blocks are deployed nowhere and the migration in `AgentTemplateArchitecture.md` §10 cannot complete. This is the single blocking item.
- **A conformance hash per block.** Version comparison catches staleness but not a local edit preserving the version (§6). A hash of each block, recorded at deployment, catches both. The bundle makes this materially easier than it was — one file to hash from, one frontmatter field to extend.
- **The contract's `version` field versus the bundle's.** `CommunicationProtocol.md` keeps its own `version`, which is correct: it is a contract and it is not in the bundle. What is unsettled is whether a deployed file should carry both that number and `bundle_version` in frontmatter, or whether the contract version is recoverable from the deployed region's own prose. Two fields is the safer answer and has not been ruled on.

---

## 11. Changelog

| Version | Date | Summary |
|---------|------|---------|
| 1.2 | 2026-08-08 | **Per-deployment region list corrected** to follow `AgentTemplateArchitecture.md` v1.4. `LanguagePatterns` and `CustomConstraints` are removed from §4's list of assembled managed-type regions: neither had a generator, both were listed under a content source that was never implemented, and both leave the deployed vocabulary entirely. No change to the bundle itself — it never held either one, and the five blocks are untouched. |
| 1.1 | 2026-08-05 | **Aligned with the conformance rework** in `AgentTemplateArchitecture.md` v1.2. An absent deployed region is graded rather than uniformly permitted: the five bundle blocks fill conduct-tier regions, whose absence is a warning (§4). Bundle rules 20–23 gain severities and mechanisms — 20 errors, 21–23 warn, since a stale or hand-edited deployment still runs and `bundle_version` carries no wire semantics (§9). |
| 1.0 | 2026-08-05 | **Initial specification.** Split from `AgentTemplateArchitecture.md` §10. Establishes the bundle as the single source and single version for all verbatim-deployed MOSAIC text, with the three-part membership test and its decidable contract criterion. Records that the bundle holds **no contracts** — the orchestration contract, including the artifact provenance stamp, deploys from its own source — so per-block contract versions in the bundle frontmatter are removed from the design. Moves the `bundle_version` stamp from a region-body comment to the deployed file's frontmatter, and states the one-version-per-deployment invariant. |

---

## 12. Rejected

- **Keeping canonical blocks inside their specifying design documents.** Three counts, in §7.
- **A per-block version in the bundle.** Would let a stamp say which block changed. Rejected because there is no partial deploy, so per-block precision describes a granularity nothing can act on, and it restores the marker proliferation the bundle exists to end. The manifest answers "what changed in this version" without putting the answer in forty-two files (§3).
- **Holding contract versions in the bundle frontmatter.** The form this design took before the contract was excluded from the bundle entirely. Once the bundle holds no contracts, there is no contract version for it to declare, and a version field describing text the file does not ship is a fiction (§2.1).
- **Merging the contract version into `bundle_version`.** A single number is tempting. Rejected because a typo fix in an unrelated block would promote the orchestration contract to a new version, and everything downstream reasoning about compatibility would be reasoning about noise (§2.1).
- **Writing `<!-- bundle-version: X -->` into each deployed region body.** The original design. Rejected: it repeats a token cost in every agent's context window, once per region, to serve the tool rather than the model. Frontmatter is already parsed (§3.2).
- **A plain integer for `bundle_version`.** Honest about there being no compatibility semantics on the wire, but wrong about the consumer — the reader is a person with a deployed project deciding whether to redeploy now or later. It would also be the one version field in MOSAIC that parses differently from every other (§3).
