# Missing Cache-Write Tokens in OpenCode / GitHub Copilot Logs

**Question:** MOSAIC usage records and the log analyzer report `cache write: 0` for every OpenCode run, while cache *reads* are large and growing. Zero cache writes is impossible if reads are non-zero — something must be writing that cache. Where is the number lost: the analyzer, the MOSAIC logger, or the source data?

**Answer:** OpenCode. Its own SDK payload already contains `write: 0`, so MOSAIC and the analyzer are faithful. The loss happens inside OpenCode's usage mapping: for models reached through an OpenAI-compatible route (which is how `github-copilot` is wired), OpenCode never reads the cache-creation count the gateway does in fact return.

**Status:** Root cause identified and externally corroborated against upstream OpenCode issues. The provider **does** report the value; it is discarded client-side and is recoverable.

**Investigated:** 2026-08-25, against logs in `issues/opencode logs/`. Causal explanation revised the same day after external verification — an earlier revision of this document blamed the Copilot proxy for dropping the data. That was wrong; see [Interpretation](#interpretation).

---

## Evidence

### 1. The analyzer is faithful

`mosaic-log-analyzer total --run 20260821T103853Z-a1f4`:

```json
{"schema_version":"1","run_id":"20260821T103853Z-a1f4","provisional":false,
 "currency":"USD",
 "tokens":{"input":343666,"cache_read":3250867,"cache_creation":0,"output":34421},
 "money":{"state":"known","amount":"1.5096104"},"complete":false}
```

It sums `usage_record` events, each carrying `"cache_creation_tokens":0`. The analyzer introduces no error.

### 2. The MOSAIC OpenCode logger is faithful

`Catalog/Hooks/mosaic-logger/opencode/lib/handlers_messages.ts` maps:

| Emitted field | Source |
|---|---|
| `cache_read_tokens` | `info.tokens.cache.read` |
| `cache_creation_tokens` | `info.tokens.cache.write` |

Direct, correct mapping. No defect.

### 3. The zero is already in the raw SDK capture

`00_orchestrator_session.raw` is OpenCode's untouched output (`native_format: opencode-sdk-session-messages-json`):

```
"tokens":{"total":36263,"input":35540,"output":723,"reasoning":0,"cache":{"read":0,"write":0}}
"tokens":{"total":36619,"input":778, "output":303,"reasoning":0,"cache":{"read":35538,"write":0}}
"tokens":{"total":36943,"input":319, "output":310,"reasoning":0,"cache":{"read":36314,"write":0}}
```

Across all three runs (two machines, two dates), `write` is **0 in 1039 of 1039 records — no exceptions.** 91 of those records also show `read: 0`: cold, fully uncached prompts that must have written a large cache prefix, still reported as zero.

Note what this file is and is not. `native_format: opencode-sdk-session-messages-json` is OpenCode's *session store*, written **after** its usage-mapping step. It is not an HTTP capture of the provider response. Anything the mapping discards is already gone before this file exists — so this evidence localises the loss to *at or upstream of the mapping*, and cannot by itself distinguish "the provider never sent it" from "OpenCode didn't read it." Section 7 settles that question.

### 4. Cache writes provably occur

`cache.read` climbs monotonically within a session: 35538 → 36314 → 36948 → 38330 → 39435 → 39871 → 40555 → …

A growing cached prefix cannot be read unless it was first written. The write events are real; only the *reporting* is absent. Message 1 additionally shows `input: 35540` with `read: 0` — a full uncached prompt whose ~35k-token write is reported as 0.

### 5. Not a MOSAIC-wide problem

Claude Code captures in the same repo report the field correctly. From `OrchestrationLogs/20260812T150000Z-9f3a` (`harness: claude-code`):

```
131359 "cache_creation_tokens":300
 95817 "cache_creation_tokens":256
 87819 "cache_creation_tokens":295
```

The schema, the event pipeline, and the analyzer all handle non-zero values correctly when the harness supplies them.

### 6. Common factor across affected runs

| Run | Provider | Model | Non-zero writes |
|---|---|---|---|
| `20260821T103853Z-a1f4` | `github-copilot` | `claude-sonnet-5` | 0 |
| `20260825T115440Z-f8d4` | `github-copilot` | `claude-sonnet-5` | 0 |
| `20260825T124913Z-3dac` | `github-copilot` | `claude-sonnet-5` | 0 |

---

### 7. Upstream confirms the loss is client-side

The symptom is a known, open OpenCode defect: [anomalyco/opencode#36749](https://github.com/anomalyco/opencode/issues/36749), *"cache write tokens always 0 for Anthropic models behind an OpenAI-compatible provider."* Its findings:

- `@ai-sdk/openai-compatible` hardcodes `cacheWrite: void 0`, **but passes the whole provider payload through as `LanguageModelUsage.raw`**.
- The discard point is `usage()` in `packages/opencode/src/session/llm/ai-sdk.ts`. It reads `inputTokenDetails.cacheWriteTokens`, with fallbacks only for *native* `anthropic` / `vertex` / `bedrock` metadata. The OpenAI-compatible route matches none of them and falls through to `0`.
- **The gateway does return `cache_creation_input_tokens`.** The reporter has a working local fix reading `raw.cache_creation_input_tokens`.

The same root cause was filed earlier for OpenRouter ([#18440](https://github.com/anomalyco/opencode/issues/18440), partially addressed by PR #22224) and affects DeepSeek and LiteLLM/Bedrock, which share the code path. The pattern recurs outside OpenCode too — [github/copilot-sdk#1073](https://github.com/github/copilot-sdk/issues/1073) reports cache fields never populated across all providers despite the APIs returning them — indicating a widespread client-side mapping gap, not a provider-side omission.

---

## Interpretation

The value is **not** lost at the provider boundary. The GitHub Copilot gateway reports cache-creation tokens; OpenCode's usage mapping never reads them for the OpenAI-compatible route and emits a hardcoded `0`. The data exists in the raw provider payload and is recoverable.

The earlier proxy-schema explanation rested on the absence of `providerMetadata` / `cacheCreation*` keys in the raw captures. That reasoning does not hold: those captures are written downstream of the mapping (see Evidence 3), so the keys' absence is exactly what a client-side discard would also produce. The observation was consistent with the hypothesis but had no power to discriminate between it and the alternative.

Practical consequence: the provider **is** caching correctly *and* **is** reporting what the cache cost. MOSAIC simply never receives the number, because OpenCode drops it first.

---

## Confidence and what remains unverified

| Claim | Confidence |
|---|---|
| Zero originates upstream of MOSAIC, not in the logger or analyzer | **Proven** — direct inspection of raw captures |
| Cache writes actually occur | **Proven** — monotonic read growth |
| MOSAIC pipeline handles non-zero values correctly | **Proven** — Claude Code logs |
| All affected runs use `github-copilot` | **Proven** — provider IDs in raw |
| Loss occurs in OpenCode's `usage()` mapping, not at the Copilot proxy | **Corroborated** — matches open upstream issue #36749 against our exact symptom |
| The gateway returns `cache_creation_input_tokens` on these runs | **Reported upstream, not confirmed on our traffic** |
| The value is recoverable inside MOSAIC's hook | **Unverified** — depends on whether the hook can reach the raw payload |

The last two rows are the remaining gaps. Upstream's claim that the gateway returns the field is credible and specific, but has not been checked against *our* Copilot endpoint and account tier.

**To verify (correctly):** capture the HTTP response body from the Copilot endpoint — via proxy or by logging `LanguageModelUsage.raw` — and look for `cache_creation_input_tokens`. Present ⇒ confirms the reading above and the value is recoverable today.

**Do not** use the experiment proposed in the earlier revision (running against a direct Anthropic provider and checking for non-zero `tokens.cache.write`). It would return non-zero, because the native-Anthropic fallback path in `usage()` exists and works — and that result would have been read as exonerating OpenCode and indicting the proxy. The test cannot separate the two hypotheses and would have confirmed the wrong one.

---

## Downstream impact

### Cost figures are understated

Cache writes bill at 1.25x input on Anthropic pricing. `pricing.yaml` supports a `cache_write` rate and `context_length_threshold` counts cache-creation tokens toward the long-context tier — both are silently fed zeros. The reported `$1.51` for run `a1f4` omits the cache-write line item entirely, and any long-context threshold crossing is under-detected.

Mitigating factor: Copilot bills in premium requests rather than tokens, so dollar figures for these runs are notional regardless.

This now carries more weight than when the loss was thought to be permanent: the numbers are **recoverable**, so the understatement is a fixable defect rather than an accepted limitation. It is also retroactively unfixable for existing runs — the value was discarded before MOSAIC ever saw it, so historical logs cannot be re-derived.

### "Measured zero" is conflated with "not reported"

This is the genuine MOSAIC-side defect surfaced by the investigation. The usage schema types `cache_creation_tokens` as a plain number, so absent data is indistinguishable from a real zero. The analyzer then renders `cache write: 0 tokens` with full confidence for a value that is actually unknown.

A fix would require distinguishing the two states — e.g. a nullable `cache_creation_tokens` plus a per-record completeness marker — and would ripple into the analyzer's rendering and pricing math. Deferred; not yet specified.

### Two unlike conditions render alike

In run `a1f4`, the analyzer shows both of these as `unpriced`:

- `contracts-designer#16` — real token counts present, but no `pricing.yaml` entry for the model. A **pricing-config gap**.
- `requirements-refinement#2` — no `usage_record` events at all (`04_session.raw` is 1111 bytes with no `tokens` object); the subagent session aborted before any assistant message landed. **Missing data**.

These are different problems with different remedies and should be reported differently.

---

## Conclusion

The MOSAIC logger's field mapping and the analyzer's aggregation are both correct and need no change — they faithfully report a zero that OpenCode hands them. But the earlier verdict of "no action required" was based on believing the data was gone at the provider boundary. It is not: OpenCode discards a value the gateway supplies, and that is fixable.

**Next steps, in order of leverage:**

1. **Determine the installed OpenCode version relative to PR #22224.** This decides whether the remedy is an upgrade or a patch, and is cheap to check. Do this first — it may make the rest moot.
2. **Confirm the field is present on our traffic** (see the corrected verification above) before investing in a workaround.
3. **Check whether `handlers_messages.ts` can reach the raw payload at hook time** — e.g. an `info.tokens.raw` or equivalent passthrough. If it can, MOSAIC can recover the value without waiting on upstream. If it cannot, the fix must come from OpenCode.

Two representational defects remain worth addressing independently of any of the above, since they are MOSAIC-side and would matter even with perfect provider data: absent usage data should not be reported as a measured zero, and pricing-config gaps should be distinguishable from missing telemetry.
