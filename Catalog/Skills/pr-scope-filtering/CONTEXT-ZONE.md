## Context Zone Relevance

Git hunks include 3 context lines before/after each actual change. A finding that overlaps a hunk's range but NOT the actual changed lines is in the **context zone** — it may or may not be relevant to the PR.

### When to Apply

Only when ALL of these are true:
1. The file is Modified (status `M`) or Renamed-with-edits (status `R<100`)
2. The finding's line range overlaps with a hunk's total range (including context)
3. The finding's line range does NOT overlap with any actual changed line within that hunk

If condition 3 fails (finding overlaps actual changed lines), the finding is definitively in scope — skip this check.

### Changed Lines vs Context Lines

In a unified diff hunk:
- Lines starting with `+` (new side) or `-` (old side) are **actual changes**
- Lines starting with ` ` (space) are **context lines** — unchanged code shown for reference

To find actual changed line ranges on the new file side:
1. Parse the hunk starting from `new_start`
2. Track line numbers: `+` lines and ` ` lines increment the new-side counter; `-` lines do not
3. Collect ranges of consecutive `+` lines — these are the actual changed ranges

### Relevance Decision

When a finding is in the context zone, determine relevance based on **semantic relationship to the nearby change**:

**In scope (context-relevant):**
- Finding is semantically related to the actual change (LLM analysis)
  - Examples: finding addresses the same variable/field being modified, targets a parameter of a changed method, relates to error handling for the changed code, etc.

**Out of scope (context-irrelevant):**
- Finding is semantically unrelated to the actual change, despite physical proximity
  - Examples: unrelated code style issue, unrelated unused import, unrelated naming convention, unrelated in a different method, etc.

**Decision method:** Use code analysis and LLM reasoning to determine if the finding's concern is semantically connected to the change. Mechanical rules (line proximity, same function body, etc.) are unreliable; semantic judgment is the correct gate.

**When uncertain:** Err toward in-scope. A marginally relevant finding posted to the PR is less harmful than a genuinely relevant finding suppressed.
