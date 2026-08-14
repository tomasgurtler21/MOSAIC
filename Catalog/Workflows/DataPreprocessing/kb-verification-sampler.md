---
version: 0.4
name: Knowledge Verification (Sampler) Workflow
description: Verify knowledge quality using automated random sampling. A sampler agent explores the codebase, generates challenge questions about non-obvious details, then tests whether available knowledge sources guide an agent to the correct answers. Produces a diagnostic report — remediation is a separate concern.
hint: Verify KB quality using automated random question sampling
author: MOSAIC
id: kb-verification-sampler
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

<Workflow type="core" name="kb-verification-sampler" version="0.4">
## Knowledge Verification (Sampler) Workflow

**Use when:** Verify knowledge quality using **automated random sampling**. A sampler agent explores the codebase, generates challenge questions about non-obvious details, then tests whether available knowledge sources guide an agent to the correct answers. Produces a diagnostic report — remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer(create) | ❌ | codebase-question-sampler | - | - | - | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | codebase-question-sampler | ❌ | verification-questions-preparer(validate) | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | verification-questions-preparer(validate) | ❌ | codebase-research* | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ❌ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Q/A artifact lifecycle** — `(create)` creates empty artifacts → `codebase-question-sampler` populates → `(validate)` validates and creates batched `VerificationAttemptedAnswers.md`
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)
</Workflow>

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
