---
name: git-read-commands
description: Safe, read-only git commands for AI agents. Covers remote refs, staleness detection, diff/log/show commands, rename handling, and pitfalls. Use for any git-based analysis (PR audits, code reviews, branch comparisons). NOT FOR write operations (commit, push, merge, rebase, checkout, reset).
---

> **Read This Entire File** until you reach `END OF SKILL`.

# Git Read Commands for AI Agents

**Core insight: local branches go stale, remote refs are truth.** Always use `origin/<branch>`, never bare branch names.

---

## Core Principle: Always Use Remote Refs

Local branches are snapshots from the last pull/checkout. They silently diverge from the remote.

**Observed impact:** Local `integration` (stale) showed 773 files / 89K insertions. Remote `origin/integration` showed 398 files / 44K insertions — nearly 2× false scope.

```bash
# WRONG                                    # CORRECT
git diff integration...PBI/Feature         git diff origin/integration...origin/PBI/Feature
```

Applies to all ref-taking commands: `diff`, `log`, `merge-base`, `show`, etc.

---

## Pre-Flight Checklist

Run before any analysis:

```bash
# 0. Detect CWD vs repo root offset
git rev-parse --show-prefix      # non-empty (e.g. "TestTool/") = you're in a subdirectory

# 1. Verify remote refs exist
git rev-parse --verify origin/<target>
git rev-parse --verify origin/<source>

# 2. Detect staleness (if local branch exists)
git rev-parse <branch>           # local hash
git rev-parse origin/<branch>    # remote hash — if different, local is stale

# 3. Fetch if refs are missing or stale (safe — read-only for working tree)
git fetch origin                 # or: git fetch origin <branch1> <branch2>
```

---

## CWD ≠ Repo Root — Path Gotcha

When CWD is a subdirectory of the repo root, git commands use **different path formats**. Silent empty results or cryptic errors — never obvious failures.

```bash
PREFIX=$(git rev-parse --show-prefix)   # e.g. "TestTool/"
```

If `PREFIX` is non-empty, consult this table:

| Command | Input `-- <path>` | Output paths | `<ref>:<path>` |
|---------|-------------------|--------------|-----------------|
| `git diff` | CWD-relative | **Repo-root-relative** | N/A |
| `git log` | CWD-relative | N/A | N/A |
| `git ls-tree` | CWD-relative | CWD-relative (scoped) | N/A |
| `git show` | N/A | N/A | **Repo-root-relative** |

**Conversions:**
```bash
# CWD-relative → repo-root (for git show):   "${PREFIX}${CWD_PATH}"
# Repo-root → CWD-relative (for -- <path>):  "${REPO_PATH#$PREFIX}"
```

---

## Command Reference

All commands below are read-only.

### git diff — Comparing Branches

```bash
# Three-dot (always use for PRs) — changes on <source> since divergence from <target>
git diff origin/<target>...origin/<source>

# Two-dot — difference between two commits (includes changes on BOTH branches)
git diff origin/<target> origin/<source>
```

Two-dot and three-dot are identical when target hasn't diverged since source branched off, but this isn't guaranteed — **always use three-dot for PRs.**

### git diff — Key Flags

| Flag | Purpose | Notes |
|------|---------|-------|
| `-M` | Rename detection | **Always use** when renames are possible |
| `--name-status` | File list with change type (A/M/D/R) | PR file list |
| `--stat` | Insertion/deletion summary | Quick scope overview |
| `--diff-filter=R` | Only renamed files | Rename map |
| `-- <path>` | Limit to specific file | **Non-renamed files only** (see below) |

### Rename Detection with -M

Without `-M`, renames appear as delete+add — all lines look "new." Always use it.

```bash
git diff -M --name-status origin/<target>...origin/<source>
# R088  old/path/File.cs  new/path/File.cs
# R100  old/path/Pure.cs  new/path/Pure.cs  (100% = pure rename)
```

**Per-file `-- <path>` silently fails for renamed files** — returns empty, no error. The file didn't exist at the merge-base under the new path.

**Solution:** Fetch the full `-M` diff once, search sections in memory for `rename to <new_path>`. See `pr-scope-filtering` skill for the workflow.

### git log — Commit History

```bash
git log --oneline origin/<target>...origin/<source>              # PR commits
git log --oneline --stat origin/<target>...origin/<source>       # with file summary
git log --oneline origin/<target>...origin/<source> -- <path>    # specific file
```

### git show — File at Specific Ref

```bash
git show origin/<branch>:"path/to/File.cs"
git show abc1234:"path/to/File.cs"
```

### git merge-base — Common Ancestor

```bash
git merge-base origin/<target> origin/<source>
# If result == origin/<target>, then two-dot and three-dot diffs are identical
```

### git rev-parse / git branch

```bash
git rev-parse origin/<branch>                 # resolve to commit hash
git rev-parse --verify origin/<branch>        # verify ref exists (exit code 0 = valid)
git branch -r                                 # list remote branches
git branch -a -v                              # all branches with last commit
```

---

## PR Analysis Pattern

```bash
git fetch origin
git rev-parse --verify origin/<target>
git rev-parse --verify origin/<source>
git diff -M --name-status origin/<target>...origin/<source>    # file list
git diff -M origin/<target>...origin/<source>                  # full diff (cache this)
```

For non-renamed files, per-file diff works: `git diff origin/<target>...origin/<source> -- "path/to/File.cs"`
For renamed files, search the cached full `-M` diff by `rename to <path>`.

---

## Anti-Patterns

| Anti-Pattern | Consequence | Fix |
|-------------|-------------|-----|
| `git diff integration...feature` (bare branch) | Stale → wrong diff | Use `origin/` refs |
| Two-dot for PR scope | Includes both-branch changes | Three-dot (`...`) |
| `-- <path>` on renamed file | Silent empty output | Full `-M` diff, search sections |
| Missing `-M` flag | Renames appear as delete+add | Always use `-M` |
| No fetch before analysis | Outdated remote refs | `git fetch origin` first |
| `git remote show origin` for validation | Slow network call | `git rev-parse` instead |
| Mixing path formats when CWD ≠ repo root | Silent empty diffs or `git show` failures | `git rev-parse --show-prefix`; see CWD ≠ Repo Root section |

---

## Never Run (Unless Explicitly Requested)

These modify repository state: `checkout`, `switch`, `commit`, `push`, `merge`, `rebase`, `reset`, `clean`, `stash drop`, `branch -d/-D`, `tag -d`.

**`git fetch` is safe** — only updates remote-tracking refs.

---

## When to Apply

Any time you use git commands to analyze a repository: PR audits, code reviews, branch comparisons, commit history, file retrieval at specific refs.

---

END OF SKILL
