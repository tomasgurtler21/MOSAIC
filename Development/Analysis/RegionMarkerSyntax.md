# Region Marker Syntax Analysis

**Question:** What delimiter syntax should MOSAIC use for the four boundary markers (`SECTION`, `DEPLOYED`, `INJECTION`, `CUSTOM`) that structure agent files? And what should those markers be called?

**Current syntax:** `[[KIND:Name]]` ... `[[/KIND:Name]]`

**Context:** The markers appear in agent source files and in deployed agent files (system prompts). They are read by both the deployment tool and the LLM at runtime. They must convey three things: the region kind (who owns it), the region name, and whether the tag opens or closes.

The analysis proceeds in two parts: §1 evaluates the **delimiter syntax** (what characters wrap the markers), §2 evaluates the **tag name and attribute design** (what vocabulary the markers use and how kind/name/version are encoded).

---

## Candidates

## Part 1 — Delimiter Syntax

Five delimiter families are realistic options. Each is evaluated against four criteria:

1. **LLM comprehension** — how well the model recognises the marker as a structural boundary, given its training distribution.
2. **Token efficiency** — overhead per region pair (open + close).
3. **Tooling** — ease of parsing, conflict with content, risk of false matches.
4. **Precedent** — whether the style has vendor backing, community adoption, or published evidence.

---

### 1. Double-Bracket Custom Markers (current)

```
[[SECTION:Identity]]
...
[[/SECTION:Identity]]
```

**LLM comprehension:** Weak. `[[...]]` is not a syntax in any markup language in standard training corpora. The model must infer from context that it is a structural boundary. MediaWiki uses `[[...]]` for internal links — a different semantic entirely, and one that may actively mislead a model into treating the marker as a reference rather than a container.

**Token efficiency:** Moderate. Opening: `[[SECTION:Identity]]` = 7 tokens (approximate, varies by tokeniser). Closing: `[[/SECTION:Identity]]` = 7 tokens. The closing tag redundantly repeats the name — §2.2 already requires unique names per file, so the repeated name carries zero information.

**Tooling:** Requires custom regex. No standard parser exists. Low collision risk with code — the double-bracket pattern is rare outside MediaWiki — but this advantage is moot when code content belongs in fenced code blocks regardless.

**Precedent:** None. No LLM vendor recommends custom bracket syntax. No published benchmark tests it. CMU's delimiter guide lists three valid families (markdown headers, XML tags, keyword markers); double brackets are not among them.

---

### 2. XML-Style Tags

```
<section name="Identity">
...
</section>
```

**LLM comprehension:** Strong. Models are trained on billions of HTML/XML documents. `<tag>...</tag>` is immediately understood as a structural container. Anthropic states Claude was "specifically tuned to pay special attention to XML structure", with internal testing showing 20-40% more consistent outputs from structured XML vs unstructured text, and a further 5-10% improvement from using the provider's preferred delimiter style. Llama-3.1 405B benchmarks show XML consistently outperforming other formats for complex prompts. XML is a *semantic* delimiter — it tells the model what role a block plays, not just where it starts.

**Token efficiency:** Good. Opening: `<section name="Identity">` = ~6 tokens. Closing: `</section>` = ~3 tokens. The closing tag does not repeat the name (standard XML convention), saving tokens on every close. Per region pair, ~2-4 tokens cheaper than `[[]]`. One study measured XML adding 31% token overhead vs no delimiters at all on short prompts — but for prompts exceeding ~500 tokens with 3+ logical sections, the accuracy gain justifies it and the overhead is negligible relative to prompt length. MOSAIC agents are 2,000-5,000+ tokens; the overhead is trivially small.

**Tooling:** Excellent. XML parsing is a solved problem with libraries in every language. False-match risk with code is handled by code fences — the same mechanism that protects every other delimiter from code content. Closing tags are unambiguous (no need to match names; nesting depth suffices).

**Precedent:** Strongest of any option. Recommended by Anthropic, OpenAI, and Google. Used in Anthropic's own system prompts (`<system-reminder>`, `<context>`, etc.). CMU lists it as one of three valid delimiter families. Every major prompt engineering guide from 2025-2026 recommends it for complex, multi-section prompts.

**Variant considerations:**

| Sub-option | Example | Pros | Cons |
|---|---|---|---|
| Attribute-based | `<section name="Identity">` | Standard XML; model has seen this billions of times; name is clearly data | Generic tag; all closing tags identical (`</section>`); name buried in attribute |
| Namespace-style | `<section:Identity>` | Compact | XML namespace semantics (URI-identified vocabulary) do not apply here; models trained on XML know what `:` means in tag names and this is not it |
| Hyphenated | `<section-Identity>` | Simple | Creates a unique tag name per region — the model has never seen `<section-Identity>` before, weakening the training-distribution advantage |
| Name-first | `<Identity type="core">` | Region name is the tag — maximally prominent; closing tags are unique and self-describing (`</Identity>`); matches how vendors use XML in prompts | Tag names are not standard HTML elements (but neither are vendor-recommended tags like `<instructions>`, `<context>`) |

Name-first is the strongest sub-option. Part 2 analyses this in detail.

---

### 3. Markdown Headings

```
## SECTION: Identity
...
## /SECTION: Identity
```

Or without explicit close:

```
## SECTION: Identity
...
(next heading implicitly closes)
```

**LLM comprehension:** Moderate-good for opening. Models understand `##` as a section heading. But markdown headings are **open-boundary only** — they mark a beginning, not an end. The model must infer where the section stops. Multiple sources identify this as the critical weakness: "When a section has no defined end, the model has to guess where your reference text stops and your instruction resumes, and under pressure it guesses wrong." Adding an explicit close heading (`## /SECTION: Identity`) is non-standard markdown and partly defeats the purpose.

**Token efficiency:** Best of all options for opening tags (~3-4 tokens). No closing tag in pure markdown, which is maximally efficient but sacrifices boundary clarity. With explicit close tags, roughly equal to XML.

**Tooling:** Parsing is simple (line-prefix match). But the lack of a closing boundary is a real problem for the deployment tool: the tool must know *exactly* where a region ends to preserve, replace, or regenerate content. Implicit closing (next heading) is fragile — content between two headings at different levels (e.g., `##` vs `###`) creates ambiguity.

**Precedent:** OpenAI recommends markdown headers for GPT-series models specifically. CMU lists it as one of three valid families. It is the natural format for human-readable prompts. However, no vendor recommends it for delimiters that require both open and close boundaries, and no published guide uses markdown headings for the four-kind, named-region pattern MOSAIC needs.

**Verdict:** Markdown headings are good for top-level document structure (and MOSAIC already uses them inside sections — `## Capabilities`, `### Process`). They are not a fit for the four-kind boundary markers, because those markers *must* have explicit open and close tags to support the deployment tool's region-manipulation operations (content replacement, nesting, reordering).

---

### 4. Keyword/Fence Markers

```
__SECTION_START:Identity__
...
__SECTION_END:Identity__
```

Or:

```
--- SECTION: Identity ---
...
--- /SECTION: Identity ---
```

**LLM comprehension:** Weak-moderate. `__KEYWORD__` is a Python dunder convention, not a structural markup pattern. `--- KEYWORD ---` is a horizontal rule in markdown, which models recognise as a *separator* but not as a *named container*. Neither pattern conveys "this is a structural region with content inside it" the way XML does. The model must learn the convention from context.

**Token efficiency:** Moderate. Comparable to `[[]]` — the overhead of prefix/suffix characters is similar.

**Tooling:** Requires custom regex (same as `[[]]`). `---` has collision risk with markdown horizontal rules and YAML frontmatter delimiters (both use `---`). `__` has collision risk with Python dunder names in code content (mitigated by code fences, but unnecessary).

**Precedent:** CMU lists `__KEYWORD__` as valid for API payloads and markdown generation. No major vendor recommends it for system prompt structuring. No published benchmark compares it against XML or markdown for prompt adherence.

**Verdict:** This is `[[]]` with a different character set. It shares the same fundamental weakness — a custom syntax the model has to learn — without any compensating advantage.

---

### 5. Hybrid: Markdown Headings (Structure) + XML Tags (Regions)

```
## Identity

<deployed name="CommunicationProtocol">
...
</deployed>

<injection name="CodebaseContext">
...
</injection>
```

**LLM comprehension:** Strong. Combines the model's familiarity with both markdown headings (for document-level sections) and XML tags (for ownership-typed regions). Multiple sources recommend this hybrid for complex prompts: "use Markdown headers for top-level sections and XML tags for individual data items."

**Token efficiency:** Good. Markdown headings are cheaper than XML section tags. XML is used only for the typed regions that need open/close boundaries.

**Tooling:** Works well. Markdown headings for the seven canonical sections (Identity, CommunicationProtocol, Capabilities, etc.) are already present in agent files — those sections use `## Heading` today *inside* `[[SECTION:]]` wrappers. The `[[SECTION:]]` wrapper could be dropped, leaving the heading as the section boundary. The three other region kinds (DEPLOYED, INJECTION, CUSTOM) would use XML tags.

**Precedent:** Explicitly recommended by multiple sources as optimal for complex, multi-section prompts.

**Verdict:** Interesting, but raises a question: the `SECTION` kind exists precisely to mark MOSAIC-authored content carried verbatim. If sections are identified by markdown headings alone, the ownership signal ("this is MOSAIC-authored, do not edit") disappears from the file. This could be recovered by wrapping the heading: `<section name="Identity">` ... `## Identity` ... `</section>`, but then the heading is redundant with the tag and the hybrid advantage vanishes. Worth considering if the `SECTION` ownership signal can be expressed differently.

---

## Evaluation Summary

| Criterion | `[[KIND:]]` (current) | XML tags | Markdown headings | Keyword markers | Hybrid |
|---|---|---|---|---|---|
| LLM comprehension | Weak | **Strong** | Moderate (no close) | Weak-moderate | Strong |
| Token efficiency | Moderate | Good | **Best** (no close) | Moderate | Good |
| Tooling (parse/manipulate) | Custom regex | **Standard parsers** | Fragile (no close) | Custom regex | Mixed |
| Open/close boundary | Yes | **Yes** | No (or non-standard) | Yes | Partial |
| Vendor recommendation | None | **All three** | OpenAI (GPT only) | CMU (API only) | Multiple |
| Conveys ownership kind | Yes | **Yes** | No | Yes | Partial |
| Training distribution | Absent | **Massive** | Large | Small | Large |

---

## Part 1 — Conclusion

XML attribute-style tags are the clear winner on every criterion. Markdown headings lack close boundaries. Custom syntaxes (`[[]]`, `__KEYWORD__`) sit outside training distributions with no compensating advantage. The remaining question is not *whether* to use XML tags but *what to name them* — which is Part 2.

---

## Part 2 — Tag Name and Attribute Design

### The Problem with Kind-as-Tag-Name

Part 1 used `<section name="Identity">` as its running example — a generic tag with the region name in an attribute. An alternative considered in the variant table was four distinct tag names mapping to the four region kinds: `<section>`, `<deployed>`, `<injection>`, `<custom>`. Both approaches share a problem: the tag name carries either a generic container word or build-process jargon, while the region's actual identity — the thing the LLM and the human care most about — is buried in an attribute.

The jargon problem is worse with four kind-named tags:

| Current kind | Proposed tag | What an LLM understands | Problem |
|---|---|---|---|
| SECTION | `<section>` | HTML5 semantic element — "a thematic grouping" | Fine, but generic — says nothing about *which* thematic grouping |
| DEPLOYED | `<deployed>` | Nothing — "deployed from where?" | Build-process jargon; meaningless at runtime |
| INJECTION | `<injection>` | SQL injection, prompt injection, code injection | Actively negative connotations in training data |
| CUSTOM | `<custom>` | Vague — everything in a prompt is "custom" | No semantic signal |

These names describe **how the content got there** (the tooling lifecycle), not **what the content is for** (which is what the LLM cares about). The model does not know there is a deployment pipeline. It does not benefit from knowing a region was "injected" vs "deployed."

### Name-First: The Region Name IS the Tag

The vendor-recommended pattern is to use the **semantic name as the tag itself**. Anthropic's own system prompts use `<system-reminder>`, `<context>`, `<example>` — not `<section name="system-reminder">`. Their documentation recommends `<instructions>`, `<context>`, `<output_format>`. OpenAI's examples follow the same pattern. No vendor wraps content in a generic container and puts the meaningful name in an attribute.

Applied to MOSAIC, the region name becomes the tag, and the ownership kind moves to a `type` attribute:

```xml
<Identity type="core">
...
</Identity>

<CommunicationProtocol type="managed" version="1.10">
...
</CommunicationProtocol>

<CodebaseContext type="project">
...
</CodebaseContext>
```

**Why this is stronger than a single generic tag:**

1. **The most important information is in the most prominent position.** The LLM sees `<Identity>`, `<Capabilities>`, `<ErrorHandling>` — each tag immediately says what the section is about. With `<section name="Identity">`, the model must read past the tag name to the attribute to learn the same thing.

2. **Closing tags are unique and self-describing.** `</Identity>`, `</CommunicationProtocol>`, `</CodebaseContext>` — every closing tag identifies itself. With a single `<section>` tag, every closing tag is `</section>`, creating the same `</div>`-soup readability problem HTML5 was designed to solve. In a file with 15 nested regions, unique closing tags are significant visual landmarks for humans.

3. **Matches vendor practice exactly.** This is how Anthropic, OpenAI, and Google all use XML tags in prompts. The pattern is not novel — it is the standard.

4. **The `type` attribute provides the consistent structural signal.** The model does not need one fixed tag name to recognise boundaries — `type="core"` / `type="managed"` / `type="project"` / `type="custom"` appears across all tags and tells the tool and the author what the ownership is. The model treats attributes as metadata, which is exactly the right role for ownership information — secondary to the content's identity.

**Objection and rebuttal:** The Part 1 variant table noted that name-first means "the model cannot learn '`<section>` means structural boundary' as a pattern." This objection was wrong. The model does not need a single tag name to recognise structure — each tag name is self-evident (`<Capabilities>` obviously starts a capabilities section), and vendor-recommended tags like `<instructions>`, `<context>`, `<output_format>` are not standard HTML elements either. What makes a tag recognisable as structure is the `<Name>...</Name>` pattern itself, not any particular tag name.

### Type Attribute Values

The `type` attribute carries the ownership/lifecycle signal that the old region kinds carried. Three naming schemes were considered:

| Current kind | What it means | Option A: Semantic | Option B: Ownership | Option C: Lifecycle |
|---|---|---|---|---|
| SECTION | MOSAIC-authored, carried verbatim | `core` | `mosaic` | `static` |
| DEPLOYED | Tool-generated, regenerated | `managed` | `shared` | `generated` |
| INJECTION | MOSAIC-declared slot, project-filled | `project` | `project` | `configurable` |
| CUSTOM | Project-invented, free-form | `custom` | `custom` | `custom` |

**Option A (semantic) is the strongest.** Each value communicates meaning without jargon:

- **`core`** — this is foundational agent content, authored by MOSAIC. The word conveys importance and stability.
- **`managed`** — this content is maintained by something (the tool/framework). Clear to humans and inert to the LLM — no negative connotations, no implementation detail.
- **`project`** — this is a declared slot for project-specific content. Signals to both authors and the LLM that this region carries context about the specific project the agent is deployed into. Crucially, this region is declared in the source file and deployed by the tool — its presence signals that the agent's instructions expect this content for correct performance.
- **`custom`** — this is user-invented, free-form content. The distinction from `project` matters: a `project` region is a slot MOSAIC defined because the agent needs it; a `custom` region is something the user added on their own initiative. The tool deploys `project` slots (empty or filled) because the source declares them. `custom` regions exist only because a user wrote them into their deployed file.

Options B and C were rejected: `mosaic` leaks a product name into a prompt (and means nothing to an LLM outside this system), `static`/`generated`/`configurable` are lifecycle terms that describe tooling mechanics.

### Version Attribute

Regions sourced from versioned content (the communication protocol, canonical bundle blocks) benefit from a `version` attribute:

```xml
<CommunicationProtocol type="managed" version="1.10">
```

This is natural XML that tools can read, humans can inspect, and the LLM understands intuitively. It replaces encoding version information in comments or external tracking. Not every region carries it — only those sourced from a versioned contract or bundle.

### Closing Tag

Standard XML: the tag name repeated, no attributes. `<Identity type="core">` closes with `</Identity>`. Because region names are unique per file (`AgentTemplateArchitecture.md` §2.2), every closing tag in a file is unique — a significant readability advantage over a single-tag approach where every close is `</section>`.

### Empty Regions

Source files contain many empty regions — `project` and `custom` slots awaiting content. These use a standard open/close pair with no body:

```xml
<CodebaseContext type="project">
</CodebaseContext>
```

Self-closing (`<CodebaseContext type="project" />`) is not used, despite being valid XML and marginally more token-efficient. The reason is practical: a human filling the slot pastes content between the tags. With an open/close pair, they paste between two lines. With a self-closing tag, they must first manually edit the tag itself (split it into open and close, remove the `/`). That is a needless obstacle for the most common operation on these regions.

### Tool Matching

**A tag is a MOSAIC region boundary if and only if it carries a `type` attribute with a recognised value (`core`, `managed`, `project`, `custom`).** Any tag without a valid `type` attribute is content, not a boundary.

For `core`, `managed`, and `project` regions this is belt-and-suspenders — the tool already knows every valid region name from the vocabulary, so even a bare `<CommunicationProtocol>` would be identifiable. But for `custom` regions the `type` attribute is the **only** discriminator. Custom regions have user-invented names the tool has never seen, so the tool cannot distinguish `<MyProjectExtension>` from a stray HTML tag in content by name alone. `<MyProjectExtension type="custom">` is unambiguous; `<MyProjectExtension>` without the attribute would be invisible to the tool — or worse, a false match if the tool tried to guess.

This makes the `type` attribute load-bearing for correctness, not just informational. The tool must require it on every region boundary, and must ignore any tag that lacks it. Code fences remain the standard protection for code blocks that happen to contain tags with `type` attributes.

### Compound Names

The compound name syntax (`Prefix:id` for enumerable items — workflows, infrastructure declarations) becomes `<Prefix:id type="core">`. The colon in an XML tag name is technically namespace syntax, but models handle it without confusion in practice, and the tool's enumeration logic (`split on :`) is unchanged. If strict XML compliance is desired, an alternative is a hyphen: `<Prefix-id type="core">`.

### Tag Casing

Tag names match region names, which use PascalCase (`Identity`, `CommunicationProtocol`, `CodebaseContext`). This is consistent with how MOSAIC already names its regions and how most XML vocabularies name elements. The `type` attribute and its values remain lowercase.

---

## Validation Against Vendor Practice

The recommendation was checked against actual XML tag usage in vendor documentation and examples (Anthropic, OpenAI, community guides).

### How vendors use XML tags in prompts

Two patterns emerge:

**Pattern 1 — Name-first, no attributes (dominant).** The tag name is the semantic label. No attributes.

```xml
<instructions>...</instructions>
<context>...</context>
<task>...</task>
<output_format>...</output_format>
<thinking>...</thinking>
<constraints>...</constraints>
```

This is the overwhelmingly common case across all vendors. Anthropic's own docs, OpenAI's prompting guide, and every community guide use this pattern for top-level prompt sections.

**Pattern 2 — Name-first, attributes for metadata (secondary).** Attributes appear on repeated or enumerable items where instances need distinguishing or labelling.

Anthropic:
```xml
<document index="1">...</document>
<document name="Q4 Report">...</document>
```

OpenAI (GPT-4.1 Prompting Guide):
```xml
<example1 type="Abbreviate"><input>San Francisco</input><output>SF</output></example1>
<doc id='1' title='The Fox'>The quick brown fox...</doc>
```

Community:
```xml
<document name="Source 1">{{paste source}}</document>
```

OpenAI's guide states explicitly: "XML is convenient to precisely wrap a section including start and end, **add metadata to the tags for additional context**, and enable nesting." Attributes are a recommended part of the pattern, not an afterthought.

### How MOSAIC's design maps to these patterns

MOSAIC's recommended syntax follows both patterns naturally:

| Vendor pattern | MOSAIC equivalent | Example |
|---|---|---|
| Name-first tag (Pattern 1) | Region name as tag | `<Identity>`, `<Capabilities>`, `<ErrorHandling>` |
| Metadata attribute (Pattern 2) | `type` for ownership kind | `type="core"`, `type="managed"` |
| Index/id attribute (Pattern 2) | `version` for versioned regions | `version="1.10"` |

The tag name carries the primary identity (what this section is about), and attributes carry metadata (who owns it, what version). This is the same role `index="1"` or `name="Q4 Report"` plays in vendor examples — secondary information that does not define the tag's purpose.

### What no vendor does

No vendor example uses a generic container tag with the meaningful name in an attribute — nobody writes `<section name="instructions">` or `<tag type="context">`. The semantic label is always the tag name itself. This validates the name-first design over the single-tag `<section name="...">` alternative.

### Attributes and LLM comprehension

No vendor documentation warns against attributes or suggests they confuse models. OpenAI explicitly recommends them for adding context. Anthropic's own document-handling examples use `index` attributes as a standard pattern. The `type` attribute in MOSAIC's design is more semantic than a simple index, but it uses plain English values (`core`, `managed`, `project`, `custom`) that carry no jargon and require no special knowledge to parse. Models treat XML attributes as metadata naturally — this is well within the training distribution.

---

## Recommendation

Name-first XML tags with `type` and optional `version` attributes:

```xml
<Identity type="core">
# Codebase Research Agent

You are the **Codebase Research Agent** ...

### Process
1. Read input artifacts...
2. ...
</Identity>

<CommunicationProtocol type="managed" version="1.10">
...protocol contract text...
</CommunicationProtocol>

<Capabilities type="core">
## Capabilities
...
</Capabilities>

<CodebaseContext type="project">
...project-specific codebase knowledge...
</CodebaseContext>

<LanguagePatterns type="custom">
...user-added language patterns...
</LanguagePatterns>
```

**Summary of decisions:**

| Decision | Choice | Rationale |
|---|---|---|
| Delimiter family | XML tags | Strongest LLM comprehension, vendor-recommended, standard parsers, explicit open/close |
| Tag name | Region name as tag (`<Identity>`, `<Capabilities>`, etc.) | Matches vendor practice; most important information in most prominent position; unique closing tags |
| Kind encoding | `type` attribute with values `core`/`managed`/`project`/`custom` | Semantic names meaningful to all three audiences (LLM, tool, human); ownership as metadata not tag identity |
| Version | Optional `version` attribute on managed regions | Natural XML; replaces external version tracking |
| Closing tag | `</TagName>` (standard XML) | Every closing tag is unique and self-describing |
| Tag casing | PascalCase (matches region names) | Consistent with existing MOSAIC naming; standard XML element casing |
| Compound names | `<Prefix:id type="core">` | Tool enumeration logic unchanged |
| Empty regions | Open/close pair, no self-closing | Self-closing forces humans to edit the tag itself when pasting content |
| Tool matching | Require valid `type` attribute | Tags without `type` are content, not boundaries |

---

## Sources

- [Anthropic Prompting Best Practices](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices) — official documentation
- [Markdown vs XML in LLM Prompts: A Comparative Analysis](https://www.robertodiasduarte.com.br/en/markdown-vs-xml-em-prompts-para-llms-uma-analise-comparativa/) — performance data showing up to 40% variation by format
- [Structuring Prompts: XML Tags, Markdown, Delimiters](https://ai-tldr.dev/learn/prompt-engineering/prompting-basics/structure-prompts-xml-markdown/) — per-scenario format recommendations
- [Delimiter and Markup Strategies](https://www.braindrip.blog/courses/prompt-engineering/02-core-prompting-techniques/delimiter-and-markup-strategies) — 15-20% section adherence improvement with structured delimiters
- [Delimiter Choice — CMU LibGuides](https://guides.library.cmu.edu/LLMDocumentationGuide/Delimiters) — three valid delimiter families
- [Mastering XML Tags for LLMs](https://medium.com/@TechforHumans/effective-prompt-engineering-mastering-xml-tags-for-clarity-precision-and-security-in-llms-992cae203fdc) — XML as semantic delimiter, vendor convergence
- [Delimiters in Prompt Engineering — Portkey](https://portkey.ai/blog/delimiters-in-prompt-engineering/) — delimiter selection guidance
- [XML Tags Don't Help Short Prompts](https://dev.to/manishramavat/xml-tags-dont-help-short-prompts-heres-when-they-actually-matter-2026-25gf) — 31% overhead on short prompts, negligible on long prompts
- [LLM Prompt Format 2026 — FutureAGI](https://futureagi.com/blog/llm-prompts-best-practices-2025/) — provider-specific recommendations, "do not mix" rule
- [XML Tags vs Markdown for AI Prompts — UD](https://www.ud.hk/en/blogs/insight/article/xml-tags-prompting-2026-07-15) — XML closed boundaries vs markdown open boundaries
- [Stop Writing Blob-Prompts: Anthropic's XML Tags](https://pub.towardsai.net/stop-writing-blob-prompts-anthropics-xml-tags-turn-claude-into-a-contract-machine-aa45ccc4232c) — Anthropic's internal testing data
