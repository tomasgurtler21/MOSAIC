# MosaicTest Extra Input

Dummy artifact seeded alongside the script fixture. Its presence in the dispatched `input_artifacts`
list — visible in the Execution Log's Inputs column — proves the orchestrator's `input_artifacts`
override reached the Runner and was applied.

The stub ignores this file: it reads only the one path containing `MosaicTestScript/`.
