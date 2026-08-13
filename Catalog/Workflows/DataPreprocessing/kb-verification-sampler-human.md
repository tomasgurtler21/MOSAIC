---
version: 1.0
name: Knowledge Verification (Sampler + Human) Workflow
description: Verify knowledge quality using both architect-provided challenge questions and automated random sampling. Gathers questions from both sources in parallel, tests whether an agent can answer them using available knowledge sources + codebase, and produces a unified diagnostic report. Remediation is a separate concern.
hint: Verify KB quality using both human questions and automated sampling in parallel
author: MOSAIC
id: kb-verification-sampler-human
referenced_agents:
  - verification-questions-preparer
  - codebase-question-sampler
  - codebase-research
  - verification-answer-validator
artifacts:
  - VerificationQuestions.md
  - VerificationAnswers.md
  - VerificationAttemptedAnswers.md
  - VerificationReport.md
---

[[SECTION:Workflow:kb-verification-sampler-human]]
<!-- workflow-version: 1.0 -->
## Knowledge Verification (Sampler + Human) Workflow

**Use when:** Verify knowledge quality using **both** architect-provided challenge questions **and** automated random sampling. Gathers questions from both sources in parallel, tests whether an agent can answer them using available knowledge sources + codebase, and produces a unified diagnostic report. Remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer(create) | ❌ | verification-questions-preparer(human), codebase-question-sampler | - | - | - | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | verification-questions-preparer(human) | ✅ | verification-questions-preparer(validate) | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | codebase-question-sampler | ❌ | verification-questions-preparer(validate) | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | verification-questions-preparer(validate) | ❌ | codebase-research* | - | verification-questions-preparer(human), codebase-question-sampler | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ✅ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format in Workflows.md).

**RESEARCH parallel fork:** `verification-questions-preparer(create)` forks into two parallel tracks — `verification-questions-preparer(human)` (HITL, user provides Q/A pairs) and `codebase-question-sampler` (autonomous, explores codebase for implementation details). Both write to the same shared artifacts (VerificationQuestions.md, VerificationAnswers.md) with independent numbering. `verification-questions-preparer(validate)` is the join point — waits for both to complete before validating all pairs and creating batched VerificationAttemptedAnswers.md.

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)
- **Knowledge-source agnostic** — verifies whether questions can be answered from whatever knowledge sources the codebase-research agent has access to (KB, docs, code comments, etc.)
- **Q/A artifact lifecycle** — `(create)` creates empty artifacts → `(human)` and `codebase-question-sampler` populate in parallel → `(validate)` validates all pairs from both sources and creates batched VerificationAttemptedAnswers.md
- **Shared artifact concurrency** — both the human preparer and sampler append to the same VerificationQuestions.md and VerificationAnswers.md. The `Source` field (`user` vs `agent`) distinguishes origin. Both agents append with independent numbering — the validate step handles any numbering conflicts
- **Question sources are independent** — if one source produces no questions (e.g., user has no questions, or sampler finds the codebase too small), the workflow continues with whatever questions the other source produced
[[/SECTION:Workflow:kb-verification-sampler-human]]

---

## Design Rationale

Explain why this workflow is structured the way it is. What trade-offs were made? Why are stages ordered as they are? What alternatives were considered and rejected? This section helps future maintainers understand the thinking behind the workflow rather than just reading what it does.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | YYYY-MM-DD | | Initial version |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- (none yet)
