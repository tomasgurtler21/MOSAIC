---
mosaictest_script: 1
---

# MosaicTest Script: request-fidelity

Unconditional SUCCESS. Echoes the received task description.

The file name itself is part of the test. It contains the literal text `%OS%`. A request that
reaches the agent through `cmd.exe` (a Windows `.cmd`/`.bat` shim such as an npm-installed
`opencode.cmd` or `copilot.cmd`) has `%OS%` expanded to `Windows_NT`, so the path the stub receives
no longer names this file and it returns FIXTURE_ERROR. Reaching this script at all proves the
input artifact path arrived byte for byte.

The echoed task_description carries the pre-consultation advice, which is full of characters that
`cmd.exe` interprets. Compare the echo against the advice in MosaicTestRouting.md.

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 1 / RESEARCH / script path with literal %OS% arrived intact / received task_description: {task_description} / returning SUCCESS
~~~

### Write
none
