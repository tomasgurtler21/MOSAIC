# Known Limitations: OpenCode Adapter

This record documents external constraints and unresolved questions that affect
the OpenCode variant of `mosaic-logger`. These items were established as
unresolvable within this adapter's development constraints and are recorded here
so that future investigators do not rediscover them from scratch.

---

## Unintercepted tool calls inside subagent sessions

Tool calls made *inside* a session that OpenCode spawned as a subagent are not
intercepted by `tool.execute.before` or `tool.execute.after` at all. This is a
confirmed, open, unresolved bug in OpenCode itself (`sst/opencode#5894`), not a
defect in this adapter. The bug bounds tool-call logging coverage for nested
subagent activity regardless of anything done in this adapter: only the
dispatching `task` call on the parent session is visible to the hook surface.

## Dispatch-closure behaviour for the `task` call is unresolved

Whether OpenCode fires `tool.execute.after` for the *dispatching* `task` tool
call itself was never established during development. The adopted implementation
uses a dual-path closure registry: whichever signal arrives first (the real
after-hook or a synthetic close triggered by the subagent's `session.idle`)
closes the call exactly once, and the other path is a no-op. This design is
correct under either behaviour, but which path actually fires in production is
still unknown.

## `session.created` delivery reliability

OpenCode has a confirmed, open bug (`sst/opencode#14808`) where the
`session.created` bus event is sometimes not delivered to plugins at all.
Primary identity resolution was deliberately moved off this event and onto the
`task` dispatch payload for this reason. `session.created` remains a secondary
path only; any session whose `session.created` is silently dropped will still
have its invocation start resolved from the dispatch payload when the subagent
is launched.

## Externally sourced, unverified type shapes

Every OpenCode type shape used in this adapter — SDK message structures, hook
payload shapes, bus event fields — was obtained from AI-summarized reads of the
`sst/opencode` `dev` branch source and documentation. No `@opencode-ai/plugin`
or `@opencode-ai/sdk` package is installed in this repository, so nothing was
type-checked against a real interface. OpenCode's plugin API is under active
development. Field names may drift between versions, or differ from what was
documented at the time of implementation. All field accesses use optional
chaining and fallback cascades so that field-name drift degrades to absent data
rather than a crash.

## No runtime verification against a live install

No verification against a running OpenCode instance was performed, by explicit
decision. Correctness rests entirely on unit tests built against the
externally-sourced type shapes described above. A future integrator should run
the adapter against a real OpenCode process and compare the emitted events
against the expected schema before treating it as production-ready.

## Unconfirmed `tool.execute.after` output shape

The second parameter of the `tool.execute.after` hook is confirmed to be an
object, but its internal field names are unconfirmed. `deriveToolOutcome` probes
several plausible field names and omits `status` from `tool_call_end` when the
payload signals neither success nor failure. This means some `tool_call_end`
events will lack a `status` field that a consumer might expect. A confirmed
field name would allow the adapter to populate `status` reliably.

## `permission.ask` resolution reporting is unestablished

Whether OpenCode's `permission.ask` hook also reports the resolution of a
permission prompt (i.e., whether the hook fires a second time after the user
responds) is unestablished. The implementation defaults to the single-event
model: one `notification` event is emitted per `permission.ask` firing, with no
`notification_id` correlation key, because minting a correlation id that nothing
will ever pair with would be worse than omitting it. If the hook proves to fire
twice, the implementation should be updated to mint a `notification_id` and pair
the ask and resolution events.
