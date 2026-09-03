# Server-Side Prompt Caching Across Independent Processes

**Question:** Each AgentTest run is an independent harness process in its own throwaway sandbox. Yet turn-1 token accounting shows some runs writing a full ~28k-token prompt to cache while others, launched seconds later from a different suite, read ~26.5k of it back. Is the Anthropic prompt cache genuinely shared *between processes*, and if so, does reusing another process's KV entries invalidate the test?

**Answer:** Yes, it is shared, and no, it does not invalidate anything. The cache lives on Anthropic's inference servers, keyed by a hash of the prompt prefix and scoped to the API key — not to a process, session, or conversation. Reuse is memoization of a pure function: KV entries at position *i* depend only on tokens `0..i` and the weights, so a cache hit is numerically equivalent to recomputation. The cache holds the *prompt's* KV, never a previous run's generated tokens or sampling decisions.

**Status:** Demonstrated empirically against MOSAIC sandboxes. Mechanism is understood; the cost-optimisation opportunity it implies is **not yet exploited and needs design work** (see [Open Work](#open-work)).

**Why it matters:** Cache reads are priced at roughly 1/10th of fresh input and 1/12th of cache writes. A ~28k-token orchestrator system prompt costs about $0.105 to write and about $0.008 to read on sonnet-class pricing. Across a 10,000-run campaign the difference between "every run cold" and "every run warm" is on the order of **$1,000**. Scheduling is therefore a first-class cost lever, and it is currently unmanaged.

---

## Mechanism

Anthropic's prompt caching is a prefix cache:

- The cache key is a hash of the token sequence **from position 0**. The first token that differs invalidates the entire remainder.
- Entries are scoped to the API key / organisation. Any process presenting the same key and the same prefix hits the same entries.
- TTL is approximately 5 minutes, **refreshed on every hit**. A steadily-used prefix stays resident indefinitely; an idle one evicts.
- Writes are billed at a premium over base input (~1.25x); reads at a steep discount (~0.1x).

Three consequences that drive everything below:

1. **Position matters, but only for content that varies.** Injecting a variable block early in a system prompt forfeits caching for everything after it. If the block is constant for a given deployment, its position is irrelevant.
2. **Concurrency defeats warming.** Processes launched simultaneously all miss, because none has finished writing when the others start. Warming is inherently serial.
3. **Identical deployments across different suites share everything.** Suites are not cache boundaries. Deployments are.

---

## How to reproduce

All paths below are relative to a retained sandbox root (`--keep-sandbox`), written under the OS temp directory as `mosaic-agent-test-workspaces/{run_id}-{test_name}-{rep}/`. Nothing here depends on a specific historical run.

### Step 0 — capture a sweep with sandboxes retained

Run at least two different suites back-to-back, single repetition, same model and harness, in a single session. Two suites that deploy the **same** agent set are required for the cross-process result; add `infrastructure-triggers` if you also want to observe prefix divergence, since its per-test `infrastructure_agents` mutate the deployed orchestrator prompt.

Record the wall-clock start order of the suites. It is the independent variable.

### Step 1 — confirm the deployed prompts are byte-identical

The claim only holds if different suites really do send the same prefix. Verify rather than assume:

```bash
# from the temp workspaces root
md5sum */subject/.claude/agents/orchestrator.md | sort | uniq -c -w32

# and the full agent set, since all deployed agents contribute to the prompt
for d in */subject; do
  printf '%s ' "$d"; cat "$d/.claude/agents/"*.md | md5sum
done
```

Expect one hash shared by every suite that declares no infrastructure agents, and a distinct hash per distinct infrastructure-agent set.

Also confirm the prompt carries no per-run identifiers, which would break sharing:

```bash
grep -c "$RUN_ID" */subject/.claude/agents/orchestrator.md   # expect 0
```

If this returns non-zero, the prefix is run-scoped and no cross-run sharing is possible — stop here.

### Step 2 — extract turn-1 token accounting per run

The orchestrator event stream is:

```
{sandbox}/subject/OrchestrationLogs/{run_id}/00_orchestrator_events.jsonl
```

Filter for `event == "usage_record"`. Each record carries `token_usage` with `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens`, plus `record_id`, `record_index`, `model`, `timestamp`.

**Deduplicate by `record_id` before summing.** The stream re-emits each record two to five times as the turn progresses; `record_index` is also reused across turns and is not a safe key. Failing to dedupe inflates every figure by 2–5x.

Take the **first** record chronologically per run. That single record is the whole experiment:

- `cache_creation_tokens ≈ full prompt size`, `cache_read_tokens == 0` → cold, this process populated the cache
- `cache_creation_tokens` small, `cache_read_tokens` large → warm, this process reused another's entries
- both substantial → partial prefix match; the read count tells you exactly how many tokens matched before divergence

Sort runs by that first record's `timestamp` and tabulate alongside the suite name.

### Step 3 — read the result

The expected shape, for suites sharing one deployment:

| Position in sweep | cache_write (turn 1) | cache_read (turn 1) |
|---|---|---|
| First run(s), launched concurrently | full prompt | **0** |
| Every later run, any suite, any process | small remainder | **full shared prefix** |

Two diagnostics fall out for free:

- **Concurrent cold runs.** Runs launched within a second or two of each other all show `cache_read == 0`. This is the control: it proves the read is not an artefact of the client, since these processes are identical to the later ones in every respect except start time.
- **Divergence point.** For a suite whose prompt differs mid-file, `cache_read_tokens` on turn 1 equals the number of tokens **preceding the first difference**. Compare against `diff` on the deployed prompts to locate the injection site. `cache_read` is effectively a token-accurate ruler for where two prompts stop agreeing.

### Step 4 — confirm reuse is semantically inert

Worth doing once, because the intuition that "run 2 inherits run 1's computation" is a natural one:

- Compare output-token counts on turns 2+ between a cold run and a warm run of the same test. They differ, and by a lot. Behaviour is not inherited.
- Compare pass-rate variance between heavily-shared suites and suites with unique prompts. Expect **no** relationship in the direction the intuition predicts — suites with unique prompts (infrastructure-triggers) are not less stable than suites sharing the full prefix.
- Across repetitions of a single test, per-run cost still spans a wide range. Warm runs are not clones of each other.

---

## Why reuse does not compromise a test

K and V vectors for token position *i* are a deterministic function of tokens `0..i` and the model weights. Nothing else enters. Reusing them is therefore identical arithmetic to recomputing them — memoization, not approximation.

The distinction that resolves the intuition: **a cache hit is not "reusing another caller's answer"; it is skipping recomputation of an identical intermediate.** No sampled token, no logit, no RNG state from the first run is stored. The second run receives the same KV it would have computed itself, then samples fresh from its own distribution.

One honest caveat: cached and uncached paths are not guaranteed bit-identical, since differing kernel and batch shapes introduce floating-point noise. This sits at the same order of magnitude as ordinary sampling nondeterminism and is not a semantic shortcut. If a test's outcome is sensitive at that level, the test is measuring noise regardless of caching.

**What caching does compromise is cost comparison.** Per-test cost becomes a function of execution order. A suite that runs first pays the warming bill for every suite after it — observed at roughly 2x for the leading suite in a single-repetition sweep. Any cross-suite cost table drawn from a 1-rep run is measuring scheduling, not the tests.

---

## The optimisation opportunity

Currently nothing in the runner reasons about cache state. Four levers, roughly in order of expected value:

**1. Serial warm-up before parallel fan-out.** The single highest-value change. Today a sweep launches N runs concurrently, all of which miss and all of which pay a full cache write. Executing one run to first token, then fanning out, converts N-1 full writes into N-1 reads. At ~28k tokens that is roughly $0.10 saved per run beyond the first.

**2. Group by deployment, not by suite.** The cache boundary is the deployed agent set, which does not align with suite boundaries. Ordering the schedule so that all runs sharing a deployment hash execute contiguously keeps each prefix resident and hit-refreshed. Runs that alternate between deployments thrash.

**3. Respect the TTL.** Entries expire after roughly 5 minutes of no hits. A long-running campaign that returns to a deployment after a gap re-pays the write. Scheduling should either keep a prefix hot or accept the write once and not fragment.

**4. Keep variable content at the tail of the prompt.** Only relevant where injected content differs between otherwise-identical deployments — in MOSAIC, the `<InfrastructureAgent>` block. Moving a mid-prompt injection point to the tail converts a large forced write into a small one. Note this is a **test-harness** optimisation: a production deployment has a fixed agent set, so its prompt is a stable prefix and the injection position is irrelevant.

A rough ceiling: if a campaign currently pays a full cache write on most runs and could instead pay a read on nearly all of them, the saving approaches the write/read spread times run count. For a 28k prompt at 10,000 runs that is order $1,000 — comparable to the entire measured spend of a full campaign.

---

## Open work

Nothing below is settled; all of it needs design before implementation.

1. **Measure the real hit rate today.** The reproduction above establishes the mechanism on a handful of runs. What fraction of a full campaign currently starts cold is unknown, and it determines whether the ceiling above is $1,000 or $100. This is a straightforward aggregation over turn-1 records across all retained sandboxes of one campaign.

2. **Quantify the concurrency/warming trade-off.** Serial warm-up costs wall-clock. With a measured fixed per-run overhead of 37–48s on top of model time, delaying the fan-out by one full run may or may not pay for itself. Needs a model of (concurrency, warm-up policy) against both dollars and total campaign duration.

3. **Decide whether the runner should schedule by deployment hash.** Requires the runner to compute a stable hash of the deployed agent set before dispatch and to reorder the work queue. Interacts with existing suite-level parallelism and with result-file naming, which is currently suite-oriented.

4. **Determine TTL behaviour under load.** The ~5 minute figure is documented, not measured here. Whether it holds under sustained concurrent access, and whether hits genuinely refresh it, should be verified before a scheduler depends on it.

5. **Confirm the picture on opencode.** The results here are from the claude-code harness. OpenCode's usage mapping is known to drop cache-write counts entirely for OpenAI-compatible routes — see [CacheWriteTokensMissingOpenCode.md](CacheWriteTokensMissingOpenCode.md) — so turn-1 accounting there is not currently trustworthy enough to run the same experiment. That gap must close first.

6. **Separate cache effects from cost reporting in the summary.** As long as per-test cost is order-dependent and unlabelled, cost tables silently encode the schedule. Either report cold/warm state per run, or normalise it away.
