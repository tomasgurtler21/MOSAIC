# Workflow Patterns

Reusable structural patterns observed across the workflow catalog. Each pattern is documented with its intent, mechanism, where it appears, and known trade-offs. The goal is to make implicit design knowledge explicit — so new workflows reuse proven structures rather than reinvent them, and so pattern-level lessons learned propagate.

This is a living reference. Patterns earn their place here by appearing in at least one workflow with enough design rationale to explain *why* they work, not just *that* they exist.

---

## 1. Creator-Reviewer Convergence Loop

**Intent:** Ensure artifact quality through automated review before any human or downstream consumer sees it.

**Mechanism:** A creator agent produces an artifact. A reviewer agent checks it. If findings exist, `On Findings` routes back to the creator. The loop repeats until the reviewer returns `SUCCESS` (no findings), at which point `On Success` advances the workflow.

```
creator → reviewer
            ├─ On Success → next step
            └─ On Findings → creator (re-draft)
```

**Where it appears:** Nearly every workflow in the catalog. Core instances:
- `requirements-refinement` / `requirements-review` (Build, Design, Verification workflows)
- `planner-tdd-soft` / `plan-review` (all plan-driven workflows)
- `contracts-designer` / `contracts-review` (Build, Design workflows)
- `test-scenario-designer` / `test-scenario-review` (`requirements-to-test-cases`)
- `test-writer-tdd` / `tests-review-tdd` (TDD Build workflows)
- `implementation-tdd` / `implementation-review` (all Build workflows)
- `comparison-analyst` / `comparison-review` (`product-comparison`)

**Model capability dynamics:** The creator-reviewer loop has an interesting property when the two roles run on models of different capability levels or different model families.

- **Stronger reviewer than creator:** More findings per round, more loops before convergence. The reviewer catches things the creator doesn't know to avoid. Convergence cost rises but final quality is higher.
- **Stronger creator than reviewer:** Fewer loops, but the reviewer still finds *relative* issues — things the creator missed or got wrong from its own perspective, even if the reviewer's ceiling is lower. A weaker reviewer is not a no-op reviewer.
- **Cross-family pairing** (e.g., different model providers for creator vs. reviewer): The main benefit is angle diversity — different model families have different blind spots, so cross-family pairing catches a broader class of issues than same-family pairing regardless of which side is "stronger."

Which direction is better — strong creator or strong reviewer — is situation-dependent and unresolved. Both configurations produce usable results. The clearest practical lesson is: use different model families for creator and reviewer when possible, to get genuine diversity of perspective rather than the same blind spots on both sides.

**Trade-offs:**
- Loop convergence is not guaranteed — a weak creator or an overly strict reviewer can cycle indefinitely. The orchestrator's retry budget is the backstop.
- The reviewer must be scoped to catch issues the creator can actually fix. Findings about upstream artifacts (e.g., a requirements gap found during plan review) should route to the upstream creator, not loop locally.

**Relationship to other patterns:** Often combined with the Convergence-Gated Human Approval pattern (Pattern 2) to add a human gate that fires only after the loop converges, rather than on every draft.

---

## 2. Convergence-Gated Human Approval (Presenter Pattern)

**Intent:** Gate human approval on the *converged* artifact — the version both creator and reviewer agree on — rather than on every intermediate draft.

**Mechanism:** A dedicated `approval-presenter` row sits downstream of the reviewer's `On Success`. It performs no analysis. Its sole job is to present the converged artifact to the human and stamp `human_approved`. On objection, `On Findings` routes to the creator, resetting the loop.

```
creator → reviewer
            └─ On Success → approval-presenter (HITL: TRUE)
                              ├─ On Success → next step
                              └─ On Findings → creator (re-draft)
```

**Where it appears:**
- `requirements-to-test-cases` — two instances: `approval-presenter(scenarios)` after test-scenario-review, `approval-presenter(cases)` after test-case-review

**Why it exists (vs. the older convention):** The catalog's older convention marks the *creator* row `TRUE`, meaning a human reviews every draft — including drafts the reviewer would have flagged anyway. This wastes human attention on unconverged work. Moving the gate to the *reviewer* row doesn't work either: the reviewer's gate fires on every invocation (including rework rounds), and the human approves the creator's artifact while only the reviewer can stamp provenance, leaving `human_approved: false` permanently.

The presenter pattern solves both problems: it fires once per convergence, and the approved artifact is in the presenter's own output list so provenance stamping works correctly.

**Trade-offs:**
- One extra row per convergence loop. Acceptable at small scale; if it becomes noisy across the catalog, the parked alternative is a separate `Gate` column with a `gate_on` invocation field (see `requirements-to-test-cases` Open Ideas).
- The presenter must perform zero analysis — if it evaluates, it can contradict the reviewer's `SUCCESS` and break the convergence semantics.

**Status:** Proven in design, pending real-world validation. Multiple workflows (`brownfield-tdd`, `greenfield-tdd`, `brownfield-design`, `brownfield-tdd-build-verified`) have noted this pattern as a planned backport once it accumulates enough field use.

---

## 3. Decompose-Then-Execute

**Intent:** Break a task too large for a single agent invocation into isolated units, execute each unit independently, then optionally consolidate results.

**The core design decision:** When does the decomposition become known — before execution starts, or during it?

This splits into two variants that share the same structural shape (progress artifact tracks units, execution loops over them, artifacts are isolated per unit) but differ in how the unit list is produced.

### Variant A: Predetermined Decomposition (Plan-Driven)

**Mechanism:** A planner agent analyzes requirements upfront and produces `Plan.md` plus per-stage `Stage-{StageNumber}/Plan.md` and `Stage-{StageNumber}/PlanProgress.md`. The full list of stages is known before execution begins. The EXECUTION phase loops over stages. Within each stage, a sequence of agents (determined by the Execution Groups table) operates on stage-scoped artifacts.

```
planner → plan-review → [per stage: execution group sequence] → final review
```

**Execution Groups:** A compact table mapping approach names to ordered groups of subagents within each stage:

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |

The planner sets each stage's approach; the orchestrator reads the Execution Groups table to determine which groups to run and in what order.

**Where it appears:**
- `brownfield-tdd`, `greenfield-tdd`, `brownfield-tdd-build-verified` — full TDD staged execution
- `brownfield-pr-fix` — staged execution with an additional Response group per stage
- `implementation-only` — staged execution without Execution Groups (fixed sequence)
- `brownfield-pr-audit` — staged execution with 4 audit types per stage

**When to use:** When the scope is well-understood enough that a planner can enumerate all stages before work begins. Feature development against a researched codebase is the canonical case.

**Trade-offs:**
- Works best at a specific feature granularity. Too small: the planner over-engineers stages for trivial work, wasting cost on correction round-trips. Too big: plan stays too vague to pin down real scope, execution agents inherit under-specified direction.
- Stage isolation means cross-stage dependencies must be managed through the progress artifact or the plan — agents within one stage cannot see another stage's artifacts unless explicitly provided.

### Variant B: Emergent Decomposition (Discovered During Execution)

**Mechanism:** No separate planner. The first execution stage analyzes the full scope and produces a progress artifact that defines subsequent stages. Later stages may add even deeper stages during their own execution, making the total work dynamic rather than predetermined.

```
Stage 1 (analyzes scope) → creates progress artifact with N stages
    ↓
Stage 2..N (parallel within tier) → may add deeper stages
    ↓
(continues until no new stages are added)
```

**Where it appears:**
- `kb-generation` — Tier 1 KB generator analyzes the codebase and creates `KBProgress.md` with all generation stages; deeper tiers may add more
- `hw-schema-kb-generation` — similar: first research stage creates `HWResearchProgress.md`; first KB generation stage creates `KBProgress.md`
- `kb-correction` — generator reads existing KB and creates correction stages dynamically

**When to use:** When the scope cannot be enumerated upfront because the structure of the work emerges from analyzing the subject matter. Documentation generation is the canonical case — you don't know how many subsystems need documenting until you've looked at the codebase.

**Related concept: Progress Artifact Bootstrap.** The progress artifact (`KBProgress.md`, `HWResearchProgress.md`) doesn't exist as a prerequisite — the first execution run creates it. This is a deliberate design choice: the structure of the work emerges from analyzing the scope, not from a separate planning step.

**Trade-offs:**
- Total work isn't known upfront. The orchestrator must handle stages being added mid-execution.
- Ordering constraints may apply within the emergent structure (e.g., correction stages run bottom-up by tier to avoid cascading invalidation).

---

## 4. Multi-Pass Research

**Intent:** Build research context incrementally, with each pass focused on a specific domain or concern, rather than attempting a single monolithic research pass.

**Mechanism:** The same research agent is invoked multiple times with different focus descriptions. Parenthetical suffixes in the workflow table disambiguate the rows. Each pass reads prior research as input and produces a focused output artifact.

```
codebase-research → codebase-research(architecture) → codebase-research(contracts)
     Research.md        ResearchArchitecture.md           ResearchContracts.md
```

**Where it appears:**
- `brownfield-pr-audit` — general research, then architecture-focused, then contracts-focused
- `brownfield-system-audit` — same three-pass pattern

**Why not one deep research pass:** A monolithic pass that tries to cover general context, architecture patterns, and contract details in a single invocation produces shallow results across all dimensions. Sequential focused passes let each invocation go deep on one concern while building on the context established by prior passes.

**Trade-offs:**
- Each additional pass costs a full agent invocation. The incremental benefit must justify the cost — a two-pass workflow (general + one focus) is often sufficient; three passes is the observed maximum.
- The order matters: later passes depend on earlier research artifacts as input. Reordering passes changes what context the focused research has available.

---

## 5. Fan-Out / Reconsolidation

**Intent:** Execute independent parallel work tracks, then merge their outputs into a unified result.

**Mechanism:** Multiple agent instances dispatch in parallel (via the `Waits For` column or staged dispatch). A downstream merger/synthesis agent waits for all tracks to complete, then consolidates their partial outputs into a single artifact.

```
                ┌─ track-A → ...
dispatcher ─────┼─ track-B → ...  ──→ merger (Waits For: all tracks)
                └─ track-C → ...
```

**Where it appears:**
- `brownfield-pr-audit` — 4 audit types fan out, each producing partial PR response artifacts; `audit-response-merger` consolidates all into final `PullRequestResponses.md`
- `product-comparison` — per-product research fans out across 9 dimensions; `topic-analyst` synthesizes per-dimension; `comparison-analyst` synthesizes across all dimensions
- `kb-generation` — per-tier KB generation stages fan out in parallel; `knowledge-base-flag-sorter` consolidates all flags

**Trade-offs:**
- The merger agent's context budget is the bottleneck. If partial outputs are large or numerous, manual file-by-file reading overwhelms the merger. This is exactly the problem the Script-Mediated Context Scaling pattern (Pattern 6) solves.
- All tracks must complete before the merger runs. One slow or stuck track blocks reconsolidation for all others.

---

## 6. Script-Mediated Context Scaling via Schema-Conformant Artifacts

**Intent:** Enable a consumer agent to process a volume of structured partial outputs that would overwhelm its context if read manually, by using scripted extraction instead of LLM-driven file reading.

**Mechanism:** Two-part contract:
1. **Producers write structured JSON embedded in markdown, conforming to a schema both sides read from a template artifact.** The producer doesn't invent its own output shape.
2. **The consumer reads exactly one partial file to learn the structure, then writes and executes scripts to parse, extract, group, and merge all remaining files.** LLM reasoning is reserved for genuinely semantic steps (e.g., judging whether two findings describe the same issue).

```
producer-1 ─→ partial-1.md (schema-conformant JSON)
producer-2 ─→ partial-2.md (schema-conformant JSON)     → consumer reads ONE,
producer-N ─→ partial-N.md (schema-conformant JSON)       scripts the rest
```

**Where it appears:**
- `brownfield-pr-audit` — `audit-to-pull-request` instances produce schema-conformant partial response queues; `audit-response-merger` scripts the consolidation

**Critical dependency:** Both producer and consumer must be aware of the artifact format up front. The schema lives in a template artifact both sides read, not implicit in whatever the producer writes. Skipping this — letting producers freelance their output shape — forces the consumer back into per-file manual reading, defeating the pattern.

**Where it should be considered:** Any workflow that fans out to many parallel instances whose partial outputs need reconsolidation, and where the volume of partials would exceed a single agent's context budget.

**Trade-offs:**
- Requires the artifact schema to be stable and shared. Schema changes must propagate to all producers and consumers simultaneously.
- The consumer must be capable of writing and executing scripts (needs terminal/code execution tools).

---

## 7. Toolchain Interposition (Build-Review)

**Intent:** Isolate complex build/deploy/test-execution mechanics in a dedicated mechanical agent, keeping execution agents focused on their primary concern (writing code, reviewing logic).

**Mechanism:** A dedicated `build-review` agent is inserted between each writer agent and its reviewer. It handles compilation, deployment to target platforms, and test execution — concerns that would overload the writer and reviewer agents if they had to own the toolchain directly. The reviewer then reads build results as an artifact input rather than invoking the toolchain itself.

```
test-writer-tdd → build-review → tests-review-tdd
implementation-tdd → build-review → implementation-review
```

**Where it appears:**
- `brownfield-tdd-build-verified` — `build-review` appears twice per TDD stage (after test writing, after implementation), with separate output artifacts for context isolation

**When to use:** When the build/compile/test toolchain is not accessible via standard terminal commands and requires specialized tool invocations (MCP servers, COM automation, proprietary IDEs, cross-compilation). If `dotnet build` or equivalent works from the shell, standard `brownfield-tdd` suffices.

**Trade-offs:**
- Doubles the agent invocations per stage compared to standard TDD workflows. Justified only when the toolchain complexity genuinely doesn't belong in execution agents' context.
- The build-review agent is mechanical (no judgment calls), so its failure mode is clean: either it builds or it doesn't, and `On Findings` routes back to the writer.

---

## 8. Three-Tier Evidence-Evaluation-Synthesis

**Intent:** Produce fair, evidence-grounded comparative analysis by strictly separating evidence gathering from evaluation from synthesis.

**Mechanism:** Three distinct tiers of agents, each with a progressively wider scope:
1. **Evidence gathering** (verdict-free): Per-entity research agents document *what is* without quality judgments.
2. **Per-dimension evaluation**: A comparison agent evaluates all entities within a single dimension.
3. **Cross-dimension synthesis**: A synthesis agent integrates all per-dimension comparisons into a unified analysis.

```
Tier 1: per-product, per-dimension research (verdict-free)
    ↓
Tier 2: per-dimension comparison (cross-product)
    ↓
Tier 3: synthesis (cross-dimension)
```

**Where it appears:**
- `product-comparison` — 9 dimension-specific research agents (Tier 1), `topic-analyst` (Tier 2), `comparison-analyst` (Tier 3)

**Why strict separation matters:** If evidence-gathering agents are allowed to evaluate, their framing biases the downstream comparison. If comparison happens at the synthesis level without per-dimension analysis, the synthesis agent must hold too much context and comparisons become shallow. The three-tier structure ensures each tier operates at the right level of abstraction with manageable context.

**Trade-offs:**
- Many agent invocations (products x dimensions + dimensions + 1). Cost scales with the product of entities and dimensions.
- The dimension set is currently fixed in the workflow table. Configurable dimension activation is structurally possible using Pattern 10 (Agent-Directed Routing) — a planner selects which dimensions to activate from a predefined vocabulary — but has not been applied to this workflow.

---

## 9. Retrieval-Driven Research (No Ingestion)

**Intent:** Access large, frequently-changing external documents at runtime through retrieval tooling rather than ingesting them into a knowledge base upfront.

**Mechanism:** A research agent specializes in retrieval *discipline* — query, extract, identify dangling references, re-query until no unresolved references remain — rather than in a specific retrieval tool. Project-specific retrieval configuration arrives through deploy-time injections.

**Where it appears:**
- `requirements-to-test-cases` — `document-research` retrieves from large specification documents (300+ pages) via runtime tooling
- `hw-schema-kb-generation` — `hw-schema-research` queries hardware schematics through a specialized tool

**When to prefer over ingestion:** When the source documents are too large or dense for lossy summarization, change frequently enough that a KB would be perpetually stale, or vary in format/membership across projects.

**Trade-offs:**
- Retrieval quality becomes the workflow's dominant risk, and it lives outside the agents. Mitigation: source locators on every extracted statement (for human spot-checking) plus an independent review gate on retrieval sufficiency.
- External tool dependency: the workflow cannot run without the retrieval tool being provided and correctly configured. This dependency should be surfaced in the workflow's hint, not buried in an agent's tool list.

---

## 10. Agent-Directed Routing (Shared Vocabulary)

**Intent:** Let a subagent influence which downstream agents execute, by selecting from a predefined vocabulary that both the agent and the orchestrator understand. This makes routing *dynamic within a bounded set* — the workflow table defines all possible paths, and the agent picks which one activates at runtime.

**Mechanism:** The workflow predefines a mapping from selection names to agent sequences. A deciding agent (typically a planner, but could be any subagent) writes its selection into an artifact. The orchestrator reads the selection, looks it up in the shared mapping, and activates only the matching agents. The orchestrator doesn't interpret the selection semantically — it just resolves the name to a route.

```
Shared vocabulary (defined in workflow):

    selection-name-A  →  [agent-X, agent-Y]
    selection-name-B  →  [agent-Y, agent-X]
    selection-name-C  →  [agent-X]

Deciding agent writes: "use selection-name-B"
Orchestrator activates: agent-Y, then agent-X
```

The key constraint: the vocabulary is closed. The deciding agent can only pick from selections the workflow already defines. This keeps routing predictable (no arbitrary agent sequences invented at runtime) while making it flexible (the same workflow supports multiple paths without duplicating rows).

**Concrete realization in the catalog: Execution Groups.** The shared vocabulary is the Execution Groups table. Selection names are approach names (TDD, Implementation-First, etc.). Agent sequences are ordered groups of rows tagged via Phase column notation (`EXECUTION.Test.[StageNumber]`, `EXECUTION.Implementation.[StageNumber]`). The planner writes each stage's approach; the orchestrator maps it to groups.

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |

**Where it appears (via Execution Groups):**
- `brownfield-tdd`, `greenfield-tdd`, `brownfield-tdd-build-verified` — planner selects execution approach per stage
- `brownfield-pr-fix` — same, with an additional Response group

**Where it could apply beyond Execution Groups:**
- Configurable research dimensions in `product-comparison` — a planner or user selects which dimensions to run
- Selective audit types in staged audits — choosing which audit tracks to activate per stage
- Any workflow where rows should be conditionally activated based on a runtime decision rather than hardcoded

**How this differs from static routing:** `On Success` / `On Findings` routing is fixed in the workflow table — every run follows the same paths. This pattern adds a second routing mechanism that's dynamic *within* the static structure: the workflow table still defines all possible paths, but a subagent's output determines which subset activates for a given unit of work.

**Relationship to Pattern 3 (Decompose-Then-Execute):** Orthogonal. Decompose-Then-Execute answers "how do we break work into units?" Agent-Directed Routing answers "within each unit, which agents run and in what order?" You can have staged execution without agent-directed routing (fixed sequence, like `implementation-only`), and agent-directed routing could theoretically apply without stages — though no workflow currently does this.

**Trade-offs:**
- The vocabulary must be defined upfront in the workflow. Adding a new selection requires updating the workflow definition, not just the deciding agent.
- An invalid selection (agent writes a name not in the vocabulary) is a silent misconfiguration — no agents activate, and the unit produces nothing. The vocabulary should be surfaced to the deciding agent explicitly.
- Adds a layer of indirection to the workflow table. Readers need to cross-reference the mapping to understand what actually runs.

---

## Anti-Patterns

Patterns observed in the catalog that are kept as explicit negative examples — what to avoid and why.

### A1. Skipping Quality Gates for Speed

**Exemplar:** `quick-fix` — skips RESEARCH (no `codebase-research`), skips test writing (no `test-writer-tdd`), has no `Requirements.md` in its artifact list.

**Why it fails:** Bug fixes ship with no regression test proving the fix holds. "Well-understood" modifications are never actually verified against the codebase. The two things a lightweight workflow is most tempted to cut — requirements clarity and regression coverage — are exactly the two things that catch the failure modes small fixes are most prone to.

**Lesson:** When designing a lighter-weight workflow, cut *scope* (fewer stages, simpler plan), not *quality gates* (review, test, requirements verification).

### A2. Creator-Gated HITL (Legacy Convention)

**Exemplar:** All older Build/Design workflows (`brownfield-tdd`, `greenfield-tdd`, `brownfield-design`, etc.) mark the *creator* row `TRUE` instead of using the Presenter Pattern.

**Why it's suboptimal:** The human reviews every draft the creator produces — including drafts the paired reviewer would have flagged anyway. This wastes human attention on unconverged work. The fix is Pattern 2 (Convergence-Gated Human Approval), pending enough field validation to trust it for backporting.

**Status:** Acknowledged in the Open Ideas section of every affected workflow. Waiting on `requirements-to-test-cases` to prove the presenter pattern before migrating.

---

## Structural Observations

Recurring structural motifs observed in the catalog. These are not full patterns with reusable design decisions — they're observations worth naming for reference, but their pattern status is unconfirmed either because they haven't been used enough or because they describe a structural consequence rather than a deliberate design choice.

### Interface Bookends

The same interface agent appears twice in a workflow — once at the start to retrieve external context, once at the end to post results back. Parenthetical suffixes disambiguate: `(retrieve)` and `(post)`.

Appears in `brownfield-pr-audit` and `brownfield-pr-fix` for PR comment retrieval/posting. The posting step typically needs a separate HITL gate for timing control (the human decides *when* to post, independent of approving *what* to post). External system state may change between retrieval and posting — the workflow operates on a snapshot.

Worth watching: currently appears only in PR-integrated workflows. If more external system integrations emerge, this may graduate to a full pattern. For now it's an observation about a structural shape two workflows share.

### Complementary Workflow Pairs

A full workflow split at a natural boundary into a "head" and a "tail" that each serve as standalone workflows. `brownfield-design` (RESEARCH/PLANNING/DESIGN) + `implementation-only` (EXECUTION/REVIEW) together reconstitute `brownfield-tdd`.

The head half (`brownfield-design`) has a genuine standalone purpose: "produce a design without implementing it" is a real, distinct request. The tail half (`implementation-only`) is weaker — its original purpose was manual run continuation, now largely superseded by native run persistence. Neither half has been used as a deliberate complementary pair in practice.

Noting this mainly as a historical observation. The temptation to split a long workflow into resume-from-phase-X variants should be resisted — native run continuation is almost always the better answer now.

---

## Pattern Composition

Most workflows combine multiple patterns. This section maps which patterns each workflow uses, for quick reference.

| Workflow | Patterns Used |
|----------|--------------|
| `brownfield-tdd` | 1 (Creator-Reviewer), 3A (Predetermined Decomposition), 10 (Agent-Directed Routing), A2 (Creator-Gated HITL) |
| `greenfield-tdd` | 1, 3A, 10, A2 |
| `brownfield-tdd-build-verified` | 1, 3A, 7 (Toolchain Interposition), 10, A2 |
| `brownfield-pr-fix` | 1, 3A, 5 (Fan-Out), 10 |
| `brownfield-pr-audit` | 1, 3A, 4 (Multi-Pass Research), 5 (Fan-Out), 6 (Script-Mediated Scaling) |
| `brownfield-system-audit` | 1, 4, 5 |
| `brownfield-design` | 1, A2 |
| `implementation-only` | 1, 3A |
| `quick-fix` | 1, 3A, A1 (Skipping Gates) |
| `brownfield-research-only` | *(none — minimal single-agent workflow)* |
| `product-comparison` | 1, 5, 8 (Three-Tier Evidence-Evaluation-Synthesis) |
| `kb-generation` | 3B (Emergent Decomposition), 5 |
| `kb-correction` | 3B |
| `hw-schema-kb-generation` | 3B, 5, 9 (Retrieval-Driven Research) |
| `kb-verification-human` | 5 |
| `requirements-to-test-cases` | 1, 2 (Presenter Pattern), 9 |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 0.1 | 2026-08-30 | MOSAIC | Initial draft — patterns extracted from analysis of all 16 catalog workflows |
