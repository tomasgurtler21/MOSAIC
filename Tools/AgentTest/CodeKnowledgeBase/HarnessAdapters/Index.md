---
run_id: 20260809T123015Z-79fd
created_by: knowledge-base-generator#3
human_approved: false
---

# Harness Adapter Framework

> Responsibility: Isolate every piece of harness-specific knowledge behind one port (`domain.HarnessAdapter`), so the rest of the module can provision a sandbox, translate an intercepted call, and spawn a subject without ever naming which agent harness it is running under.

## Overview

The tool's whole premise — intercept a real collaborator invocation and answer with a scripted stub — depends on a mechanism that differs completely from one agent harness to the next: how hooks are registered, whether a call's result can be substituted outright or only have its input rewritten, how a correlation token survives the round trip, what configuration scopes might silently compete with the tool's own hooks. This area is where that variability lives, contained behind a single port so nothing else in the module needs a harness name.

Two implementations exist: a scripted, no-LLM `fake` adapter used to drive the whole pipeline in tests with no external dependency, and `claudecode`, the adapter for the Claude Code CLI/hook system — currently the only real harness this tool drives. Both are required to satisfy the same conformance suite, unchanged.

The port is deliberately narrow: it answers exactly the questions the conformance suite asks, nothing more. Process control (actually starting the subject) is a separate port (`SubjectLauncher`, documented in the Test Execution & Evaluation Flow area) precisely so a harness adapter never has to both describe a subject's launch and know how to perform it.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **`domain.HarnessAdapter` port** | The eight-method contract every adapter implements: identity, capability declaration, configuration-scope enumeration/inspection, provisioning/deprovisioning a sandbox, spawn-plan construction, and bidirectional call/outcome translation. Owned by the Domain area; documented here because its obligations are meaningless without the conformance suite that enforces them. |
| **Conformance suite (`contract`)** | The executable specification of the port. A `Config` value supplies an adapter constructor plus the handful of harness-native seams (building a native pre/post payload, observing an emitted native reply) the suite needs to drive an adapter through its own wire format without any test knowing that format itself. `Run(t, cfg)` exercises every obligation; every adapter must pass it unchanged. |
| **`fake` adapter** | A scripted `HarnessAdapter` with no LLM dependency. Declares whatever `Capabilities` a test configures (so both the substitute and rewrite-prompt paths are reachable deterministically), consumes a per-collaborator queue of scripted `Turn`s, and also stands in for the subject process itself via a `SpawnPlan` whose `Stdin` carries an encoded script for a matching scripted stand-in binary. Its native wire shape is private to its own package and never leaks. |
| **`claudecode` adapter** | The real adapter for the Claude Code harness. Composed of several files, each owning one facet: hook registration/composition (`registration.go`), the interceptor's own hook entries (`bridge.go`), native payload shapes (`native.go`), payload-to-model translation (`translate.go`), correlation-token planting/recovery (`correlation.go`), configuration-scope enumeration/inspection (`scopes.go`), subject spawn-plan construction (`spawn.go`), CLI output decoding (`decode.go`), interpreter resolution for the logger bundle (`interpreter.go`), and the harness's fixed project-scoped layout (`paths.go`). |

## Key Flows

### Provisioning a sandbox

Agent definition files for the subject and each declared stub collaborator are rendered into the sandbox by the `domain.AgentDeployer` port (`internal/agentdeploy`) during `runner.setup`, **before** `Provision` is called. The port invokes `mosaic-deploy render --output json` as a subprocess and uses the destination path the tool reports — the adapter itself never knows or reconstructs where under the sandbox a harness expects an agent file to live. Rendered paths are recorded in the provisioning ledger as each render returns, so `Deprovision` removes them even if a later step fails.

An adapter's `Provision` then writes everything else the harness needs entirely under the sandbox it is given, and returns a `Provisioning` ledger (files and directories, in creation order) that `Deprovision` later removes exactly — nothing more, nothing less, and idempotently. For `claudecode` this means: composing the interceptor's own hook registration (via `bridge.go`'s `Bridge`/`InterceptorEntries`) together with any logger-bundle registration fragments the caller supplied, and writing the composed settings document. Composition happens over a parsed model (`registration.go`'s `Settings`/`Contribution`), never by splicing text, because the single-rewriter invariant below is only checkable over a parsed structure and byte-determinism (for golden-file testing) is only achievable over a re-serialized one.

`Provision` must refuse — return an error — rather than proceed whenever the composed configuration would contain more than one entry that rewrites the intercepted call's input (`ErrMultipleRewriters`). This is checked once, over the whole composed set, before any subject is spawned; it is not an assumption enforced elsewhere. On any failure partway through, `Provision` still returns the ledger of what it created before the failure, so the caller can still tear down exactly that partial state. The adapter's returned `Provisioning` is merged into the setup ledger (not assigned over it), so rendered paths already recorded there survive.

### Translating a call, both directions

`TranslateCall` turns a native payload (the harness's own wire format) into `domain.InterceptedCall`; `TranslateOutcome` turns a `domain.InterceptionOutcome` decision back into a native reply. Both directions must degrade gracefully — an unrecognised or malformed native payload is always a returned error, never a panic and never a zero-valued call passed off as success. This obligation is what keeps an interceptor failure from crashing or hanging the live subject turn it runs inside (see the System-Wide Patterns entry on this in the project Index).

For `claudecode`, `PhasePre` and `PhasePost` are materially different code paths (`translatePre`/`translatePost` in `translate.go`), because the post-invocation point additionally must recover an already-completed collaborator's observed response (`extractText`) and because its `TranslateOutcome` always passes through unconditionally on `PhasePost` — this harness's post-invocation point can only observe, never rewrite or substitute a completed call's result.

### Correlation token round trip

Every adapter nominates one native field (declared as `Capabilities().CorrelationField`) to carry an opaque correlation token from the pre-invocation interception point through to the post-invocation point, so a post-invocation record can be keyed back to its start record without relying on identity or call sequence alone. The token and the field name it rides in must both be free of any vocabulary that would tip the subject off that it is being tested (`fake`, `mock`, `test`, `mosaic`, `stub`, `harness` are explicitly banned in the conformance suite). `claudecode` plants its token inside the rewritten prompt text itself, separated by an invisible marker character, because this harness offers no dedicated field that survives the round trip — the token has to ride inside a field the harness already echoes back.

### Capability-honest outcome delivery

`Capabilities()` must be constant across calls and truthful. The single most load-bearing test in the conformance suite (`CheckCapabilityHonesty`) drives a stubbed invocation through an adapter's translation pair and compares the *observed* effect (via `Config.Observe`, which interprets the harness's own native reply) against what `Capabilities().SupportsDirectSubstitution` predicts. An adapter that declares direct substitution but only achieves prompt rewriting — or the reverse — fails here, because a wrong capability flag otherwise produces stubs injected unfaithfully in a way no downstream evaluation can detect. `claudecode` declares `SupportsDirectSubstitution: false` (its pre-invocation point can only rewrite input, never fabricate a result) and fails loudly (`ErrSubstitutionUnsupported`) rather than emit a reply that merely looks like a substitution if ever asked for one.

### Configuration-scope inspection and neutralization

`ConfigScopes` enumerates every scope the harness merges (ordered lowest to highest precedence), including scopes entirely outside the sandbox (e.g. a user-level settings file). `InspectScopes` examines every non-sandbox scope and reports, per scope, whether it registers a hook that would rewrite the intercepted call's input, and whether the adapter neutralized (rather than merely inspected) that scope. A scope the harness offers a way to isolate (`ConfigScope.Isolatable`) must actually come back neutralized — the adapter never writes outside the sandbox, so "neutralized" always means redirecting the harness's own view of that scope, never editing it. `claudecode` redirects its entire user-scope configuration into a location inside the sandbox's control directory via an environment variable set in the subject's spawn plan, which is what lets it report the user scope as neutralized. An absent scope is treated as "neutralized if isolatable" (nothing to compete), never silently folded in as "inspected and clean" when a read failure or malformed document is what actually happened — those degrade to "still potentially rewriting, treated as unresolved for safety" instead.

### Pre-flight environment check (claudecode-specific)

Before any subject is spawned, `claudecode.Adapter.CheckEnvironment` validates everything that must hold: a usable interpreter for the logger bundle's registration entries (probed by actually running it, not merely found on the path), a readable logger bundle, a resolvable absolute path for the currently-running binary (the bridge's own executable), and configuration scopes free of un-neutralized input-rewriting hooks. This runs during pre-flight rather than during provisioning, because a wrong interpreter produces a run with no logs and therefore no attributable cost — a silent failure the tool cannot afford to let through only to surface later as an unexplained gap in evidence.

### Spawn planning

`SpawnPlan` is pure description: it builds a `domain.SpawnPlan` (executable, args, working directory, environment, timeout, an early-exit sentinel path) without starting any process or touching any subprocess API — the conformance suite explicitly asserts the sandbox is untouched by merely calling it. Something else (a `SubjectLauncher`, outside this area) executes the plan later. `claudecode` sets its own generous backstop timeout constant rather than any test-declared value, because no argument this method receives carries a test's declared timeout — that is enforced elsewhere, by the runner's supervisor cancelling the launch context.

## Relationships

| Talks To | For |
|----------|-----|
| Domain | Owns the `HarnessAdapter` port and every model type (`InterceptedCall`, `InterceptionOutcome`, `HarnessCapabilities`, `ProvisionRequest`, `Provisioning`, `SpawnPlan`, `ConfigScope`, `ScopeFinding`) this area's methods speak in. |
| Interception Pipeline | The interceptor process invokes an adapter's `TranslateCall`/`TranslateOutcome` once per intercepted call, synchronously inside a live subject turn. |
| Subject Launch | Consumes the `SpawnPlan` an adapter built, and receives a harness-specific envelope-decoding function (e.g. `claudecode.DecodeEnvelope`) from the composition root so it can interpret a finished invocation without knowing which harness produced it. |
| Composition Root | The only place outside `internal/harness/` allowed to construct a concrete adapter (`claudecode.New` or `fake.New`) and wire it behind the port. Also constructs the `domain.AgentDeployer` port (`agentdeploy.New`) and wires it into `runner.Deps`, so neither the adapter nor the runner names the deploy tool binary. |
| `internal/agentdeploy` | Renders agent definition files into the sandbox before `Provision` is called. The runner holds the port; the adapter holds nothing — this is what keeps the adapter from knowing harness-layout details it used to derive when it wrote stub definitions itself. |
| `mosaic-common/hookbundle`, `mosaic-common/harness` | `claudecode` delegates logger-bundle manifest/variant resolution and CLI argument construction / envelope parsing to these shared packages rather than re-implementing them. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **Capability honesty** | An adapter's declared `Capabilities()` must match what it can actually observably achieve; the conformance suite treats a mismatch as a contract failure, not a quality issue. |
| **Single-rewriter invariant** | At most one registered entry may rewrite the intercepted call's input, across every source composed into the sandbox (the interceptor's own entry plus any bundle fragments plus any pre-existing non-sandbox scope). Provisioning must refuse rather than silently let a second rewriter win. |
| **Neutralized vs. merely inspected scope** | A configuration scope is "inspected" once its document has been read and judged; it is "neutralized" only when the adapter has additionally redirected the harness's view of it away from anything that could compete with the sandbox. |
| **Correlation token** | An opaque, vocabulary-free value planted at the pre-invocation point and recovered at the post-invocation point, used to key related records together without relying on identity/order. |
| **Observed effect** | What a native reply actually causes, as reported by a harness-specific `Config.Observe` function in the conformance suite — the ground truth `Capabilities()` is checked against. |

## Boundaries

- **Owns:** Everything harness-specific — hook/registration formats, native payload shapes, capability declaration, scope inspection/neutralization, spawn-plan construction for a given harness, envelope decoding for a given harness's completed invocation.
- **Does Not Own:** Actually starting or supervising a subject process (Subject Launch), deciding what outcome an intercepted call should receive (Interception Pipeline's pure decision core), constructing which concrete adapter is used at runtime (Composition Root).

## Invariants & Conventions

- No package outside `internal/harness/` may import a concrete adapter package directly; the one exception is the composition root, which alone may construct one. Everything else depends only on the `domain.HarnessAdapter` port.
- `Capabilities()` is constant for an adapter's lifetime and must be truthful, enforced by the conformance suite's capability-honesty check.
- `TranslateCall`/`TranslateOutcome` never panic and never return a zero-valued call as if it were a success; a malformed or unrecognised native payload is always a handleable error.
- `Provision` writes only under the sandbox it is given and returns a ledger; `Deprovision` removes exactly what the ledger records, idempotently.
- `SpawnPlan` performs no process control; it is pure description over its inputs.
- A correlation token, and the field name that carries it, must never contain vocabulary that would reveal to the subject that it is under test.
- Every implementation — including the scripted `fake` — is required to pass the same conformance suite (`contract.Run`) unchanged; the suite is the authority on the port's obligations, not the port's doc comments alone.

## Known Complexity

This area's overview already required tracing the port, the full conformance suite (nine files), and every facet of the `claudecode` adapter to describe accurately — the obligations are specified indirectly through the suite rather than directly in the port, so understanding what an adapter must do requires reading both together. No further deeper-tier document is recommended beyond this one: the flows above (provisioning, translation, correlation, capability honesty, scope neutralization, spawn planning) are each self-contained enough to be understood at this tier without a subsystem-level spec, and a KB consumer working on a new adapter or modifying `claudecode` can navigate directly to the relevant file group from the Components table above.
