# TOML Library Spike Outcome

## Chosen library

`github.com/pelletier/go-toml/v2` (v2.4.3)

## Fallback

`github.com/BurntSushi/toml` was evaluated as fallback. It was not chosen because
`pelletier/go-toml/v2` satisfies all criteria and has stricter duplicate-key error
behaviour, which is required by the decode contract (`ErrMalformedDeployed` for
duplicate keys).

## Hostile round-trip

Verified in `toml_spike_verify_test.go`: hostile scalar content (embedded newlines,
unicode, backslash sequences, escape sequences) round-trips correctly. The library
parses multi-line basic strings (`"""`) and single-line basic strings with full
escape processing. Values decode to byte-identical string values when re-read.

## Parse failures as errors (not partial values)

Verified: syntax errors and duplicate keys both surface as non-nil errors from
`toml.Unmarshal`. The library does not silently accept invalid TOML or silently
take the last of duplicate keys.

## Transitive dependencies

None. `github.com/pelletier/go-toml/v2` has no transitive module dependencies.
`go mod tidy` confirms the workspace resolves cleanly with only this module added.

## Source-text positioning availability

**Not available.** `pelletier/go-toml/v2` provides no API to recover the source
file position (line/column) of a decoded value. The prior-bytes channel for
marker-carried user keys must be implemented using a hand-written span locator
that scans the raw TOML bytes to locate each key's value span.

The carriage stage must build that locator; this stage records the finding so
the carriage stage knows which path to take.

## Scalar source-spelling recovery

**Not recoverable.** The following spellings were each probed (see
`TestTOMLSpike_ScalarSpellingProbe` in `toml_spike_verify_test.go`):

| Source spelling | Decoded Go type | Decoded value | Spelling recoverable? |
|-----------------|-----------------|---------------|-----------------------|
| `0x1F`          | `int64`         | `31`          | **No** |
| `1_000`         | `int64`         | `1000`        | **No** |
| `+5`            | `int64`         | `5`           | **No** |
| `inf`           | `float64`       | `+Inf`        | **No** |
| `nan`           | `float64`       | `NaN`         | **No** |
| `42`            | `int64`         | `42`          | No (value matches, but only coincidentally) |

**Verdict: scalar source spelling is NOT recoverable.**

Integer, float, and boolean user keys whose source uses exotic spellings (`0x1F`,
`1_000`, `+5`, `inf`, `nan`) must be escalated to marker carriage (the prior-bytes
channel), not value-carried. The carriage stage must implement marker handling for
these cases. A plain decimal integer whose value can be re-expressed without loss
(e.g. `42`) may be value-carried, but its quote style in the carriage container
(unquoted TOML integer) is the only evidence of its original type, not of its
original spelling.
