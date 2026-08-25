# AgentTest — Orchestrator Routing Test Scenarios

All tests follow the **oneshot pattern**: seed an `Orchestration.md` with execution log history that places the orchestrator at the decision point, then assert it makes the correct next dispatch (agent choice, input artifacts, HITL flag).

Reference workflow for most scenarios: `brownfield-tdd`.

---

## Scenario Catalog

Each scenario is one verifiable routing condition. Scenarios are atomic — whether they become individual tests or get combined into multi-assertion tests is a separate concern tracked in the [Test-to-Scenario Mapping](#test-to-scenario-mapping).

### HITL

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| H-1 | HITL-enabled agent returns SUCCESS but output artifact has `human_approved: false` | Re-dispatch same agent to complete HITL gate, not advance to next row | Ready |
| H-2 | Plan stage table marks stage HITL=true, workflow table marks agent HITL=false | Dispatch agent with `human_in_the_loop: true` (additive merge) | Ready |
| H-3 | Plan stage HITL=true applies to ALL agents in that stage, not just first | Second agent in same HITL stage also dispatched with `human_in_the_loop: true` | Ready |

### Status Code Routing

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| S-1 | Reviewer returns COMPLETED_NEEDS_ACTION | Route to On Findings target, not On Success | Ready |
| S-2 | Agent returns PARTIALLY_DONE | Re-dispatch same agent type (successor invocation), not advance to next row | Ready |
| S-3 | Agent returns BLOCKED (E101 INPUT_NOT_FOUND) | Tier 1 auto-retry (up to 3 attempts), then Tier 2 alternative strategy | Ready |
| S-4 | Agent returns BLOCKED (E501 TOOL_UNAVAILABLE) | Tier 1 auto-retry (up to 3 attempts), then Tier 2 alternative strategy | Ready |
| S-5 | Agent returns NEEDS_CLARIFICATION | Provide context or escalate to human, not advance | Ready |
| S-6 | Agent returns CAPABILITY_EXCEEDED | Try close alternative or escalate to human | Ready |
| S-7 | Agent returns BLOCKED (E503 USER_CONTACT_UNAVAILABLE) on HITL dispatch | Tier 1 auto-retry, do not skip the HITL gate | Ready |

### Creator/Reviewer Gate

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| G-1 | Reviewer returns COMPLETED_NEEDS_ACTION — route back to creator | Creator receives review findings artifact in input_artifacts | Ready |
| G-2 | Creator fixes and returns SUCCESS after reviewer findings | Route back to reviewer for re-review, not advance past gate | Ready |
| G-3 | On re-review dispatch, should reviewer receive its own previous review artifact? | UNDECIDED — see [Discussion: G-3](#discussion-g-3) | Decision pending |

### Distant Route-Back

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| R-1 | Distant route-back completes (e.g. stage 5 → planner) — quality gate | Dispatch paired reviewer (e.g. plan-review) before re-entering EXECUTION | Ready |
| R-2 | Distant route-back to planner — input artifacts | Provide workflow-table inputs (Research.md, Requirements.md), not just local stage context | Ready |
| R-3 | Distant route-back to contracts-designer — quality gate | Dispatch contracts-review before re-entering EXECUTION | Ready |
| R-4 | Route-back from test-runner (REVIEW) to planner — quality gate | Dispatch plan-review before re-entering EXECUTION | Ready |

### Wildcard & Artifact Path Resolution

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| W-1 | Workflow table says `Stage-*/Plan.md` — dispatch to contracts-designer | Expand to all existing Stage-{N}/Plan.md (e.g. 4 stages → 4 paths) | Ready |
| W-2 | Workflow table says `Stage-*/Plan.md` AND `Stage-*/PlanProgress.md` — dispatch to plan-review | Both wildcards fully expanded | Ready |
| W-3 | Wildcard expansion after route-back that changed stage count | Expansion picks up newly added stages, not stale count | Ready |

### Execution Groups & Stage Progression

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| E-1 | EXECUTION phase entry — first stage, TDD approach | Dispatch test-writer-tdd (correct first agent per approach) | Ready |
| E-2 | Stage with Approach = "Implementation-Only" | Skip test-writer-tdd/tests-review-tdd, dispatch implementation-tdd directly | Ready |
| E-3 | Stage with Approach = "Implementation-First" | Dispatch implementation-tdd before test-writer-tdd (reversed from TDD) | Ready |
| E-4 | Stage with Approach = "Tests-Only" | Dispatch test-writer-tdd/tests-review-tdd only, skip implementation agents | Ready |

### Parallel Dispatch

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| P-1 | Multiple independent stages (no Depends On) after prior stage completes | Dispatch first-group agents for all eligible stages, not just one | Blocked (AgentTest capability TBD) |
| P-2 | Stage completion unblocks a dependent stage (Depends On all met) | Dispatch first agent for newly-eligible stage | Blocked (AgentTest capability TBD) |
| P-3 | Workflow fork — On Success names multiple agents | Dispatch all named agents | Blocked (AgentTest capability TBD) |

### Infrastructure Agent Triggers

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| I-1 | STAGE_END trigger — checkpoint agent at stage boundary | Dispatch checkpoint agent before next stage's first workflow agent | Tested |
| I-2 | INVOCATION_INTERVAL trigger — review agent after N invocations | Dispatch review agent (threshold from agent's last firing, not modulus) | Tested (2 tests: precise + overdue) |
| I-3 | Multiple triggers fire on same boundary (checkpoint + commit both STAGE_END) | Dispatch ALL fired agents in declaration order, each with own Seq | Tested |
| I-4 | Gated agent — checkpoint trigger exists but `checkpoints: disabled` | Do NOT dispatch checkpoint agent | Tested |
| I-5 | PHASE_END trigger | Dispatch infrastructure agent at phase transition | Tested |
| I-6 | Restore-class agent — never fires automatically regardless of declared triggers | Do NOT dispatch restore agent even if trigger condition is met | Tested |

### Optional Phases

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| O-1 | Contracts phase skip — if one is skipped, both must be skipped | Never contracts-designer without contracts-review or vice versa | Decision pending |

### State Recovery

| ID | Scenario | Expected | Status |
|----|----------|----------|--------|
| X-1 | Resume when last Execution Log row is an infrastructure agent | Route from last *workflow* agent row, not the infrastructure row | Ready (low priority) |

---

## Test-to-Scenario Mapping

Filled in when tests are created. One test may cover multiple scenarios; one scenario may appear in multiple tests (different variants/workflows).

| Test ID | Test Name | Scenarios Covered | Workflow | Notes |
|---------|-----------|-------------------|----------|-------|
| findings-route-back | Findings Route-Back | S-1, G-1 | brownfield-tdd | Seeded after planner SUCCESS. plan-review COMPLETED_NEEDS_ACTION → planner-tdd-soft with plan-review.md in inputs |
| partially-done-redispatch | PARTIALLY_DONE Redispatch | S-2 | brownfield-tdd | Seeded at EXECUTION stage 1 impl. implementation-tdd PARTIALLY_DONE → same agent re-dispatched |
| creator-fix-rereview | Creator Fix Re-review | G-2 | brownfield-tdd | Seeded after planner fix (plan-review CNA → planner SUCCESS). Must re-review, not advance to contracts-designer |
| hitl-redispatch-unapproved | HITL Re-dispatch Unapproved | H-1 | brownfield-tdd | Seeded after requirements-refinement SUCCESS with human_approved: false. Must re-dispatch for HITL gate, not advance |
| impl-only-skip-tests | Impl-Only Skip Tests | E-2 | brownfield-tdd | Stage 1 Approach=Implementation-Only. Must skip test-writer-tdd/tests-review-tdd, dispatch implementation-tdd directly |
| hitl-plan-stage-override | HITL Plan Stage Override | H-2, E-1 | brownfield-tdd | Plan stage 1 HITL=Yes overrides workflow HITL=false. test-writer-tdd dispatched with human_in_the_loop: true. Also proves E-1 (TDD first agent) |
| hitl-plan-stage-all-agents | HITL Plan Stage All Agents | H-3 | brownfield-tdd | Stage 1 HITL=Yes, test-writer-tdd already done. tests-review-tdd must also get human_in_the_loop: true |
| blocked-e101-retry | BLOCKED E101 Retry | S-3 | brownfield-tdd | implementation-tdd BLOCKED E101 → Tier 1 auto-retry, same agent re-dispatched |
| blocked-e501-retry | BLOCKED E501 Retry | S-4 | brownfield-tdd | implementation-tdd BLOCKED E501 → Tier 1 auto-retry, same agent re-dispatched |
| needs-clarification-no-advance | NEEDS_CLARIFICATION No Advance | S-5 | brownfield-tdd | implementation-tdd NEEDS_CLARIFICATION → route back to planner-tdd-soft for clarification, not advance |
| blocked-e503-hitl-retry | BLOCKED E503 HITL Retry | S-7 | brownfield-tdd | requirements-refinement BLOCKED E503 on HITL dispatch → retry with human_in_the_loop still true |
| planner-routeback-quality-gate | Planner Route-Back Quality Gate | R-1, R-2, R-4 | brownfield-tdd | test-runner CNA → planner (with Research.md, Requirements.md) → plan-review quality gate. Plan-review gets updated Plan.md + all stage plans |
| contracts-routeback-quality-gate | Contracts Route-Back Quality Gate | R-3 | brownfield-tdd | implementation-review CNA routes to contracts-designer → contracts-review quality gate before re-entering EXECUTION |
| wildcard-input-expansion | Wildcard Input Expansion | W-1 | brownfield-tdd | contracts-designer dispatch after plan-review SUCCESS. Stage-*/Plan.md must expand to Stage-1/Plan.md + Stage-2/Plan.md |
| capability-exceeded-escalate | CAPABILITY_EXCEEDED Escalate | S-6 | brownfield-tdd | implementation-tdd CAPABILITY_EXCEEDED → zero dispatches expected. Orchestrator must escalate to human, not retry or advance |
| impl-first-reorder | Impl-First Reorder | E-3 | brownfield-tdd | Stage 1 Approach=Implementation-First. Must dispatch implementation-tdd first, not test-writer-tdd |
| tests-only-skip-impl | Tests-Only Skip Impl | E-4 | brownfield-tdd | Stage 1 Approach=Tests-Only. Must dispatch test-writer-tdd, skip implementation agents entirely |
| wildcard-dual-expansion | Wildcard Dual Expansion | W-2 | brownfield-tdd | plan-review dispatch after planner SUCCESS. Both Stage-*/Plan.md and Stage-*/PlanProgress.md must expand to all stage files |
| wildcard-after-routeback | Wildcard After Route-Back | W-3 | brownfield-tdd | After route-back adding Stage-3, plan-review wildcards must expand to all 3 stages, not stale 2-stage count |
| stage-end-checkpoint | STAGE_END Checkpoint | I-1 | brownfield-tdd | implementation-review SUCCESS completes stage 1 → checkpoint-manager-git fires before stage 2 |
| interval-precise-boundary | Interval Precise Boundary | I-2 | brownfield-tdd | implementation-tdd SUCCESS at global_sequence=10, interval=10 → review fires exactly at threshold |
| interval-overdue | Interval Overdue | I-2 | brownfield-tdd | implementation-tdd SUCCESS at global_sequence=10, interval=3 → review fires when obviously past threshold |
| multiple-triggers-same-boundary | Multiple Triggers Same Boundary | I-3 | brownfield-tdd | implementation-review SUCCESS completes stage 1 → checkpoint + commit both fire in declaration order |
| gated-checkpoint-disabled | Gated Checkpoint Disabled | I-4 | brownfield-tdd | STAGE_END met but checkpoints: disabled → checkpoint skipped, test-writer-tdd dispatched directly |
| phase-end-trigger | PHASE_END Trigger | I-5 | brownfield-tdd | requirements-review SUCCESS completes RESEARCH → infra-phase-end fires before planner-tdd-soft |
| restore-class-exclusion | Restore Class Exclusion | I-6 | brownfield-tdd | STAGE_END met, restore-class agent with STAGE_END trigger → NOT dispatched, test-writer-tdd dispatched instead |

---

## Scenario Details

Detailed context for each scenario — failure modes observed, setup specifics, variant ideas. The catalog table above is the canonical list; this section provides the depth needed to actually build the tests.

### H-1: HITL Re-dispatch on `human_approved: false`

**Setup:** Orchestration log shows e.g. `requirements-refinement` completed with SUCCESS. Its output artifact (`Requirements.md`) exists with `human_approved: false`. Orchestrator resumes.

**Failure mode observed:** Orchestrator treats SUCCESS as sufficient, ignores the `human_approved` stamp, and advances to the next row.

**Variants:** Test with `requirements-refinement` (RESEARCH), `planner-tdd-soft` (PLANNING), `contracts-designer` (DESIGN).

### H-2 / H-3: HITL Additive Merge From Plan Stage Table

**Setup:** EXECUTION stage 3 in progress. Plan.md stage table marks stage 3 with HITL=true. Workflow table has `test-writer-tdd` as HITL ❌.

**Failure mode anticipated:** Orchestrator only checks the workflow table's HITL column, ignores Plan's per-stage HITL. Subtle — requires reading Plan for HITL resolution, not just stage ordering.

**H-3 specifically:** Same stage, second agent (e.g. `tests-review-tdd`). Verify stage HITL applies to all agents, not just the first dispatched.

### S-1: COMPLETED_NEEDS_ACTION Misrouted as SUCCESS

**Setup:** `plan-review` completed with `COMPLETED_NEEDS_ACTION`. Workflow On Findings → `planner-tdd-soft`.

**Failure mode:** Orchestrator treats COMPLETED_NEEDS_ACTION as success variant and advances to `contracts-designer`.

**Note:** Most basic status-code routing test. Will likely combine with G-1 (review artifact in input) and/or G-2 (full loop). Listed separately as regression baseline.

### S-2: PARTIALLY_DONE — Re-dispatch Same Agent Type

**Setup:** A workflow agent (e.g. `implementation-tdd` during EXECUTION) returns `PARTIALLY_DONE` — some work items completed, more of the same work needed.

**Expected:** Orchestrator dispatches a successor invocation of the same agent type (new sequence number, same agent name). Does not advance to the next workflow row, does not treat it as failure.

**Failure mode anticipated:** Orchestrator treats PARTIALLY_DONE as SUCCESS and advances to `implementation-review`, leaving work incomplete. Or treats it as an error and enters error handling instead of a clean re-dispatch.

**Protocol definition:** "Route to successor agent (same type)" — this is a continuation, not a retry. The agent is expected to pick up where it left off using its progress artifact.

**Variants:** Test during EXECUTION (most realistic — large stage where implementation-tdd can't finish in one invocation) and during RESEARCH (codebase-research with a large codebase).

### S-3 / S-4: BLOCKED — Tiered Error Handling

**Setup (S-3):** Agent returns BLOCKED with `error_code: E101` (INPUT_NOT_FOUND). E.g. `test-writer-tdd` can't find its input artifact.

**Setup (S-4):** Agent returns BLOCKED with `error_code: E501` (TOOL_UNAVAILABLE). E.g. build tool or test runner unavailable.

**Expected:** Orchestrator applies tiered error handling: Tier 1 auto-retry (up to 3 attempts with exponential backoff), then Tier 2 alternative strategy (reduce scope, skip optional phase), then Tier 3 human escalation.

**Failure mode anticipated:** Orchestrator skips retry and escalates immediately, or retries indefinitely without progressing through tiers, or treats BLOCKED as a terminal failure and stops the run without escalation.

**Note:** Testing the full 3-tier cascade may require `stop_after_invocations: 4+` (initial + 2 retries + escalation). Alternatively, test just the first routing decision (does it retry or not?).

### S-5: NEEDS_CLARIFICATION

**Setup:** Agent returns `NEEDS_CLARIFICATION` — uncertain or context incomplete.

**Expected:** Orchestrator provides additional context or escalates to human for clarification. Does not advance, does not treat as failure.

**Failure mode anticipated:** Orchestrator advances to next row ignoring the clarification need, or enters error handling instead of the clarification path.

### S-6: CAPABILITY_EXCEEDED

**Setup:** Agent returns `CAPABILITY_EXCEEDED` — task beyond agent's ability.

**Expected:** Orchestrator tries a close alternative strategy or escalates to human. Does not retry the same agent with the same inputs (unlike BLOCKED).

**Failure mode anticipated:** Orchestrator retries the same agent (pointless — it already said it can't do this) or advances as if successful.

### S-7: BLOCKED E503 on HITL Dispatch

**Setup:** Agent dispatched with `human_in_the_loop: true` returns BLOCKED with `error_code: E503` (USER_CONTACT_UNAVAILABLE — no user interaction tools available).

**Expected:** Orchestrator retries (Tier 1) — the tool availability may be transient. Must NOT skip the HITL gate or advance without human approval.

**Failure mode anticipated:** Orchestrator treats E503 as "HITL not possible, proceed without it" — effectively bypassing the human oversight gate.

### G-1: Review Artifact Passed to Creator on Findings Route-Back

**Setup:** `plan-review` completed COMPLETED_NEEDS_ACTION, `plan-review.md` exists. Orchestrator routes back to `planner-tdd-soft`.

**Failure mode observed:** Routes back correctly but omits `plan-review.md` from input_artifacts — creator has no visibility into findings.

**Variants:** All five creator/reviewer pairs in brownfield-tdd (requirements, plan, contracts, tests within stage, implementation within stage).

### G-2: Creator SUCCESS Does Not Pass the Gate

**Setup:** Execution log shows: (1) planner SUCCESS, (2) plan-review COMPLETED_NEEDS_ACTION, (3) planner SUCCESS (fix). Orchestrator resumes.

**Failure mode observed:** Treats creator's second SUCCESS as gate-passed, advances to contracts-designer.

**Orchestrator instructions explicit:** "You cannot advance past a creator/reviewer pair without the reviewer returning SUCCESS last."

**Variants:** All five pairs. Consider combining with G-3 assertion (re-review input artifact) once decided.

### Discussion: G-3

**Question:** On re-review dispatch (reviewer re-invoked after creator fix), should the reviewer receive its own previous review artifact as input?

**For:** Reviewer can verify each flagged finding was addressed, reduces rubber-stamping risk, audit trail of raised-vs-fixed.

**Against:** Fresh evaluation on merits, previous review may anchor on stale findings, workflow table Input column doesn't list it.

**Recommendation:** Include it — re-review's job is specifically "verify these fixes," not "review from scratch." Same principle as G-1 (review findings to creator). If decided, becomes a `task_messages` assertion on the G-2 test.

**Status:** Awaiting decision.

### R-1 / R-3 / R-4: Quality Gate After Distant Route-Back

**Setup (R-1):** Normal flow through PLANNING, DESIGN, EXECUTION stages 1-4. Stage 5's `implementation-review` routes back to `planner-tdd-soft`. Planner completes SUCCESS.

**Failure mode observed:** Orchestrator sees route-back as "minor fix," skips review gate, jumps straight back to EXECUTION. Worse when change seems small or route-back is distant.

**R-3:** Same pattern but route-back to `contracts-designer` → must hit `contracts-review`.
**R-4:** Route-back from `test-runner` (REVIEW phase) to `planner-tdd-soft` → must hit `plan-review`.

### R-2: Missing Input Artifacts on Distant Route-Back

**Setup:** Stage 5's `implementation-review` routes back to `planner-tdd-soft`. Workflow table says planner inputs: `Research.md, Requirements.md`.

**Failure mode observed:** Orchestrator provides only stage 5 plan and progress, omitting `Requirements.md` and `Research.md` — treating route-back as if planner only needs local context.

### W-1 / W-2 / W-3: Stage-* Wildcard Expansion

**Affected workflow rows (brownfield-tdd):**
- `plan-review` inputs: `Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md`
- `contracts-designer` inputs: `Research.md, Requirements.md, Plan.md, Stage-*/Plan.md`
- `contracts-review` inputs: `Plan.md, Stage-*/Plan.md, ContractsDesign.md`

**W-1 failure mode observed:** Orchestrator passes `Plan.md` but omits per-stage plans, or passes literal `Stage-*/Plan.md` unexpanded, or only some stages.

**W-3:** After route-back that adds new stages, expansion must use current state, not cached earlier count.

### E-1 through E-4: Execution Group Selection

**Context:** Plan stage table has Approach column. Orchestrator must read it and dispatch the correct group sequence. The brownfield-tdd Execution Groups table defines the mapping.

**E-2 failure mode anticipated:** Orchestrator ignores Approach column, defaults to full TDD sequence for every stage.

### P-1 / P-2 / P-3: Parallel Dispatch

**Orchestrator instructions explicit:** "Dispatch all eligible targets before waiting on any one of them" and "none is skipped in favor of another."

**P-1 failure mode observed:** Stages treated as strictly sequential even when plan allows parallelism.

**P-3 note:** brownfield-tdd has no explicit On Success forks — may need custom test workflow.

**Blocked:** AgentTest parallel dispatch assertion support TBD.

### I-1 through I-6: Infrastructure Agent Triggers

**Key rules:** Triggers evaluated after Orchestration.md update, before next workflow dispatch. All fired agents run (not a selection). One agent fires at most once per evaluation. No cascades. Gated classes skip when switch disabled. Restore always skipped.

**I-2 detail:** Threshold, not modulus. Count from agent's last firing row in log, not from run start. If never fired, count from sequence 0.

**I-3 detail:** Stage boundary with checkpoint (STAGE_END) + commit (STAGE_END) both declared and enabled. Must produce two dispatches, two sequence numbers, two log rows.

**I-5 note:** No current concrete agent uses PHASE_END. May need custom test infrastructure agent.

### O-1: Optional Contracts Phase (DECISION PENDING)

**Open design question:** Skip decision requires domain judgment orchestrator can't make (can't read artifacts per context window protection). Considering always-invoke: contracts-designer returns "Nothing to do" if unneeded.

**Current failure mode:** Orchestrator skips design phase when it shouldn't — guesses from task description.

**If always-invoke is adopted:** Scenario collapses to simple "dispatch contracts-designer after plan-review SUCCESS" with no skip logic.

### X-1: Recovery Past Infrastructure Agent Log Rows

**Setup:** Last rows: `implementation-tdd#14 SUCCESS`, `checkpoint-manager-git#15 SUCCESS`. Resume.

**Failure mode anticipated:** Sees checkpoint row as last, doesn't know what to dispatch. Or treats checkpoint row as stage boundary "handled" and skips to next stage.

**Priority:** Low — only on resume after crash between infrastructure dispatch and next workflow dispatch.

---

## General Notes

- All tests use the oneshot pattern with seeded `Orchestration.md`
- Primary workflow: `brownfield-tdd` (most scenarios observed there)
- Some scenarios (P-3, I-5) may need custom test workflows
- Infrastructure agent scenarios (I-*) need test workflows with `<InfrastructureAgents>` declarations and corresponding stub agent definitions
- Each test needs: seed Orchestration.md, seed artifacts with correct provenance frontmatter, stub registry for expected dispatch(es), assertions on agent identity + input artifacts + HITL flag
- `stop_after_invocations: 1` for most (one dispatch decision per test)
- Infrastructure agent tests may need `stop_after_invocations: 2+` to capture infra dispatch + next workflow dispatch
- Parallel dispatch tests need `stop_after_invocations: 2+` and multi-dispatch assertions
- Tests are expensive to run — combine scenarios into single tests where the same seeded state can verify multiple conditions (e.g. S-1 + G-1 in one test: reviewer COMPLETED_NEEDS_ACTION routes to creator WITH review artifact)
