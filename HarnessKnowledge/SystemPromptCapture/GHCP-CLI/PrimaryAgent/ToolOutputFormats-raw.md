# Tool Output Formats — Raw Captures

| Field | Value |
|-------|-------|
| **Harness** | GHCP CLI (GitHub Copilot CLI) |
| **Context** | Primary agent session |
| **Captured** | 2026-09-06 |
| **Formatting** | XML tags use bracket notation: `<tag>` -> `[tag]` to avoid rendering conflicts |

---

## view — full file read (varied-content-test.txt)

**Call:** `view(path: "TestArtifacts/varied-content-test.txt")`

--- RAW OUTPUT START ---
Line 1: Greetings, Universe! 🚀
Line 2: This file has "double quotes" and 'single quotes' for validation.
Line 3:     Four-space indentation here.
Line 4:	Tab-indented content here.
Line 5: Special characters: <chevrons> & ampersand | pipe \ backslash
Line 6: Unicode: resume, naive, 中文测试, Кириллица, עברית
Line 7: Blank line follows:

Line 9: Resumed after blank line.
Line 10: Path-like content: /opt/tools/bin/run --mode=capture
Line 11: Code-like content: const calc = (x) => { return x > 0 && x < 100; }
Line 12: Regex-like content: ^begin.*center[a-z]+finish$
Line 13: XML-like content: <node role="test">nested value</node>
Line 14: JSON-like content: {"name": "probe", "count": 77, "items": [4, 5, 6]}
--- EDIT TARGET START ---
Line 16: Swap this line with different content.
--- EDIT TARGET END ---
Line 18: This line must stay intact after edit tests.
Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
Line 20: Final line with trailing newline.
--- RAW OUTPUT END ---

## view — partial read with view_range [5, 9]

**Call:** `view(path: "TestArtifacts/varied-content-test.txt", view_range: [5, 9])`

--- RAW OUTPUT START ---
Line 5: Special characters: <chevrons> & ampersand | pipe \ backslash
Line 6: Unicode: resume, naive, 中文测试, Кириллица, עברית
Line 7: Blank line follows:

Line 9: Resumed after blank line.
--- RAW OUTPUT END ---

## view — directory listing

**Call:** `view(path: "TestArtifacts")`

--- RAW OUTPUT START ---
large-file-3000-lines.txt
varied-content-test.txt
--- RAW OUTPUT END ---

## view — error: file not found

**Call:** `view(path: "TestArtifacts/does-not-exist.txt")`

--- RAW OUTPUT START ---
Path C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\does-not-exist.txt does not exist. Please provide a valid path.
--- RAW OUTPUT END ---

## view — large file, default parameters (no limit specified)

**Call:** `view(path: "TestArtifacts/large-file-3000-lines.txt")`

--- RAW OUTPUT START ---
File too large to read at once (108.4 KB). Consider using the grep tool to search within the file, or view with view_range to read specific sections.
--- RAW OUTPUT END ---

## glob — with matches

**Call:** `glob(paths: "TestArtifacts", pattern: "*.txt")`

--- RAW OUTPUT START ---
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt
--- RAW OUTPUT END ---

## glob — no matches

**Call:** `glob(paths: "TestArtifacts", pattern: "*.nonexistent-ext-zzz")`

--- RAW OUTPUT START ---
No files matched the pattern.
--- RAW OUTPUT END ---

## grep — with matches (output_mode: content)

**Call:** `grep(paths: "TestArtifacts/varied-content-test.txt", pattern: "DISTINCT_MATCH_TOKEN_ABC", output_mode: "content")`

--- RAW OUTPUT START ---
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt:Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
--- RAW OUTPUT END ---

## grep — no matches

**Call:** `grep(paths: "TestArtifacts", pattern: "NO_SUCH_TOKEN_WILL_MATCH_XYZ", output_mode: "content")`

--- RAW OUTPUT START ---
No matches found.
--- RAW OUTPUT END ---

## grep — with include filter (glob param), files_with_matches mode

**Call:** `grep(paths: "TestArtifacts", pattern: "Line", glob: "*.txt", output_mode: "files_with_matches")`

--- RAW OUTPUT START ---
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
--- RAW OUTPUT END ---

## grep — anomaly: `-n: true` parameter triggers a repo-settings hook denial

**Call:** `grep(paths: "TestArtifacts/varied-content-test.txt", pattern: "DISTINCT_MATCH_TOKEN_ABC", output_mode: "content", -n: true)`

--- RAW OUTPUT START ---
Denied by preToolUse hook from "repo settings" (hook errored)
--- RAW OUTPUT END ---

## edit — success

**Call:** `edit(path: "_temp-edit-test.txt", old_str: "Line 16: Swap this line with different content.", new_str: "Line 16: This content was swapped by the edit tool test.")`

--- RAW OUTPUT START ---
File C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\PrimaryAgent\_temp-edit-test.txt updated with changes.
--- RAW OUTPUT END ---

## edit — error: old_str not found

**Call:** `edit(path: "_temp-edit-test.txt", old_str: "This string absolutely does not appear anywhere in the target file XYZZY_NONEXISTENT_STRING_12345", new_str: "This text does not exist in the file.")`

--- RAW OUTPUT START ---
No match found: old_str not found in C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\PrimaryAgent\_temp-edit-test.txt; no changes made; you might want to try again with correct old_str
--- RAW OUTPUT END ---

## create — success

**Call:** `create(path: "_temp-create-test.txt", file_text: "This is a temporary file created to capture the create/write tool output format.\n")`

--- RAW OUTPUT START ---
Created file C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\PrimaryAgent\_temp-create-test.txt with 82 characters
--- RAW OUTPUT END ---

## powershell — success

**Call:** `powershell(command: "Write-Output \"hello-from-powershell-test\"", description: "Simple successful powershell command for capture")`

--- RAW OUTPUT START ---
hello-from-powershell-test
<shellId: 28 completed with exit code 0>
--- RAW OUTPUT END ---

## powershell — error: nonzero exit code

**Call:** `powershell(command: "exit 7", description: "Nonzero exit code command for capture")`

--- RAW OUTPUT START ---

<shellId: 27 completed with exit code 7>
--- RAW OUTPUT END ---

## powershell — long output (seq-equivalent: 1..5000)

**Call:** `powershell(command: "1..5000 | ForEach-Object { \"line-$_\" }", description: "Long output command for capture", initial_wait: 30)`

--- RAW OUTPUT START ---
Output too large to read at once (47.7 KB). Saved to: C:\Users\tgurt\AppData\Local\Temp\1788726941240-copilot-tool-output-50748-cf0dfbcc-2f8b-48bd-8fa4-c9538bcfd255.txt
Consider using tools like grep (for searching), head/tail (for viewing start/end), view with view_range (for specific sections), or jq (for JSON) to examine portions of the output.

Preview (first 500 chars):
line-1
line-2
line-3
line-4
line-5
line-6
line-7
line-8
line-9
line-10
line-11
line-12
line-13
line-14
line-15
line-16
line-17
line-18
line-19
line-20
line-21
line-22
line-23
line-24
line-25
line-26
line-27
line-28
line-29
line-30
line-31
line-32
line-33
line-34
line-35
line-36
line-37
line-38
line-39
line-40
line-41
line-42
line-43
line-44
line-45
line-46
line-47
line-48
line-49
line-50
line-51
line-52
line-53
line-54
line-55
line-56
line-57
line-58
line-59
line-60
line-61
line-62
line-63
line-
<shellId: 29 completed with exit code 0>
--- RAW OUTPUT END ---

## ask_user — test question

**Call:** `ask_user(message: "This is a test question for the System Prompt Capturer's Tool Output Format Capture step — capturing how the ask_user tool renders a simple form. You can answer with anything, or decline.", requestedSchema: {"properties": {"test_response": {"type": "string", "title": "Test response", "description": "Any text response to exercise the ask_user output format capture."}}})`

--- RAW OUTPUT START ---
User responded: this is user response, hello
--- RAW OUTPUT END ---

## task — spawn minimal subagent (Primary agent tier only)

**Call:** `task(agent_type: "explore", name: "Capture test subagent", description: "Minimal test task for capture", mode: "sync", prompt: "This is a minimal test task for the System Prompt Capturer's Tool Output Format Capture step. Simply reply with the exact text: CAPTURE_TEST_ACK. Do not use any tools.")`

--- RAW OUTPUT START ---
CAPTURE_TEST_ACK
--- RAW OUTPUT END ---
