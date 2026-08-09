---
run_id: 20260809T123015Z-79fd
created_by: knowledge-base-generator#2
human_approved: false
---

# Interception Pipeline

> Responsibility: Decide, per intercepted collaborator call, what the harness should be told, and durably record that decision — as a short-lived, out-of-process step invoked once per call inside a live agent turn.

## Overview

This is the heart of the tool. Every time the subject under test tries to call a real collaborator, the harness routes that call through this pipeline instead of letting it reach the real collaborator. The pipeline decides whether to answer with a stubbed response, let the call pass through untouched, or halt the subject's turn — then durably records what happened so the Runner (see Execution & Evaluation Flow) can later reconstruct the whole run as evidence.

The area is split into a **pure decision core** (`intercept`, plus the pure matching helper `stubmatch`) and an **imperative shell** (`interceptor`) that performs I/O around it. Three infrastructure packages back the shell: a lock-guarded state document (`runstate`), an append-only log (`invlog`), and a file-effect materialiser (`sideeffects`). This split exists so the decision logic — the part with actual policy — is testable without a filesystem, a clock, or a process, while every failure mode that comes from touching the real world is contained in one place.

The defining constraint on this whole area: **the interceptor process must never damage the run it is measuring.** It runs synchronously inside the subject's turn. Whatever goes wrong internally, it must still answer with something the harness can use and let the subject continue — a test tool that crashes the thing it is testing produces evidence about itself, not about the subject.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **intercept** | Pure decision core. Given a normalized call and the current run state, decides the outcome (substitute / rewrite prompt / passthrough / halt), the state mutation, the log records to emit, and any file side effects — all as returned data, nothing performed. |
| **stubmatch** | Pure lookup: resolves a call (collaborator identity + per-identity invocation ordinal) against the active stub registry, returning whether it matched and, if not, which unmatched policy applies. |
| **interceptor** | The imperative shell — the actual process entry point a harness's interception hook invokes. Reads the native payload, translates it, drives the state read-modify-write, applies side effects, appends log records, and writes the reply. Owns all failure containment. |
| **runstate** | The crash-safe, lock-guarded JSON state document that is the *only* channel through which independent interceptor processes (and the driving Runner) share state about one run. |
| **invlog** | The append-only JSONL invocation log that many independent short-lived processes write to concurrently without coordination beyond the OS's atomic-append guarantee. |
| **sideeffects** | Materialises a stub's declared file effects beneath the subject's directory and records exactly what it created, so teardown can reverse exactly that and nothing else. |

## Key Flows

### One interception (pre-invocation)

1. The harness invokes the interceptor process once, handing it the native call payload on stdin.
2. The shell reads the payload and asks the harness adapter (see Harness Adapters) to translate it into the normalized `InterceptedCall` model. Translation happens outside any lock.
3. **Halt-on-entry check** (no lock, no state read): if the early-exit sentinel file already exists in the control directory — written by an earlier interception in this same sandbox — the call halts immediately with `HaltEarlyExit`, without ever touching run state.
4. Otherwise, the shell performs one lock-guarded read-modify-write via `runstate.Store.Update`: it reads the current state, calls `intercept.Decide` with that state plus the call, and commits the resulting state (`RunState.Apply(delta)`) atomically. Nothing except the read, the decision, and the commit happens inside the lock — no side effects, no log append — because the lock sits on the critical path of a live agent turn.
5. Inside `Decide`, the pre-invocation path checks, in order: the early-exit threshold (halts everything once the run's invocation count has reached it), then extraction failure (collaborator identity undeterminable — halts, never guessed), then resolves the call through `stubmatch.Match`:
   - **Matched** → outcome is `Substitute` or `RewritePrompt`, chosen solely by `HarnessCapabilities.SupportsDirectSubstitution` (no harness name ever enters this decision); the call is marked in-flight and its expected response added as a pending stub awaiting echo verification.
   - **Unmatched, generic-response policy** → same substitution/rewrite choice, using the registry's declared generic response.
   - **Unmatched, passthrough policy** → outcome is `Passthrough`; the call is marked in-flight but nothing is pending.
   - **Unmatched, halt policy** → outcome is `Halt` with `HaltUnmatched`.
6. After the lock releases, side effects (files a matched stub declares) are applied and log records are appended — in that order, before the reply is written, so a subject acting immediately on the reply can never outrun either.
7. If the outcome is an early-exit halt, the shell writes the sentinel file so later calls in the same sandbox short-circuit at step 3.
8. The adapter translates the outcome back into a native reply; the shell writes it and (after `FlushDelay`) returns exit code 0 — always, whatever happened.

### One interception (post-invocation)

Only reached for harnesses whose interception layer has a post-invocation point (`HarnessCapabilities.SupportsPostInterception`). The shell recovers the pending stub by correlation token, and `Decide` compares the collaborator's actual observed output against the expected stub response (`CompareEcho`: parsed as JSON, key order and whitespace ignored — any surrounding prose is treated as a mismatch, not tolerated). It emits an end log record carrying the echo result and clears the in-flight entry. **The post-invocation path never halts or denies** — the collaborator has already run by this point, so refusing it could only damage the subject's turn, never prevent anything.

### Failure containment

Every failure inside `runOneInterception` — an unreadable payload, a translation error, absent/corrupt/unreadable state, a lock the process could not acquire, a side effect it could not write, or a panic anywhere in the call graph (recovered by a deferred handler) — converges on one boundary: write a diagnostic to the diagnostics stream, emit a best-effort neutral (`Passthrough`) native reply so the harness never sees a missing or malformed answer, and append an error record to the log if the log is reachable. Because containment is structural (one boundary every path funnels through), a new failure mode added later cannot bypass it.

### Lock acquisition and reclamation

`runstate.Store.Update` acquires an exclusive lock file before every read-modify-write, polling with exponential backoff up to a fixed timeout. If the current lock holder's process is verifiably dead, or the lock has aged past a staleness threshold, the lock is **reclaimed**: the acquirer renames the existing lock file away (an atomic operation that can only succeed for one racing reclaimer) and then re-creates it under its own identity. Losing either race just means retrying from the top of the poll loop.

`runstate` itself only *reports* a reclamation happened (`UpdateResult.LockReclaimed` + prior holder info) — it cannot log it, because `runstate` may not import `invlog` (layering boundary). The `interceptor` shell is the caller that turns a reported reclamation into a `RunEventLockReclaimed` log record. A reclaimed lock means a prior holder's state update may have been lost, so every verdict computed from that run afterwards is suspect — this is why the record exists at all, not just informational bookkeeping.

### Side-effect lifecycle

A matched stub can declare file effects (paths + content, or a `$ref` fixture reference resolved through the `fixtures` resolver). `sideeffects.Applier.Apply` resolves each effect's path relative to the subject directory and **refuses any path that would escape it**. It tracks which directories it newly created (not pre-existing ones) so a later `Remove` call — driven by a persisted `EffectLedger` — deletes exactly the files and only the directories this pipeline created, deepest-first, and only if empty. Nothing outside the ledger is ever touched. The ledger is saved/loaded independently of the Apply call, because the process that applies effects (the interceptor) and the process that later tears them down (the driver/Runner) are different processes.

## Relationships

| Talks To | For |
|----------|-----|
| Harness Adapters (`domain.HarnessAdapter`) | `interceptor` calls `TranslateCall`/`TranslateOutcome` to convert between the harness's native payload shape and the pipeline's normalized model; the pipeline itself never encodes harness-specific knowledge. |
| Authoring & Preflight / Fixtures | `sideeffects.Applier` resolves `$ref` file-effect content through a `fixtures.Resolver`; the active stub registry and parallel-group declarations it reads (`interceptor.LoadRegistry`/`LoadGroups`) are files a preflight-composed sandbox setup already resolved and token-expanded. |
| Execution & Orchestration (Runner) | The Runner reads `runstate` and `invlog` as evidence after a run completes; it also drives `sideeffects.Remove` during teardown using the persisted ledger. The interceptor pipeline and the Runner never run in the same process — they communicate only through these on-disk artifacts. |
| Domain | Every package here imports only `domain` (and, within this area, each other per the layering rule) — no ambient I/O types, no harness names, no clock reads outside injected `Input.Now`/`domain.Clock`. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| Decision core vs. imperative shell | `intercept` (+ `stubmatch`) decide; `interceptor` (+ `runstate`/`invlog`/`sideeffects`) do I/O. A conditional in the shell that looks like policy is misplaced and belongs in the core. |
| Collaborator identity + ordinal | Stub matching keys on a composite collaborator identity plus a **per-identity** invocation ordinal (1-based) — counters are independent per identity; the global sequence number is a naming/ordering concept only, never used for matching. |
| Correlation token | An opaque, innocuous token that survives from the pre-invocation call through to the post-invocation call, letting the pipeline recover the pending stub for echo comparison without any other shared context. |
| Substitute vs. RewritePrompt | Both mean "answer with the stubbed value" — which one is chosen depends only on `HarnessCapabilities.SupportsDirectSubstitution`, never on which harness is running. `RewritePrompt` replaces the call's input with an instruction to echo the stub response verbatim, containing no test-revealing vocabulary. |
| Halt reasons | Three distinct reasons a pre-invocation call can halt: early exit (invocation count threshold reached), unmatched (registry has no stub and policy says halt), extraction failed (collaborator identity couldn't be determined at all). Post-invocation never halts. |
| Run-level vs. per-invocation log records | `RecordStart`/`RecordEnd` describe one specific invocation (keyed by `Seq`, `CorrelationToken`); `RecordRun` describes something about the run as a whole (lock reclaimed, early exit triggered, unmatched invocation) and isn't tied to a single call. |
| Effect ledger | The record of exactly what `sideeffects.Apply` created (files + newly-created directories), persisted so a separate later process can undo precisely that footprint. |

## Boundaries

- **Owns:** the interception decision itself (substitute/rewrite/passthrough/halt), stub-to-call matching, the shared run-state document and its locking/reclamation protocol, the invocation log's append/read/filter behavior, and materialising/reversing a stub's declared file effects.
- **Does Not Own:** translating between native harness payloads and the normalized call/outcome model (Harness Adapters); starting or supervising the subject process (Subject Launch / Execution & Orchestration); computing a verdict from the evidence this pipeline produces (Evaluation & Reporting); parsing or validating authored stub registries and test definitions before a run starts (Authoring & Preflight — this pipeline only reads the already-resolved, token-expanded active registry).

## Invariants & Conventions

- No package in this area performs a clock read of its own except through an injected `domain.Clock` (shell) or `Input.Now` (core) — reproducibility depends on it.
- Run state mutations always go through the lock-guarded read-modify-write; there is no unguarded write path, and the mutating closure passed to `Update` must not itself perform I/O.
- State commits are write-temp-then-rename: no reader ever observes a partial document, and a crash between the write and the rename leaves the previous state readable.
- The invocation log is append-only JSONL; a single `Append` call issues one write for its whole batch, which is what keeps concurrent appends from independent processes from interleaving or truncating each other. A trailing partial line on read (the shape a crash mid-append leaves) is tolerated and reported, never treated as an error; a malformed line elsewhere in the file is skipped and reported by line number.
- Absent, corrupt, and unreadable run state are three distinct, `errors.Is`-matchable conditions (`ErrStateAbsent`, `ErrStateCorrupt`, `ErrStateUnreadable`) — conflating them makes later diagnosis wrong, since "absent" means setup never ran and "corrupt" means it ran and something broke afterward.
- A side effect's resolved path is always checked against escaping the subject directory; an effect that would land outside it is refused rather than silently clamped.
- The interceptor process's `Run` entry point always returns exit code 0 and always writes a reply — non-zero exit or a missing reply is read by the harness as a failed hook, which would damage the very run being measured.
- On Windows, file rename/delete/open-for-write against a lock or state file can transiently fail with sharing-violation or access-denied errors under contention; `runstate` retries these transiently rather than treating them as protocol failures. This is a platform accommodation, not a semantic change to the lock protocol.

## Known Complexity

No deeper-tier documentation is recommended beyond this document. The lock reclamation protocol and the failure-containment boundary are precise but were captured fully at this tier; a KB consumer investigating either can now go straight to the `runstate` lock implementation or the `interceptor` shell's failure paths with this document as a map, without needing a further intermediate tier.
