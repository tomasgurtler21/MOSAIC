---
mosaictest_script: 1
---

# MosaicTest Script: payload-fences

## Selector
none

## Outcome: always

### Status
SUCCESS

### Message
~~~
row 2 / EXECUTION / payload-stress / inline `code` and ``double `nested` backticks`` / a whole fenced block follows:
```json
{"this": "is inside a fenced block inside status_message"}
```
and a stray unbalanced fence: ``` / returning SUCCESS
~~~

### Write
none
