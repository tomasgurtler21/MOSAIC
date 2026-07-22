---
name: efficient-file-reading
description: Efficient file reading strategies that maximize context quality while minimizing context waste. Use when exploring codebases, reading documentation, analyzing configuration files, or investigating any file-based content. Covers scout-first reading, targeted search patterns, and structure-aware exploration. Tool-agnostic principles applicable across all harnesses.
---

> **Read This Entire File:** This skill file must be read in full before applying any guidance. If your file reading tool has a line limit (e.g., 80 lines by default), use explicit limit/offset parameters to read beyond that. Keep reading until you reach the `END OF SKILL` marker at the bottom of this file. Do not proceed until you have done so.

# Efficient File Reading

This skill governs how you read files. It does not cover what to investigate, how to plan research, or how to manage context from other sources (conversation history, tool outputs, etc.) — only file reading behavior.

The core insight: **precise context produces better results than exhaustive context**, even when gathering it requires more tool calls.

## Core Principle: Context Quality Over Speed

Multiple targeted reads that build precise understanding outperform single large reads that flood context with irrelevant content.

**Why this matters:**
- Context window is a finite, shared resource — every irrelevant line displaces a potentially relevant one
- Irrelevant content actively degrades output quality — it doesn't just take up space, it competes with relevant content for attention and can mislead reasoning
- The cost of extra tool calls is negligible compared to the cost of degraded output from context pollution

This is a hard rule. The speed-over-precision tradeoff is a false economy: a faster read that produces worse output saves nothing.

---

## Reading Principles

These are not a sequence — apply whichever is relevant to your current situation.

### Scout Before You Commit

Before reading any unfamiliar file, read a small opening portion. This serves two purposes:
1. **File size discovery** — read tool responses typically include total line count. This prevents blind full reads.
2. **Orientation** — the opening reveals what kind of file you're dealing with and how to approach it.

What the opening reveals:
- **Code files:** Class/module declaration, imports, and the beginning of the public interface
- **Documentation:** Table of contents, heading structure, or document purpose
- **Configuration:** Format (JSON, YAML, etc.) and top-level structure
- **Test files:** Test class organization and naming patterns

### Default to Targeted Reading

Targeted reading is the default. Full reads are the exception for genuinely small files where targeting overhead would exceed the content itself.

When you need to understand something, narrow your read to just that:

**Understanding what something is (structure/interface):**
- Read structure and contracts first — declarations, signatures, headings, doc comments, top-level keys
- These reveal *what* something does without the noise of *how* it does it
- Dive into implementation details only for the specific parts you're investigating

**Finding something specific:**
- Search for it by name, keyword, or pattern — then read only the match location with surrounding context
- Don't browse sequentially hoping to find it

**Understanding how something is used:**
- Search across files for the symbol, term, or pattern
- Read only the matching locations with enough context to understand usage

**Orientation (entering something unfamiliar):**
- List contents first — files, directories, headings, sections
- Use naming conventions and structure to navigate
- Read structural files (entry points, indexes, manifests, READMEs) to understand organization

### Search Over Browse

When you know what you're looking for, search for it. This applies at two levels:

**Within a file:** Search for the specific function, variable, class, section heading, or configuration key you need. Read the match location with context.

**Across files:** Search for patterns, symbols, or terms across multiple files to find where something is defined, used, or configured. Then read only the relevant locations.

Searching is almost always faster and more precise than sequential reading.

### Parallel Reads Are Fine

When you have multiple targeted reads to make, execute them in parallel. Scouting several files at once, reading specific sections across different files, or running multiple searches simultaneously are all efficient patterns. The constraint is that each individual read is targeted — parallelism amplifies good reading habits, not replaces them.

---

## Structure-Aware Reading

Different file types reward different exploration strategies:

**Code files** — Public API is typically at the top or grouped together. Read declarations, method/function signatures, and doc comments first. These reveal what the code does. Dive into method bodies only for the specific behavior you're investigating.

**Markdown/documentation** — Headings provide a table of contents. Search for or scan headings to find relevant sections, then read only those sections in detail.

**Configuration files (JSON, YAML, TOML)** — Top-level keys reveal structure. Search for specific configuration keys rather than reading the entire file.

**Test files** — Test method names and descriptions document intended behavior, often more clearly than the production code itself. Search for or scan test names first to understand what a module is supposed to do, then read specific test implementations only when needed.

---

## Anti-Patterns

### Blind Full Reads
**Problem:** Reading an entire file without knowing its size. You expect 50 lines, it's 2,000 lines. Your context fills with irrelevant content, displacing information you actually need later.
**Instead:** Scout first — read a small opening portion to discover file size (most read tools report total line count in their response metadata), then choose your strategy based on what you actually need.

### Reading Everything "Just in Case"
**Problem:** Deliberately reading an entire large file to "make sure you don't miss anything," even when you only need specific parts.
**Reality:** You miss things more often because important details get buried in irrelevant context. Relevant information competes with noise for your attention.
**Instead:** Read only what you need. If you later discover you need more, go back — that's cheaper than pre-loading everything.

### Sequential Browsing for Specific Content
**Problem:** Reading a file in sequential chunks (lines 1-100, then 101-200, then 201-300...) hoping to stumble on something specific. Each chunk adds noise to context while a single search would locate the content immediately.
**Instead:** Search for the term, pattern, or identifier you need. Read the search results with surrounding context.

### Reading Implementation to Understand Interface
**Problem:** Reading an entire 500-line class to understand what it does.
**Reality:** The public interface — class declaration, method signatures, doc comments — tells you what it does. The rest tells you how, which you usually don't need.
**Instead:** Read the public interface and signatures. Read private implementation only for the specific behavior you're investigating.

---

## When to Apply This Skill

Apply these principles whenever you read files to gather information — whether exploring codebases, reading documentation, analyzing configuration, or investigating any file-based content. This skill governs HOW you read, not WHAT you investigate.

---

END OF SKILL
