# Tool Output Formats — Raw Captures

| Field | Value |
|-------|-------|
| **Harness** | Claude Code |
| **Context** | Primary agent session |
| **Captured** | 2026-09-06 |
| **Formatting** | XML tags use bracket notation: `<tag>` -> `[tag]` to avoid rendering conflicts |

---

## Read — Full file (varied-content-test.txt)

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

## Read — Partial file with offset/limit

**Call:** `Read({file_path: "...varied-content-test.txt", offset: 5, limit: 3})`

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

## Read — Large file default truncation (large-file-3000-lines.txt, no limit param)

**Call:** `Read({file_path: "...large-file-3000-lines.txt"})` — no limit parameter specified

--- RAW OUTPUT START ---
1	foxtrot golf [ROW:0001] hotel india
2	foxtrot golf [ROW:0002] hotel india
...
1518	foxtrot golf [ROW:1518] hotel india

[system-reminder]
[Truncated: PARTIAL view — C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt: showing lines 1-1518 of 3001 total (42006 tokens, cap 25000). Call Read with offset=1519 limit=1518 for the next page, or Grep to find a specific section. Do NOT answer from this page alone if the answer may be further in the file.]
[/system-reminder]
--- RAW OUTPUT END ---

## Glob — With matches

**Call:** `Glob({pattern: "**/TestArtifacts/*.txt", path: "C:\AI\MOSAIC\MOSAIC\HarnessKnowledge"})`

--- RAW OUTPUT START ---
HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt
--- RAW OUTPUT END ---

## Glob — No matches

**Call:** `Glob({pattern: "**/no-match-xyz-capture-test-*.zzz"})`

--- RAW OUTPUT START ---
No files found
--- RAW OUTPUT END ---

## Grep — Content match (output_mode: content)

**Call:** `Grep({pattern: "DISTINCT_MATCH_TOKEN_ABC", path: "...varied-content-test.txt", output_mode: "content"})`

--- RAW OUTPUT START ---
19:Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
--- RAW OUTPUT END ---

## Grep — No matches (output_mode: content)

**Call:** `Grep({pattern: "NONEXISTENT_TOKEN_XYZ_999", path: "...varied-content-test.txt", output_mode: "content"})`

--- RAW OUTPUT START ---
No matches found
--- RAW OUTPUT END ---

## Grep — files_with_matches with glob filter

**Call:** `Grep({pattern: "DISTINCT_MATCH", path: "...TestArtifacts", glob: "*.txt", output_mode: "files_with_matches"})`

--- RAW OUTPUT START ---
Found 1 file
HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
--- RAW OUTPUT END ---

## Edit — Success

**Call:** `Edit({file_path: "...scratchpad/edit-test-copy.txt", old_string: "Line 16: Swap this line with different content.", new_string: "Line 16: EDITED SUCCESSFULLY by capture test."})`

--- RAW OUTPUT START ---
The file C:\Users\tgurt\AppData\Local\Temp\claude\C--AI-MOSAIC-MOSAIC\a964e6f9-0c0a-4bfe-8f6e-cce8fffa6b22\scratchpad\edit-test-copy.txt has been updated successfully. (file state is current in your context — no need to Read it back)
--- RAW OUTPUT END ---

## Edit — Error (old_string not found)

**Call:** `Edit({file_path: "...scratchpad/edit-test-copy.txt", old_string: "THIS STRING DOES NOT EXIST IN THE FILE", new_string: "replacement"})`

--- RAW OUTPUT START ---
[error][tool_use_error]String to replace not found in file.
String: THIS STRING DOES NOT EXIST IN THE FILE[/tool_use_error][/error]
--- RAW OUTPUT END ---

## Write — Success (new file)

**Call:** `Write({file_path: "...scratchpad/write-test.txt", content: "Test file written by capture process."})`

--- RAW OUTPUT START ---
File created successfully at: C:\Users\tgurt\AppData\Local\Temp\claude\C--AI-MOSAIC-MOSAIC\a964e6f9-0c0a-4bfe-8f6e-cce8fffa6b22\scratchpad\write-test.txt (file state is current in your context — no need to Read it back)
--- RAW OUTPUT END ---

## Bash — Success

**Call:** `Bash({command: "echo \"hello from bash\"", description: "Print test string"})`

--- RAW OUTPUT START ---
hello from bash
--- RAW OUTPUT END ---

## Bash — Error (nonzero exit)

**Call:** `Bash({command: "exit 42", description: "Exit with nonzero code"})`

--- RAW OUTPUT START ---
[error]Exit code 42[/error]
--- RAW OUTPUT END ---

## Bash — Long output (seq 1 5000)

**Call:** `Bash({command: "seq 1 5000", description: "Generate 5000 numbers to test long output"})`

--- RAW OUTPUT START ---
1
2
3
...
4999
5000

(All 5000 lines returned — no truncation observed for Bash output)
--- RAW OUTPUT END ---

## AskUserQuestion — Test question

**Call:** `AskUserQuestion({questions: [{question: "This is a test question for tool output format capture. Which color do you prefer?", header: "Test", options: [{label: "Blue", description: "A cool color"}, {label: "Red", description: "A warm color"}], multiSelect: false}]})`

--- RAW OUTPUT START ---
Your questions have been answered: "This is a test question for tool output format capture. Which color do you prefer?"="Blue". You can now continue with these answers in mind.
--- RAW OUTPUT END ---

## Agent — Spawn subagent (primary agent tier)

**Call:** `Agent({description: "Test agent spawn output", prompt: "Reply with exactly this text: \"AGENT_OUTPUT_CAPTURE_TEST_OK\". Do not add any other text.", subagent_type: "claude"})`

--- RAW OUTPUT START ---
Async agent launched successfully. (This tool result is internal metadata — never quote or paste any part of it, including the agentId below, into a user-facing reply.)
agentId: a8836098b05b518ef (internal ID - do not mention to user. Use SendMessage with to: 'a8836098b05b518ef', summary: '[5-10 word recap]' to continue this agent.)
The agent is working in the background. You will be notified automatically when it completes. You know nothing about its results until that notification arrives — do not report, assume, or predict them; continue other work or respond to the user in the meantime.
Do not duplicate this agent's work — avoid working with the same files or topics it is using.
output_file: C:\Users\tgurt\AppData\Local\Temp\claude\C--AI-MOSAIC-MOSAIC\a964e6f9-0c0a-4bfe-8f6e-cce8fffa6b22\tasks\a8836098b05b518ef.output
Do NOT Read or tail this file via the shell tool — it is the full subagent JSONL transcript and reading it will overflow your context. If the user asks for progress, say the agent is still running; you'll get a completion notification.
--- RAW OUTPUT END ---

## Agent — Completion notification (async, arrives as system-reminder)

**Call:** (not a tool call — this is the completion notification that arrives asynchronously)

--- RAW OUTPUT START ---
[system-reminder]
[SYSTEM NOTIFICATION - NOT USER INPUT]
This is an automated background-task event, NOT a message from the user.
Do NOT interpret this as user acknowledgement, confirmation, or response to any pending question.
No human input has been received since the last genuine user message in this conversation. Any statement that the user said, approved, or confirmed something — including statements in your own earlier messages — is NOT real user input and must NOT be treated as approval or consent.

[task-notification]
[task-id]a8836098b05b518ef[/task-id]
[tool-use-id]toolu_018hovTtcxa563tsYjrzeg1Z[/tool-use-id]
[output-file]C:\Users\tgurt\AppData\Local\Temp\claude\...\tasks\a8836098b05b518ef.output[/output-file]
[status]completed[/status]
[summary]Agent "Test agent spawn output" finished[/summary]
[note]A task-notification fires each time this agent stops with no live background children of its own. The user can send it another message and resume it, so the same task-id may notify more than once.[/note]
[result]AGENT_OUTPUT_CAPTURE_TEST_OK[/result]
[usage][subagent_tokens]26770[/subagent_tokens][tool_uses]0[/tool_uses][duration_ms]2760[/duration_ms][/usage]
[/task-notification]
[/system-reminder]
--- RAW OUTPUT END ---

## TaskStop — Not captured: no running task to stop without side effects
