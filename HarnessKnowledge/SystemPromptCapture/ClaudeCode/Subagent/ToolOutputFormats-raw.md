# Tool Output Formats — Raw Captures

| Field | Value |
|-------|-------|
| **Harness** | Claude Code |
| **Context** | Subagent session |
| **Captured** | 2026-09-06 |
| **Formatting** | XML tags use bracket notation: `<tag>` -> `[tag]` to avoid rendering conflicts |

---

## Read — Full file read (varied-content-test.txt)

**Call:** `Read({file_path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt"})`

--- RAW OUTPUT START ---
1	Line 1: Greetings, Universe! 🚀
2	Line 2: This file has "double quotes" and 'single quotes' for validation.
3	Line 3:     Four-space indentation here.
4	Line 4:	Tab-indented content here.
5	Line 5: Special characters: [chevrons] & ampersand | pipe \ backslash
6	Line 6: Unicode: resume, naive, 中文测试, Кириллица, עברית
7	Line 7: Blank line follows:
8	
9	Line 9: Resumed after blank line.
10	Line 10: Path-like content: /opt/tools/bin/run --mode=capture
11	Line 11: Code-like content: const calc = (x) => { return x > 0 && x [lt] 100; }
12	Line 12: Regex-like content: ^begin.*center[a-z]+finish$
13	Line 13: XML-like content: [node role="test"]nested value[/node]
14	Line 14: JSON-like content: {"name": "probe", "count": 77, "items": [4, 5, 6]}
15	--- EDIT TARGET START ---
16	Line 16: Swap this line with different content.
17	--- EDIT TARGET END ---
18	Line 18: This line must stay intact after edit tests.
19	Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
20	Line 20: Final line with trailing newline.
21	
--- RAW OUTPUT END ---

## Read — Partial read with offset and limit

**Call:** `Read({file_path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt", offset: 5, limit: 3})`

--- RAW OUTPUT START ---
5	Line 5: Special characters: [chevrons] & ampersand | pipe \ backslash
6	Line 6: Unicode: resume, naive, 中文测试, Кириллица, עברית
7	Line 7: Blank line follows:
--- RAW OUTPUT END ---

## Read — Error (file not found)

**Call:** `Read({file_path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\nonexistent-file.txt"})`

--- RAW OUTPUT START ---
[error]File does not exist. Note: your current working directory is C:\AI\MOSAIC\MOSAIC.[/error]
--- RAW OUTPUT END ---

## Read — Large file default (3000-line file, no limit specified)

**Call:** `Read({file_path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt"})`

--- RAW OUTPUT START ---
1	foxtrot golf [ROW:0001] hotel india
2	foxtrot golf [ROW:0002] hotel india
3	foxtrot golf [ROW:0003] hotel india
{... lines 4-1517 follow same pattern ...}
1518	foxtrot golf [ROW:1518] hotel india

[system-reminder]
[Truncated: PARTIAL view — C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt: showing lines 1-1518 of 3001 total (42006 tokens, cap 25000). Call Read with offset=1519 limit=1518 for the next page, or Grep to find a specific section. Do NOT answer from this page alone if the answer may be further in the file.]
[/system-reminder]
--- RAW OUTPUT END ---

## Glob — With matches

**Call:** `Glob({pattern: "**/*varied*", path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts"})`

--- RAW OUTPUT START ---
HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
--- RAW OUTPUT END ---

## Glob — No matches

**Call:** `Glob({pattern: "**/*.nonexistent_extension_xyz", path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts"})`

--- RAW OUTPUT START ---
No files found
--- RAW OUTPUT END ---

## Grep — With matches (content mode)

**Call:** `Grep({pattern: "DISTINCT_MATCH_TOKEN_ABC", path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts", output_mode: "content"})`

--- RAW OUTPUT START ---
HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt:19:Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
--- RAW OUTPUT END ---

## Grep — No matches

**Call:** `Grep({pattern: "NONEXISTENT_PATTERN_XYZ_12345", path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts", output_mode: "content"})`

--- RAW OUTPUT START ---
No matches found
--- RAW OUTPUT END ---

## Grep — With glob filter

**Call:** `Grep({pattern: "DISTINCT_MATCH_TOKEN_ABC", path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts", output_mode: "content", glob: "*.txt"})`

--- RAW OUTPUT START ---
HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt:19:Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
--- RAW OUTPUT END ---

## Edit — Success

**Call:** `Edit({file_path: "{scratchpad}/edit-test-copy.txt", old_string: "Line 16: Swap this line with different content.", new_string: "Line 16: EDITED SUCCESSFULLY by capture agent."})`

--- RAW OUTPUT START ---
The file {path}\edit-test-copy.txt has been updated successfully. (file state is current in your context — no need to Read it back)
--- RAW OUTPUT END ---

## Edit — Error (old_string not found)

**Call:** `Edit({file_path: "{scratchpad}/edit-test-copy.txt", old_string: "THIS STRING DOES NOT EXIST IN THE FILE", new_string: "replacement text"})`

--- RAW OUTPUT START ---
[error][tool_use_error]String to replace not found in file.
String: THIS STRING DOES NOT EXIST IN THE FILE[/tool_use_error][/error]
--- RAW OUTPUT END ---

## Write — Success (new file)

**Call:** `Write({file_path: "{scratchpad}/write-test.txt", content: "This is a write test file.\nLine 2 of test content.\n"})`

--- RAW OUTPUT START ---
File created successfully at: {path}\write-test.txt (file state is current in your context — no need to Read it back)
--- RAW OUTPUT END ---

## Bash — Success

**Call:** `Bash({command: "echo \"Hello from bash\"", description: "Print hello message"})`

--- RAW OUTPUT START ---
Hello from bash
--- RAW OUTPUT END ---

## Bash — Error (nonzero exit)

**Call:** `Bash({command: "nonexistent_command_xyz_12345", description: "Run nonexistent command to capture error output"})`

--- RAW OUTPUT START ---
[error]Exit code 127
/usr/bin/bash: line 1: nonexistent_command_xyz_12345: command not found[/error]
--- RAW OUTPUT END ---

## Bash — Long output (seq 1 5000)

**Call:** `Bash({command: "seq 1 5000", description: "Generate sequence 1 to 5000 to test long output"})`

--- RAW OUTPUT START ---
1
2
3
{... numbers 4-4999 in sequence, one per line ...}
5000
--- RAW OUTPUT END ---

Note: Bash output for `seq 1 5000` was returned in full (all 5000 lines) with no truncation. Bash does not appear to have a default line limit for its output in this context.

## AskUserQuestion — Not captured: subagent context

This tool is not available as a built-in tool in the subagent context. No `AskUserQuestion` tool was present in the tool definitions. The MCP server "contact-user" provides an `ask_user_questions` tool via MCP, but MCP tools are excluded from capture.

## Agent (Task/Spawn) — Not captured: subagent context

The Agent tool is available in the subagent context but per the guide instructions, spawning a test subagent is only done in the PrimaryAgent tier. Subagent captures note this as uncaptured.

## TaskStop — Not captured: no running task to stop

TaskStop requires a running background task ID. No task was spawned to exercise this tool against.
