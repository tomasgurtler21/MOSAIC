---
version: "1.2"
name: "Requirements to Test Cases Workflow"
description: "Derive abstract test cases from a requirement held in large external specification documents. Resolves the requirement to closure via runtime retrieval, models the test scenario space, authors test cases in a project-defined format, and exports them to the target system."
hint: "Generate abstract test cases from specification documents via runtime retrieval — no implementation"
author: MOSAIC
id: requirements-to-test-cases
referenced_agents:
  - document-research
  - requirements-review
  - test-scenario-designer
  - test-scenario-review
  - approval-presenter
  - test-case-writer
  - test-case-review
  - test-case-export
artifacts:
  - Requirements.md
  - Research.md
  - requirements-review.md
  - TestScenarios.md
  - test-scenario-review.md
  - approval-presenter-scenarios.md
  - TestCases.md
  - test-case-review.md
  - approval-presenter-cases.md
  - ExportReport.md
---

[[SECTION:Workflow:requirements-to-test-cases]]
<!-- workflow-version: 1.1 -->
## Requirements to Test Cases Workflow

> **Version:** 1.1

**Use when:** Deriving abstract test cases from requirements that live in **large external specification documents** the agents never ingest wholesale — accessed instead through retrieval tooling at runtime. Resolves one requirement (or a small set) to dependency closure, models the test scenario space, authors test cases in a project-defined format, and exports them to the target test management system. No implementation, no test code.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | document-research | ❌ | requirements-review | - | - | Requirements.md | Research.md |
| RESEARCH | requirements-review | ❌ | test-scenario-designer | document-research | - | Requirements.md, Research.md | requirements-review.md |
| DESIGN | test-scenario-designer | ❌ | test-scenario-review | - | - | Requirements.md, Research.md | TestScenarios.md |
| DESIGN | test-scenario-review | ❌ | approval-presenter(scenarios) | test-scenario-designer | - | Requirements.md, Research.md, TestScenarios.md | test-scenario-review.md |
| DESIGN | approval-presenter(scenarios) | ✅ | test-case-writer | test-scenario-designer | - | TestScenarios.md, test-scenario-review.md | TestScenarios.md, approval-presenter-scenarios.md |
| EXECUTION | test-case-writer | ❌ | test-case-review | test-scenario-designer | - | Requirements.md, Research.md, TestScenarios.md | TestCases.md |
| REVIEW | test-case-review | ❌ | approval-presenter(cases) | test-case-writer | - | Requirements.md, Research.md, TestScenarios.md, TestCases.md | test-case-review.md |
| REVIEW | approval-presenter(cases) | ✅ | test-case-export | test-case-writer | - | TestCases.md, test-case-review.md | TestCases.md, approval-presenter-cases.md |
| COMPLETION | test-case-export | ✅ | COMPLETE | test-case-writer | - | TestCases.md | ExportReport.md |

**Notes:**

- **Requirements.md is user-created** — names the target requirement identifier(s), the document set in scope, and any retrieval scope limits.
- **`NEEDS_CLARIFICATION` from any row routes to `document-research`**, which is re-entrant and accepts a targeted "this is missing" invocation.
- **Route findings one hop upstream**, to the agent that produced the artifact the finding is about — not further.
- **`approval-presenter` rows are the only human gates.** Each is reachable only via `On Success` from its reviewer, so it runs once the loop has converged. Dispatch it with the approved artifact in **both** `input_artifacts` and `output_artifacts` — it stamps `human_approved` there and must be permitted to write it.

[[/SECTION:Workflow:requirements-to-test-cases]]

---

## Deployment Notes

This workflow's agents are generic. Everything domain-specific — the retrieval stack, the scenario taxonomy, the test-case format, the target system's schema — arrives through injections at deploy time. Filling them is the deploying project's responsibility.

Three pieces of content go into **two agents each**: a producer and the reviewer that checks its work. **The region name is the same in both**, so the pairing is visible from the region name alone rather than having to be looked up.

| Content | Region name | Fill in |
|---|---|---|
| Test case format specification | `[[INJECTION:TestCaseFormatSpecification]]` | `test-case-writer`, `test-case-review` |
| Abstraction / controlled vocabulary | `[[INJECTION:ControlledVocabulary]]` | `test-case-writer`, `test-case-review` |
| Scenario dimension taxonomy | `[[INJECTION:CoverageDimensions]]` | `test-scenario-designer`, `test-scenario-review` |

**Fill each pair with the same content.** Filling one side and not the other fails neither validation nor dispatch — it produces a reviewer that checks conformance against no criteria and reports clean, or a producer working to a narrower taxonomy than its reviewer measures it against. That is the sharpest failure mode this workflow has, and it is silent.

Note that `test-case-writer` deliberately carries **no** `OutputArtifactTemplate`: the test-case format *is* the shape of its output artifact, and a second region for the same thing would be a place for the two to disagree. `test-scenario-designer` keeps its `OutputArtifactTemplate` because `TestScenarios.md` has a structure beyond the dimension taxonomy. In the two reviewers, `OutputArtifactTemplate` governs the shape of their own review reports and nothing else.

**Injections that stand alone** (no partner, but the workflow does not function without them):

| Agent | Region | Content |
|---|---|---|
| `document-research` | `[[INJECTION:IdentityExtension]]`, `[[INJECTION:CodebaseContext]]` | Which retrieval tools exist, what they return, how they misbehave |
| `document-research` | `[[INJECTION:SourceLocatorConventions]]` | What locator the retrieval tooling provides — pages, clauses, chunk ids, or nothing. The citation discipline degrades honestly rather than inventing locators, but only if this states what is available |
| `test-case-export` | `[[INJECTION:TargetSystemSchema]]` | Target sheets, columns, identifier scheme, re-export policy. Not `OutputArtifactTemplate` — that region governs `ExportReport.md`, since the target workbook is a project file rather than an orchestration artifact |
| `approval-presenter` | `[[INJECTION:IdentityExtension]]` | How the user is contacted, how much detail they want up front, domain vocabulary for orienting them. Deployed once and used by both presenter rows |
| `approval-presenter` | `[[INJECTION:ErrorHandlingExtension]]` | Recovery when the user cannot be reached (`E503`), which is this agent's dominant failure mode and entirely deployment-shaped |

`approval-presenter` deliberately carries **no** `OutputArtifactTemplate`. Reshaping its approval record would invite fields the presenter must *derive* — severity, priority, category — and deriving any of them is the evaluation its central constraint forbids. The record keeps a generic numbered-findings shape that the producing agent already knows how to read.

**User-contact tooling follows the HITL column.** The producer agents — `document-research`, `test-scenario-designer`, `test-case-writer` — carry no `user_interaction` tool. The workflow's rule that no agent asks the user a domain question is therefore structural rather than only textual: they cannot ask. Flipping any of those rows to `✅` would produce `BLOCKED`/`E503`, correctly, since the agent has no means to present.

The three review agents keep the tool despite sitting on `❌` rows here. They are generic and are gated `✅` in other workflows, so stripping it would make them unusable elsewhere. In this workflow the flag is simply never set for them.

**The `requirements-review` row has a purpose-built alternative.** The generic agent covers this gate — its remit already includes verifying that acceptance criteria are testable and that research findings are sufficient to proceed, and its codebase-alignment step is simply inert here. Where it proves too general in practice, `requirements-testability-review` is a drop-in built for this one job: swap the subagent name in that row and in `referenced_agents`. It writes the same `requirements-review.md`, so nothing downstream changes.

**Deployment is opt-in.** This workflow reaches a workspace only if named in that deployment's `selections.yaml`, and the same holds for each agent.

**Known tooling gap at time of writing.** The seven agents are written to `AgentTemplateArchitecture.md` v1.3. The validator accepts that schema — `vocabulary.go` carries the seven-slot `CanonicalOrder` and all five conduct regions in `CanonicalDeployed`. But no bundle assembly is wired into the deployment tool, so `ClosingProcedure`, `AuthorityHierarchy`, `ProtocolConstraints`, `ErrorHandlingCommon` and `ExecutionPhilosophyCommon` will deploy **empty**. Per §2.4.1 that is the Conduct tier: the agents still speak the contract and still run, without artifact-access imperatives, the retry rule, or the authority ranking. This affects every agent written to the current schema, not only these.

---

## Design Rationale

### Why there is no ingestion phase

The obvious first design was a preprocessing workflow: ingest the specification into a knowledge base, then derive tests from the KB — mirroring `kb-generation`. It was rejected on three grounds specific to this document class. The source is 300+ pages of dense tables and diagrams where almost every line can be load-bearing, so lossy summarisation into a KB tier is not an acceptable transformation. The document set is not fixed in size or membership. And it changes often enough that a KB would spend most of its life stale.

Runtime retrieval inverts this: nothing is ingested, and the agent pulls exactly what a specific requirement needs, when it needs it. `hw-schema-research` is the existing precedent — it queries a live proprietary format through structured tool calls rather than ingesting it — so this is a paved road rather than new ground.

The cost is that **retrieval quality becomes the workflow's dominant risk, and it lives outside the agents**. Two mitigations are built in: every extracted statement in `Research.md` carries the strongest source locator the retrieval tooling provides, so a human can spot-check provenance; and `requirements-review` runs as an independent gate on whether the retrieved context is actually sufficient, rather than trusting the retriever's own judgement of its work.

### Why `document-research` specialises in process, not in a retrieval tool

Retrieval stacks vary between projects and churn on a timescale of months. Retrieval *discipline* does not churn at all. The agent is therefore generic in the part that matters — query, extract, identify every dangling reference in what was extracted, re-query, and stop only when no unresolved reference remains — and project-specific only through injections that describe what tools exist, what they return, and how they misbehave. The registry precedent holds: `codebase-research` does not specialise in ripgrep versus LSP.

This is what makes the agent reusable across the planned SRS-review workflow and any other document-driven workflow, on three different retrieval stacks, without forking it.

### Why scenarios and test cases are separate artifacts

Domain practice goes from requirement to written test cases in one step, without an intermediate scenario model. The split is deliberately not a transcription of how humans work.

Enumerating a scenario space — the combinatorial product of module variant, channel, parameterisation, fault mode and operating state — is open-ended reasoning about coverage. Rendering a scenario into a strict, tool-bound output format is closed, mechanical compliance. Merging them produces the characteristic failure: output that is perfectly format-conformant with a coverage hole nobody can see, because the only artifact that exists is a list of tests that were written, and a test that was never conceived leaves no trace in it. The separate `TestScenarios.md` gives the coverage argument something to be argued against, and gives the traceability chain a middle link.

The split is expected to be challenged during real use, and dropping `test-scenario-review` is the first experiment to run.

### Why human approval sits on a presenter row rather than on a reviewer

The protocol defines HITL as an approval gate on finished output. The library's existing convention of marking *creators* `✅` inverts that: the human does first-pass review of work no agent has checked, and the review agent then audits the human's judgement. This workflow never adopted that inversion.

Version 1.0 moved the gate to the reviewers instead, which fixed the ordering but left two problems. The reviewer's gate fires on *every* invocation, including rounds whose findings guarantee rework and where there is nothing yet to approve. And more seriously, at a reviewer's gate the human is approving the **creator's** artifact — `TestScenarios.md`, `TestCases.md` — which is not among the reviewer's output artifacts. Per the protocol an agent stamps `human_approved` only on its own outputs, so the approved artifact would stay `human_approved: false` permanently and the orchestrator's stamp check could never see that a human had signed it off.

Version 1.1 uses a dedicated `approval-presenter` row per convergence loop, per the architect's decision in `OnSuccessHITL.md` §6/§7. Three properties make it the right mechanism rather than a workaround:

**Convergence is expressed by table position.** The presenter row is reachable only via `On Success` from its reviewer. Routing already encodes "the agents agree" — so "fire the gate only at convergence" needs no new HITL value, no policy field, no change to the additive `row.hitl OR stage_hitl` merge, and no engine change.

**It closes the provenance gap.** The approved artifact appears in the presenter's `output_artifacts`, so the presenter stamps `human_approved: true` on the artifact the human actually approved. Nothing else in the design achieves this.

**It cannot contradict itself.** A reviewer re-dispatched to present would re-review, because that is what its instructions say to do — and a second pass that finds something new returns `COMPLETED_NEEDS_ACTION`, contradicting the `SUCCESS` that triggered it. A presenter performs no analysis, so it has nothing to disagree with. The absence of analysis is the feature.

**The rework loop closes itself.** On objection the presenter returns `COMPLETED_NEEDS_ACTION`, `On Findings` routes to the creator, and the creator's rewrite resets `human_approved` to `false` by the protocol's own rule. Review then re-runs automatically and the loop re-converges back to the presenter. The human is consulted once per convergence, not once per round.

The cost is one extra row per loop, and that cost is the thing to watch: if it becomes noisy across the library, `OnSuccessHITL.md` §5.3 (a separate `Gate` column with a `gate_on` invocation field) is the parked alternative.

### Why the reviewers are not human gates

All three review rows are `❌`. Reviews run to convergence automatically, and the human sees only the settled result. `requirements-review` in particular is `❌` deliberately and has no presenter: the artifact under review there is the retrieved dossier, which is large, dense, and genuinely hard for a human to check by reading. A retrieval gap surfaces more usefully one step later, as a missing dimension in the scenario space, where the human is looking at a structured model rather than pages of extracted specification text.

### Why no agent may ask the user a domain question

In a safety context an agent's plausible guess is the most dangerous possible output, because it is indistinguishable from knowledge and it arrives inside an artifact that asserts coverage. Routing every information gap back to retrieval — rather than to a human who may answer from memory, or to the agent's own priors — means every statement in the final test cases is traceable to a document. Where the document genuinely does not answer it, that is a finding about the specification, which is exactly what `requirements-review` exists to surface.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-05 | MOSAIC | Initial version |
| 1.1 | 2026-08-06 | MOSAIC | Human approval moved from the review rows to dedicated `approval-presenter` rows, per the architect decision in `OnSuccessHITL.md` §7. Reviews now converge automatically (`❌`); the presenter is reachable only on convergence and stamps `human_approved` on the artifact the human actually approved, closing the §4.5 provenance gap. `requirements-review` gate becomes `❌` with no presenter. |
| 1.2 | 2026-08-06 | MOSAIC | Terminology only, no routing or gate change. The provenance stamp field named throughout this document was renamed `hitl_confirmed` → `human_approved` in Communication Protocol v1.10; every reference here follows, including the two historical entries below, so the document carries one spelling of one field. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- **Staged scale-up.** Planner after `requirements-review` splitting a requirement set into stages; DESIGN/EXECUTION/REVIEW rows become staged with `Stage-{StageNumber}/` artifacts. Deliberately deferred until the single-requirement path is proven.
- **Shared domain model.** At multi-requirement scale the scenario taxonomy (module variants, channel semantics, fault classes) is cross-cutting rather than per-requirement, and rebuilding it per stage is both wasteful and a consistency risk. Either a run-once phase producing a shared taxonomy artifact, or freeze it into the scenario designer's injection.
- **Dropping `test-scenario-review`.** The first simplification to test once the full path works.
- **A third `approval-presenter` row after `requirements-review`**, if running the workflow shows that retrieval gaps need catching before scenario design rather than surfacing as missing dimensions afterwards. Omitted in 1.1 because the dossier is large and dense and a human reading it is unlikely to spot what is absent.
- **Watch the row cost.** One presenter row per convergence loop is cheap here at two rows; across the library it may not be. `OnSuccessHITL.md` §5.3 — a separate `Gate` column with a `gate_on` invocation field — is the parked alternative if it becomes noisy.
- **Coverage counter-check.** An agent that queries retrieval for what *should* have been found and compares against `TestScenarios.md` — defends against a retrieval miss that every downstream agent then inherits silently. Currently mitigated only by `requirements-review`.

**Dead ends (tried and rejected):**
- **KB ingestion of the specification** — rejected; see Design Rationale.
- **A `TestProfile.md` input artifact** carrying the output format, glossary and gold examples. Rejected: the agent template architecture already solves this with `[[INJECTION:OutputArtifactTemplate]]` and `[[INJECTION:IdentityExtension]]`, which bind at deploy rather than per run. Format is stable; making it a per-run artifact would be paying run-time cost for deploy-time data.
- **A third HITL column value (`✅ˢ`, on-success)** firing the gate only on the converging pass, implemented by re-dispatching the reviewer. Rejected by architect review — see `OnSuccessHITL.md` §4. It encodes a *policy* into a flag that carries an *amount*, which breaks the additive `row.hitl OR stage_hitl` merge that stage HITL depends on; it needs `already_presented` state the orchestration artifact has nowhere to store; and the re-dispatched reviewer would re-review and could contradict its own prior `SUCCESS`. Superseded by the presenter row in 1.1.
- **Reviewer-held approval gates (workflow v1.0).** Correct in ordering, but the human approved the *creator's* artifact while only the reviewer could stamp provenance — so the approved artifact stayed `human_approved: false` permanently. Superseded by the presenter row in 1.1.
- **Specialising `document-research` per retrieval flavour** (vector / graph / hybrid). Rejected: would produce one agent per stack for a process that is identical across all of them.
