# Agent Customization Flexibility Analysis

**Question:** How close can a MOSAIC user get to their ideal agent? Where does the framework enable best-practice context engineering, and where does it constrain it?

**Context:** MOSAIC agents are system prompts with a schema. The schema exists to solve real problems — drift across forty-two agents, protocol interop, deployment automation. But a schema is also a constraint on the person writing the prompt. This analysis evaluates the trade-off: what the framework gives the user in exchange for what it takes away, measured against what current context engineering practice says a well-built system prompt looks like.

---

## 1. What Context Engineering Asks For

The current consensus (Anthropic's own guidance, the CMU delimiter study, and a year of production prompt engineering) converges on a short list of properties for a well-structured system prompt:

1. **Semantic delimiters** — XML-style tags that a model recognises from training as structural containers, not decoration.
2. **Clear ownership** — the reader (model or human) can tell which parts are fixed, which are extensible, which are theirs.
3. **Positional awareness** — critical instructions go where the model attends hardest (early, and immediately before the point of action).
4. **Single-source shared content** — anything repeated across agents is maintained once, not copied.
5. **Minimal overhead** — infrastructure text does not crowd out the content that makes this agent different from every other agent.
6. **Full user sovereignty** — the prompt author can add, reorder, and override any instruction to produce the behavior they need.

---

## 2. Where MOSAIC Meets or Exceeds the Ideal

### 2.1 Delimiter Choice — Best Available

The `<Name type="kind">` syntax was chosen after a formal analysis (`RegionMarkerSyntax.md`) evaluating five delimiter families against LLM comprehension, token efficiency, tooling, and precedent. The name-first XML variant that was selected is the strongest sub-option of the strongest family:

- **LLM comprehension:** Models are trained on billions of XML/HTML documents. Anthropic states Claude is "specifically tuned to pay special attention to XML structure." The benchmark evidence (20-40% more consistent outputs from structured XML) applies directly.
- **Tag names are the region names:** `<Identity type="core">` puts the semantically meaningful word — the section's purpose — in the most prominent position. Closing tags are unique and self-describing (`</Identity>`).
- **The `type` attribute carries ownership:** `type="core"`, `type="managed"`, `type="project"`, `type="custom"` tells the model and the human author who wrote this region and what the rules are, in the tag itself. No lookup required.

This is not merely "good enough." MOSAIC picked the syntax that current evidence says works best for LLMs, and enriched it with ownership semantics that plain XML does not carry. A user writing a prompt from scratch would be advised to use exactly this delimiter style.

### 2.2 Ownership Model — Solves a Real Problem Elegantly

The four region kinds are a genuine design contribution. Most prompt frameworks have two states: "the framework's text" and "your text." MOSAIC distinguishes four:

| Kind | Who writes it | What happens on update | User's relationship to it |
|------|---------------|------------------------|---------------------------|
| `core` | MOSAIC authors | Carried from source verbatim | Read, don't touch |
| `managed` | Deployment tool | Regenerated from canonical source | Infrastructure — it's there for interop |
| `project` | User, in a MOSAIC-declared slot | Preserved byte-identically | Your content, their slot |
| `custom` | User, in a user-invented slot | Preserved byte-identically | Your content, your slot |

This is clearer than any competing system. The tag itself answers "may I edit this?" without consulting documentation. The `project` vs. `custom` distinction — MOSAIC declares the slot vs. the user invents the slot — is unusually precise about provenance and has practical consequences (schema reorder behavior).

### 2.3 Section Architecture — Matches Attention Patterns

The canonical 7-section order (Identity, CommunicationProtocol, Capabilities, Constraints, ErrorHandling, OutputFormat, ExecutionPhilosophy) is not arbitrary. It follows the attention pattern: identity first (where the model attends hardest), then the contract it operates under, then what it can do, then what it must not, then how to fail, then how to report, then working philosophy.

The order rule is deliberately relaxed — "not out of order" rather than "exactly these seven." Sections may be absent. Additional sections may be added. The check is a subsequence, not an equality. This gives users structural freedom while preserving the attention-optimized ordering of the parts that exist.

### 2.4 Single-Sourcing — Drift Prevention at Scale

The deployed sections bundle and the communication protocol solve the exact problem that context engineering at scale always hits: N copies of shared text diverging. MOSAIC's measurement (`AgentBodyDrift.md`) found four divergent fragments across forty-two agents, with three defects riding along. The solution — managed regions filled from a single canonical source on every deploy — eliminates the failure mode structurally rather than through discipline.

A user benefits from this directly: they never have to manually synchronize protocol text, authority hierarchy, closing procedure, or error handling boilerplate across their agents. It's done for them, correctly, every time.

### 2.5 Project Regions — Guided Customization

The injection catalogue (`IdentityExtension`, `CodebaseContext`, `OutputArtifactTemplate`, `SeverityThresholds`, `SeverityDefinitions`, `ErrorHandlingExtension`, `ContextLimits`) guides a user to the right places to customize without requiring them to understand the full schema. Empty project regions in a source file are slots that say "put your content here." The `TODO.md` generated at deploy time makes this actionable.

This is notably better than "here's a template, fill in the blanks" — the regions survive updates, follow the source on schema reorder, and are never required to be filled. A user who fills none of them has a working agent. A user who fills all of them has a specialized one. Both states are valid by design (principle 3).

### 2.6 Custom Regions — Unconstrained Extension

Beyond the declared project regions, a user can invent any region with `type="custom"` and place it anywhere in their deployed file. The name vocabulary is fully open. The tool preserves custom regions byte-identically on update. This is the escape hatch that makes the system genuinely extensible rather than merely configurable.

A user who needs a concept MOSAIC did not anticipate — `<LanguagePatterns type="custom">`, `<ProtocolExtension type="custom">`, `<DomainKnowledge type="custom">`, anything — can add it without forking the framework, and their content survives updates.

### 2.7 Multi-Agent System Integration

The Communication Protocol, status code vocabulary, artifact provenance stamp, HITL gate, and authority hierarchy collectively solve the hardest context engineering problem: making agents work together reliably. A single agent prompt is one problem; a system of forty-two agents that must interoperate through a shared contract is a different one entirely.

MOSAIC handles this by making the interop layer non-negotiable (`type="managed"`, regenerated on every deploy) while leaving everything domain-specific to the user. This is the right trade-off for a multi-agent system: the parts that must agree do agree, and the parts that should differ do differ.

---

## 3. Where MOSAIC Constrains the User

### 3.1 Core vs. Managed: Two Different Questions

Both `type="core"` and `type="managed"` regions are not user-authored, but the constraint they impose is fundamentally different.

**Core regions are not a real constraint.** Core regions are written by whoever authors the agent source file. For MOSAIC's catalog agents, that's MOSAIC. But a user writing their own agent writes their own core regions — Identity, Capabilities, Constraints, ErrorHandling, OutputFormat, ExecutionPhilosophy are all theirs. Even for a user treating a MOSAIC catalog agent as upstream, the core regions are the agent-specific parts: the Goal, Scope, Process, the agent's own constraints, its status mapping. If the user disagrees with core content, they have two clean options: fork the source and own it, or write their own agent from scratch. Both produce a working agent with full update support for managed content. Core content is the user's own text, or text the user chose to adopt.

**Managed regions are the actual constraint.** These are filled by the deployment tool from canonical sources the user does not control. The content is regenerated on every deploy. The user didn't write it, can't edit it durably, and the tool will overwrite anything they change. This is text *imposed on* the user's prompt for systemic reasons (interop, drift prevention), and it is the only text in the system where the user has no sovereignty.

**The rest of this section concerns managed regions exclusively.** Core is a non-issue once the distinction is clear.

### 3.2 Managed Content Cannot Be Overridden

A user can **add** instructions anywhere via project and custom regions. They cannot:

- **Remove** a managed instruction they disagree with
- **Replace** a managed instruction with their own version
- **Suppress** a managed region they don't need for a particular agent

**Why this matters against principle 7.** The architecture document itself states: *"The nearest instruction wins in practice. A model follows the specific, role-adjacent instruction over the general one three sections up."* And §6.5 warns: *"An injection redefining a rule stated in a managed region leaves the agent two answers with no basis to choose, and it will follow the nearer one."*

This creates a structural asymmetry: the user can extend but not contradict. If a managed region says X, and the user's reality requires not-X, they have no clean mechanism. Their options:

1. **Add a contradicting custom region nearby** — the model gets two conflicting instructions and follows whichever is nearer (unpredictable, acknowledged as a hazard in the spec).
2. **Edit the deployed file directly** — works until the next deploy wipes their changes.
3. **Fork the source** — loses all update benefits, reintroduces drift.

In practice, this is the gap between "highly customizable" and "fully sovereign." Every other aspect of the system — delimiters, section order, project regions, custom regions — gives the user control. Managed content does not.

### 3.3 The Practical Weight of This Constraint

How much this costs depends on *what* the managed content says. Not all managed content is equally non-negotiable. Some of it is genuine interop contract (message shape, status codes) that *should* be immutable. Some of it contains environment assumptions (shared filesystem, local user access) that become false in deployment topologies MOSAIC does not yet support. The detailed breakdown is in `ManagedRegionOverrideAnalysis.md`, which classifies every managed instruction as contract vs. environment-dependent.

**Honest assessment:** For a user operating within MOSAIC's current deployment model (single harness, shared filesystem), the constraint rarely bites. The managed content is infrastructure, and most of it is correct for the use case. The friction appears when the deployment topology changes — specifically cross-harness scenarios where environment assumptions become false assertions. At that point, the user needs to override specific managed instructions that state things about their environment that are no longer true, and "extend, don't contradict" is not adequate.

### 3.4 Fixed Minimum Prompt Size

A secondary consequence: the managed regions create a floor on prompt size that the user cannot reduce. The Communication Protocol, five bundle blocks, and harness constraints together consume a fixed number of tokens regardless of how simple the agent's actual job is. For a trivially simple agent on a model with a small context window, this overhead could be disproportionate.

This is less of an issue as context windows grow, but it is a real constraint today for the `LOW` tier models the system explicitly supports.

### 3.5 Intra-Section Ordering

Within a section, managed regions have fixed positions (e.g., `ProtocolConstraints` at top of Constraints, `HarnessConstraints` at bottom, user content between). A user who has studied their model's attention patterns and wants to place a critical constraint at the very top of the section — ahead of `ProtocolConstraints` — cannot do so in a way that survives deployment.

This is a minor constraint in practice (the user can use a custom region at the top of the *previous* section), but it illustrates the broader pattern: the user controls *what* they say but not always *where* it sits relative to managed content.

---

## 4. Comparative Assessment

Against the six context engineering properties from §1:

| Property | MOSAIC Score | Notes |
|----------|-------------|-------|
| Semantic delimiters | **Excellent** | Best available choice, with ownership semantics added |
| Clear ownership | **Excellent** | Four-kind model is clearer than any competing system |
| Positional awareness | **Very Good** | Canonical order matches attention patterns; minor limitation on intra-section ordering relative to managed content |
| Single-source shared content | **Excellent** | Structural solution to drift, not a discipline solution |
| Minimal overhead | **Good** | Managed content creates a floor; appropriate for orchestrated agents, potentially heavy for simple ones |
| Full user sovereignty | **Good with one gap** | Users can add anything anywhere; users cannot override managed content |

---

## 5. The Design Tension

The gap in §3.1 is not an oversight — it's a trade-off. The same property that prevents drift (managed content is regenerated wholesale) is the property that prevents user override. You cannot have both "this text is identical across all agents and updated from one source" and "any user can replace this text in their agent."

MOSAIC chose drift prevention, which is the right choice for a multi-agent system where protocol interop matters more than any single agent's prompt optimization. The question is whether a mechanism could exist that lets a user *intentionally* override a specific managed instruction — with full awareness that they're taking ownership of that instruction and losing automatic updates for it — without breaking the single-source property for everyone else.

That mechanism would look something like: a project region that is explicitly marked as an override rather than an extension, where the tool deploys the managed content but the override region's content takes priority by position or by explicit instruction. The tool could even warn on update when managed content changed under an active override.

Whether this is worth building depends on how often real users hit the constraint in §3.1 — and whether the workarounds (editing deployed files, or accepting the "two conflicting instructions" ambiguity) are adequate in practice.

---

## 6. Summary

MOSAIC agents are among the most thoughtfully structured system prompts in production multi-agent use. The delimiter choice, ownership model, section architecture, and single-sourcing are each individually strong and collectively coherent. The guided customization through project regions, unconstrained extension through custom regions, and the skill system give users substantial control over their agents' behavior.

The single structural limitation is that managed content — the text deployed identically across all agents for protocol interop and drift prevention — cannot be overridden by a user, only extended. This matters most when a user's domain or model requires different behavior than a managed instruction assumes, and the "extend, don't contradict" guidance leaves them with no clean resolution mechanism. Whether this rises from "theoretical limitation" to "practical problem" depends on deployment context, but it is the one point where the framework's interests and the user's interests can structurally conflict.
