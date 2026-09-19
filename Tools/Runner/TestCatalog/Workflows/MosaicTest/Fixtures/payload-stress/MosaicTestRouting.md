---
mosaictest_routing: 1
---

# MosaicTest Routing Fixture: payload-stress

Four rules. Three dispatches (one per EXECUTION row in the single stage), then a stop.

Each task description carries content from the same payload class as the row it dispatches,
so the orchestrator-to-runner relay is itself a payload fidelity channel under test. The
subagent's script controls what goes into `status_message`; the task description here controls
what goes through the orchestrator response parse path. They exercise different code.

## Pre-Consultation
none

## Rule: run-start

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-PAYLOAD-STRESS-ONE / orchestrator relay unicode: ünïcøde ✓ 日本語 Ελληνικά Кириллица 🧩🔧 / combining: é vs é / rtl: مرحبا / dispatch row 1
~~~

### Overrides
none

## Rule: after mosaictest-scripted SUCCESS #1

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-PAYLOAD-STRESS-TWO / orchestrator relay fences: inline `code` and ``double `nested` backticks`` / a whole fenced block follows:
```json
{"this": "is inside a fenced block inside task_description"}
```
and a stray unbalanced fence: ``` / dispatch row 2
~~~

### Overrides
none

## Rule: after mosaictest-scripted SUCCESS #2

### Action
dispatch

### Agent
mosaictest-scripted

### TaskDescription
~~~
MOSAICTEST-PAYLOAD-STRESS-THREE / orchestrator relay json-in-json: {"status_code":"SUCCESS","nested":{"quote":"he said \"hi\"","backslash":"C:\\AI\\MOSAIC","newline":"a\nb"},"arr":[1,2,3]} / dispatch row 3
~~~

### Overrides
none

## Rule: after mosaictest-scripted SUCCESS #3

### Action
stop

### Reason
~~~
MOSAICTEST-ROUTING-COMPLETE / three payload dispatches done as scripted / orchestrator relay carried unicode, fenced backticks, and JSON-in-JSON / ending the run
~~~
