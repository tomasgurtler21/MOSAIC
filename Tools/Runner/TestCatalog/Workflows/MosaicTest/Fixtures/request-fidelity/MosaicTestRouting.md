---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: request-fidelity

The pre-consultation section carries the request-direction payload. The Runner appends it to every
auto-routed dispatch, so it travels Runner → harness CLI → agent inside the request's
`task_description`, which is the field serialised **before** `input_artifacts`. If the command line
is cut or split anywhere inside it, the script path is lost too and the stub fails loudly.

The advice deliberately contains what `cmd.exe` interprets on a command line: `%VAR%` expansion,
the `^` escape character, the `|` pipe, `!` (delayed expansion), double quotes, backslash runs and
a line break. Go's JSON encoder escapes `&`, `<` and `>` to `\u0026`, `\u003c`, `\u003e`, so they
cannot reach `cmd.exe` raw today; `&` is included anyway so a change to the encoder is noticed.
`<` and `>` are left out on purpose, because a raw `>` would make `cmd.exe` create stray files.

No rules: the single row's SUCCESS routes to COMPLETE through the engine in Auto mode. Any routing
consultation matches no rule and the stub stops, catching the unexpected consultation.

## Pre-Consultation
task_description:
~~~
MOSAICTEST-REQUEST-FIDELITY / percent: %OS% %PATH% 100%% / caret: a^b ^^ ^ / pipe: x | MOSAICTEST-PIPE-NOT-A-COMMAND / bang: !OS! / amp: a & b / quotes: "one" ""two"" \"three\" / backslashes: C:\AI\ \\server\share\ trailing\
second line of the advice / MOSAICTEST-REQUEST-FIDELITY-END
~~~
constraints:
~~~
MOSAICTEST-REQUEST-FIDELITY-CONSTRAINT / percent: %OS% / caret: ^ / pipe: |
~~~
