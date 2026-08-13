# mosaic-deploy Documentation

This index covers all technical documentation for the `mosaic-deploy` tool.
Start with the [README](../README.md) for installation and first-run instructions,
then use the references below for deeper topics.

---

## For users

| Document | What it covers |
|----------|----------------|
| [README](../README.md) | Installation, the two flows (deploy and update), config file reference, runtime layout, troubleshooting |
| [Configuration Reference](configuration.md) | Every key in `tool-config.yaml` and `user-config.yaml`; the `tool_destinations` tool-mapping layer; precedence, validation rules, worked examples |
| [CLI Reference](cli.md) | Every subcommand, flag, exit code, and the selections file format; machine-readable JSON output |

---

## For harness authors

| Document | What it covers |
|----------|----------------|
| [Harness Contributor Guide](harness-contributor-guide.md) | Choosing a provision tier; step-by-step for descriptor-only and external module harnesses; FieldValue encoding; testing checklist |
| [Descriptor Schema Reference](descriptor-schema.md) | Full YAML schema for `harness.yaml` descriptor files; all field groups with annotated examples |
| [External Module Protocol](external-protocol.md) | JSON-over-stdio protocol for external harness modules; every method with request/response shapes; error handling; shutdown sequence |

---

## For MOSAIC source authors

| Document | What it covers |
|----------|----------------|
| [SourceFilesFormat.md](../../Catalog/SourceFilesFormat.md) | Generic agent frontmatter fields, boundary tag conventions (`[[SECTION:...]]`, `[[DEPLOYED:...]]`, `[[INJECTION:...]]`), version bump rules. Read this before editing generic agent files. |

---

## Document map

The six documents form an outward-pointing reference tree:

```
README.md                  ← start here (users)
├── docs/configuration.md  ← every config key, tool mappings
├── docs/cli.md            ← CLI flags and exit codes
├── docs/harness-contributor-guide.md
│   ├── docs/descriptor-schema.md   ← descriptor YAML reference
│   └── docs/external-protocol.md  ← subprocess protocol
└── ../../Catalog/SourceFilesFormat.md  ← generic source conventions
```

---

## Cross-references

The following table shows where each major concept is documented.

| Concept | Primary reference |
|---------|------------------|
| Installing mosaic-deploy | [README — Installation](../README.md#installation) |
| Runtime directory layout | [README — Runtime layout](../README.md#runtime-layout) |
| Deploy flow | [README — Deploy](../README.md#deploy--create-a-new-workspace) |
| Update flow and conflict resolution | [README — Update](../README.md#update--bring-a-workspace-up-to-date) |
| Utility/infrastructure-only deploy | [README — Utility/infrastructure only](../README.md#utilityinfrastructure-only--deploy-a-subset-without-workflows) |
| tool-config.yaml fields | [Configuration Reference — tool-config.yaml](configuration.md#tool-configyaml) |
| user-config.yaml fields | [Configuration Reference — user-config.yaml](configuration.md#user-configyaml) |
| Mapping a generic tool to a harness tool | [Configuration Reference — tool_destinations](configuration.md#tool_destinations) |
| Silencing the repeated "custom tool" prompt | [Configuration Reference — Why you want it](configuration.md#why-you-want-it) |
| Tool-mapping precedence (user / project / descriptor) | [Configuration Reference — Precedence](configuration.md#precedence) |
| Overriding a built-in harness at runtime | [Configuration Reference — What is NOT configurable here](configuration.md#what-is-not-configurable-here) |
| CLI subcommands and flags | [CLI Reference](cli.md) |
| Selections file format | [CLI Reference — Selections file](cli.md#selections-file-format) |
| Exit codes | [CLI Reference — Exit codes](cli.md#exit-codes) |
| Machine-readable JSON output | [CLI Reference — Machine-readable output](cli.md#machine-readable-output---output-json) |
| Choosing a harness provision tier | [Harness Contributor Guide — Which tier?](harness-contributor-guide.md#which-tier-should-you-use) |
| Writing a descriptor-only harness | [Harness Contributor Guide — Descriptor-only](harness-contributor-guide.md#descriptor-only-harness) |
| Writing an external module | [Harness Contributor Guide — External module](harness-contributor-guide.md#external-module) |
| FieldValue JSON encoding | [Harness Contributor Guide — FieldValue encoding](harness-contributor-guide.md#fieldvalue-encoding) |
| Descriptor YAML schema | [Descriptor Schema Reference](descriptor-schema.md) |
| Tool spec (list vs permission shape) | [Descriptor Schema Reference](descriptor-schema.md) |
| Frontmatter spec (add/drop/key-order) | [Descriptor Schema Reference](descriptor-schema.md) |
| Injections | [Descriptor Schema Reference](descriptor-schema.md) |
| External protocol handshake | [External Module Protocol — Handshake](external-protocol.md) |
| External protocol methods | [External Module Protocol — Methods](external-protocol.md) |
| External protocol error handling | [External Module Protocol — Errors](external-protocol.md) |
| Generic source frontmatter fields | [SourceFilesFormat.md](../../Catalog/SourceFilesFormat.md) |
| Boundary tag conventions (`[[SECTION:]]`, `[[DEPLOYED:]]`, `[[INJECTION:]]`) | [SourceFilesFormat.md](../../Catalog/SourceFilesFormat.md) |
| Import-boundary guard | [README — Import-boundary guard](../README.md#import-boundary-guard) |
| Relationship to boundary_transformer.py | [README — boundary_transformer.py](../README.md#relationship-to-boundary_transformerpy-and-boundary_validatorpy) |

---

## Built-in harnesses

The following harnesses are compiled into the `mosaic-deploy` binary:

| Harness ID | Display name | Notes |
|------------|--------------|-------|
| `claude-code` | Claude Code | Tools emitted as a comma-separated scalar |
| `opencode` | OpenCode | Permission-shape tool mapping; reference external module available |
| `ghcp-cli` | GitHub Copilot CLI | List-shape tool mapping |
| `vscode-ghcp` | VS Code GitHub Copilot | List-shape tool mapping |

Third-party harnesses use the descriptor-only or external module tier. See the
[Harness Contributor Guide](harness-contributor-guide.md).
