---
version: 0.4
name: Knowledge Verification (Human) Workflow
description: Verify knowledge quality using architect-provided challenge questions. Tests whether an agent can answer expert questions using available knowledge sources + codebase. Produces a diagnostic report — remediation is a separate concern.
hint: Verify KB quality using human-provided challenge questions
author: MOSAIC
id: kb-verification-human
referenced_agents:
  - verification-questions-preparer
  - codebase-research
  - verification-answer-validator
artifacts:
  - VerificationQuestions.md
  - VerificationAnswers.md
  - VerificationAttemptedAnswers.md
  - VerificationReport.md
---

[[SECTION:Workflow:kb-verification-human]]
<!-- workflow-version: 0.4 -->
## Knowledge Verification (Human) Workflow

**Use when:** Verify knowledge quality using **architect-provided challenge questions**. Tests whether an agent can answer expert questions using available knowledge sources + codebase. Produces a diagnostic report — remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer | ✅ | codebase-research* | - | - | - | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ✅ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)
- **Knowledge-source agnostic** — verifies whether questions can be answered from whatever knowledge sources the codebase-research agent has access to (KB, docs, code comments, etc.)
[[/SECTION:Workflow:kb-verification-human]]

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
