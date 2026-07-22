---
name: pr-scope-filtering
description: Precise git diff scope filtering for PR reviews. Use when deciding whether an audit finding applies to a PR, filtering a response queue to in-scope findings, or checking file/line overlap with changed hunks. Covers prerequisite git checks, hunk parsing, rename detection, and file-category classification.
---

> **Read This Entire File** until you reach `END OF SKILL`.

# PR Scope Filtering via Git Diff

A finding is **in scope** only if its file+line range overlaps with lines actually changed in the PR. File presence alone is not sufficient.

---

## Prerequisite: Verify Git State

**Always use remote refs** (`origin/<branch>`), never bare branch names. See `git-read-commands` skill for full pre-flight checklist.

**Failure mode:** A stale local `integration` showed 773 files instead of 398 — nearly 2× false scope.

---

## The Canonical Git Command

```bash
git diff -M origin/<target>...origin/<source>           # full diff (three-dot, rename-aware)
git diff -M --name-status origin/<target>...origin/<source>  # file list with change types
git diff origin/<target>...origin/<source> -- path/to/File.cs  # per-file (non-renamed only)
```

Always three-dot for PRs. Always `-M` when renames are possible.

---

## File Categories

From `-M --name-status` output:

| Status | Scope Rule |
|--------|------------|
| `A` (added) | ALL lines in scope |
| `M` (modified) | Only lines overlapping changed hunks |
| `R100` (renamed, 100% match) | NO lines in scope — pure move |
| `R001`–`R099` (renamed with edits) | Only lines overlapping changed hunks (use `-M` diff) |
| `D` (deleted) | Out of scope |

**Never treat a renamed file as "all lines in scope."**

---

## Hunk Headers

```
@@ -<old_start>[,<old_count>] +<new_start>[,<new_count>] @@
```

**Use `+` side** (new file) for scope checking: `start = new_start`, `end = new_start + new_count - 1`.

Git includes 3 context lines before/after actual changes. These are part of the hunk range for overlap checking. For findings that overlap only context lines (not actual changes), see CONTEXT-ZONE.md.

### Overlap Check

```python
def overlaps(finding_start, finding_end, hunk_start, hunk_end):
    return finding_start <= hunk_end and finding_end >= hunk_start
```

---

## Line Number Semantics

Findings reference the **new branch** (PR source). Always use the `+` side of hunks.

**Line drift:** When lines are added/removed earlier in a file, subsequent line numbers shift. Verify against actual source if numbers seem off:
```bash
git show origin/<source>:"path/to/File.cs"
```

---

## Rename-Aware Diff Workflow

**Per-file `-- <path>` silently returns empty for renamed files** — the file didn't exist at the merge-base under the new path. No error, just empty output.

### Correct approach:

1. Get rename map: `git diff -M --diff-filter=R --name-status origin/<target>...origin/<source>`
2. Fetch full `-M` diff once (cache it): `git diff -M origin/<target>...origin/<source>`
3. Search cached diff for `rename to <new_path>` to find relevant section
4. `similarity index 100%` → zero lines in scope (pure rename)
5. Otherwise parse hunk headers normally

---

## Path Handling

Git paths are repo-root-relative. Audit findings may use different prefixes. Normalize before comparing.

If git diff returns empty for a file that should be in the PR, verify the exact path:
```bash
git diff -M --name-status origin/<target>...origin/<source> | grep <filename>
```

---

## Decision Tree

```
File in PR's changed list?
  NO  → Out of scope
  YES → Status A?        → All lines in scope
        Status R100?      → Out of scope (pure rename)
        Status D?         → Out of scope (deleted)
        Status R<100 or M?
          Finding overlaps any hunk range?
            NO  → Out of scope
            YES → Finding overlaps actual changed lines? (see CONTEXT-ZONE.md for method)
                  YES → In scope (definite)
                  NO  → Context zone: apply relevance check (see CONTEXT-ZONE.md)
```

---

## Common Mistakes

| Mistake | Consequence | Fix |
|---------|-------------|-----|
| Bare branch name instead of `origin/` | Wrong diff from stale branch | Always `origin/<branch>` |
| `-- <path>` on renamed file | Silent empty output | Full cached `-M` diff |
| File in PR → all findings in scope | Out-of-hunk findings posted | Always check hunk overlap |
| Using `-` side line numbers | Wrong range | Use `+` side |
| Excluding hunk context lines from overlap check | Under-inclusion | Hunk range (with context) is the overlap boundary; then apply context zone check |
| Per-file `-M` diff for each rename | N subprocess calls | Fetch full diff once |
| Skipping findings matching resolved threads | Resolved ≠ fixed | Forward findings regardless |
| Hunk overlap → in scope (ignoring context zone) | Context-irrelevant findings leak through | Check actual changed lines, not just hunk range — see CONTEXT-ZONE.md |

---

## When to Apply

- Deciding if an audit finding's file/line range is relevant to a PR
- Filtering a batch of response queue entries to in-scope only
- Any PR workflow where audits produce findings with file+line locations

**Prerequisite:** See `git-read-commands` skill for correct ref usage.

---

END OF SKILL
