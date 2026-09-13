# Tool Output Formats — Raw Captures

| Field | Value |
|-------|-------|
| **Harness** | GitHub Copilot CLI (GHCP CLI) |
| **Context** | Subagent session |
| **Captured** | 2026-09-06 |
| **Formatting** | XML tags use bracket notation: `<tag>` -> `[tag]` to avoid rendering conflicts |

---

## Wrapper format note

All tool call results observed in this session's rendered context appear wrapped as:

```
[result]
[name]{tool name}[/name]
[output]{tool output text}[/output]
[/result]
```

This is the wrapper as visible to this agent. It could not be confirmed whether this is the harness's literal internal XML wrapper or a display rendering of an underlying format, since the raw pre-render wrapper is not directly inspectable from within a normal conversation turn.

---

## view — success: full file read (small file)

**Call:** `view({path: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/varied-content-test.txt"})`

--- RAW OUTPUT START ---
[result]
[name]view[/name]
[output]Line 1: Greetings, Universe! 🚀
Line 2: This file has "double quotes" and 'single quotes' for validation.
Line 3:     Four-space indentation here.
Line 4:	Tab-indented content here.
Line 5: Special characters: [chevrons] & ampersand | pipe \ backslash
Line 6: Unicode: resume, naive, 中文测试, Кириллица, עברית
Line 7: Blank line follows:

Line 9: Resumed after blank line.
Line 10: Path-like content: /opt/tools/bin/run --mode=capture
Line 11: Code-like content: const calc = (x) => { return x > 0 && x < 100; }
Line 12: Regex-like content: ^begin.*center[a-z]+finish$
Line 13: XML-like content: [node role="test"]nested value[/node]
Line 14: JSON-like content: {"name": "probe", "count": 77, "items": [4, 5, 6]}
--- EDIT TARGET START ---
Line 16: Swap this line with different content.
--- EDIT TARGET END ---
Line 18: This line must stay intact after edit tests.
Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
Line 20: Final line with trailing newline.[/output]
[/result]
--- RAW OUTPUT END ---

Note: `<` and `>` characters appearing IN the file's own content (e.g. `<chevrons>`, `<node role="test">`) were also rendered to this agent using bracket notation ([ ]), the same as literal XML tags. It is not fully certain from this vantage point whether the harness itself bracket-escapes angle brackets in tool output, or whether this is an artifact of this capture agent's own rendering layer. Flagged as an open question.

---

## view — success: partial file read via view_range (offset/limit equivalent)

**Call:** `view({path: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/large-file-3000-lines.txt", view_range: [1, 30]})`

--- RAW OUTPUT START ---
[result]
[name]view[/name]
[output]foxtrot golf [ROW:0001] hotel india
foxtrot golf [ROW:0002] hotel india
foxtrot golf [ROW:0003] hotel india
foxtrot golf [ROW:0004] hotel india
foxtrot golf [ROW:0005] hotel india
foxtrot golf [ROW:0006] hotel india
foxtrot golf [ROW:0007] hotel india
foxtrot golf [ROW:0008] hotel india
foxtrot golf [ROW:0009] hotel india
foxtrot golf [ROW:0010] hotel india
foxtrot golf [ROW:0011] hotel india
foxtrot golf [ROW:0012] hotel india
foxtrot golf [ROW:0013] hotel india
foxtrot golf [ROW:0014] hotel india
foxtrot golf [ROW:0015] hotel india
foxtrot golf [ROW:0016] hotel india
foxtrot golf [ROW:0017] hotel india
foxtrot golf [ROW:0018] hotel india
foxtrot golf [ROW:0019] hotel india
foxtrot golf [ROW:0020] hotel india
foxtrot golf [ROW:0021] hotel india
foxtrot golf [ROW:0022] hotel india
foxtrot golf [ROW:0023] hotel india
foxtrot golf [ROW:0024] hotel india
foxtrot golf [ROW:0025] hotel india
foxtrot golf [ROW:0026] hotel india
foxtrot golf [ROW:0027] hotel india
foxtrot golf [ROW:0028] hotel india
foxtrot golf [ROW:0029] hotel india
foxtrot golf [ROW:0030] hotel india[/output]
[/result]
--- RAW OUTPUT END ---

No line-number prefixes or truncation markers were added by the tool for this range read — output is the bare file content for the requested range.

---

## view — directory listing

**Call:** `view({path: "HarnessKnowledge/SystemPromptCapture/TestArtifacts"})`

--- RAW OUTPUT START ---
[result]
[name]view[/name]
[output]large-file-3000-lines.txt
varied-content-test.txt[/output]
[/result]
--- RAW OUTPUT END ---

Note: directory listing is a bare newline-separated list of entry names (no type markers, sizes, or trailing slashes for subdirectories were present in this flat two-file directory — behavior for nested subdirectories was not exercised here).

---

## view — error: file not found

**Call:** `view({path: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/does-not-exist.txt"})`

--- RAW OUTPUT START ---
[result]
[name]view[/name]
[output]Path C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\does-not-exist.txt does not exist. Please provide a valid path.[/output]
[/result]
--- RAW OUTPUT END ---

Note: the error message echoes back the fully resolved absolute Windows path, not the relative path given in the call.

---

## view — large file with default parameters (no view_range specified)

**Call:** `view({path: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/large-file-3000-lines.txt"})`

--- RAW OUTPUT START ---
[result]
[name]view[/name]
[output]File too large to read at once (108.4 KB). Consider using the grep tool to search within the file, or view with view_range to read specific sections.[/output]
[/result]
--- RAW OUTPUT END ---

Note: this harness's `view` tool does NOT truncate a large file with a default line limit and partial content the way the guide's generic "read" tool test case expects. Instead, for files it judges too large (documented threshold: >20KB) it refuses entirely and returns only this short guidance message, no file content at all, unless `view_range` is supplied or `forceReadLargeFiles: true` is passed. There is no visible "first N lines then truncated" behavior for `view` without parameters.

---

## glob — with matches

**Call:** `glob({pattern: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/*.txt"})`

--- RAW OUTPUT START ---
[result]
[name]glob[/name]
[output].\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
.\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt[/output]
[/result]
--- RAW OUTPUT END ---

Note: paths are returned in Windows-style backslash form with a leading `.\`, not in the forward-slash form the pattern was given in, and not sorted alphabetically (large-file appears after varied-content, i.e. not lexicographic order — likely filesystem enumeration order or recency).

---

## glob — no matches

**Call:** `glob({pattern: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/*.nonexistent-ext"})`

--- RAW OUTPUT START ---
[result]
[name]glob[/name]
[output]No files matched the pattern.[/output]
[/result]
--- RAW OUTPUT END ---

---

## grep — success: with matches (content mode)

**Call:** `grep({pattern: "DISTINCT_MATCH_TOKEN_ABC", paths: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/varied-content-test.txt", output_mode: "content"})`

--- RAW OUTPUT START ---
HarnessKnowledge/SystemPromptCapture/TestArtifacts/varied-content-test.txt:Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
--- RAW OUTPUT END ---

Note: this call's raw output was NOT wrapped in the `[result][name]...[/name][output]...[/output][/result]` structure seen for other tools in this rendering — it appeared as bare text (`{file path}:{matching line}`). This may indicate grep results render differently, or the wrapper is stripped for single-line plain-text tool outputs in this display layer.

---

## grep — no matches

**Call:** `grep({pattern: "NO_SUCH_TOKEN_XYZ_ZZZ", paths: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/varied-content-test.txt"})`

--- RAW OUTPUT START ---
[result]
[name]grep[/name]
[output]No matches found.[/output]
[/result]
--- RAW OUTPUT END ---

---

## grep — with glob include filter, count mode

**Call:** `grep({pattern: "hotel", paths: "HarnessKnowledge/SystemPromptCapture/TestArtifacts", glob: "*.txt", output_mode: "count"})`

--- RAW OUTPUT START ---
[result]
[name]grep[/name]
[output]HarnessKnowledge/SystemPromptCapture/TestArtifacts\large-file-3000-lines.txt:3000[/output]
[/result]
--- RAW OUTPUT END ---

Note: only large-file-3000-lines.txt matched "hotel" (3000 occurrences, one per line); varied-content-test.txt does not contain "hotel" so it is silently omitted from count-mode output rather than shown with a 0 count. Also note the mixed path separator style: forward slashes from the supplied `paths` root, backslash before the filename.

---

## grep — error/blocked: denied by preToolUse hook (using `-n: true` parameter)

**Call:** `grep({pattern: "DISTINCT_MATCH_TOKEN_ABC", paths: "HarnessKnowledge/SystemPromptCapture/TestArtifacts/varied-content-test.txt", output_mode: "content", "-n": true})`

--- RAW OUTPUT START ---
[result]
[name]grep[/name]
[output]Denied by preToolUse hook from "repo settings" (hook errored)[/output]
[/result]
--- RAW OUTPUT END ---

Note: this exact call, re-issued moments later with identical parameters, was denied again with the same message (confirmed reproducible — not a one-off flake). The IDENTICAL call without the `-n: true` parameter succeeded (see the "with matches" case above). This indicates a repo-level `preToolUse` hook configured in this workspace specifically intercepts/rejects grep calls requesting line numbers (`-n: true`), and the hook itself errored while evaluating (message says "hook errored", implying the denial may be a fail-closed default rather than an intentional policy match). This is workspace/repo-specific hook behavior, not a general harness default — flagged for awareness but may not be reproducible in other workspaces.

---

## edit — success

**Call:** `edit({path: "...\\_tmp_edit_test.txt", old_str: "Line 16: Swap this line with different content.", new_str: "Line 16: REPLACED CONTENT FOR EDIT TEST."})`

--- RAW OUTPUT START ---
[result]
[name]edit[/name]
[output]File C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\Subagent\_tmp_edit_test.txt updated with changes.[/output]
[/result]
--- RAW OUTPUT END ---

---

## edit — error: old_str not found

**Call:** `edit({path: "...\\_tmp_edit_test.txt", old_str: "THIS STRING DOES NOT EXIST IN THE FILE AT ALL", new_str: "irrelevant replacement"})`

--- RAW OUTPUT START ---
[result]
[name]edit[/name]
[output]No match found: old_str not found in C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\Subagent\_tmp_edit_test.txt; no changes made; you might want to try again with correct old_str[/output]
[/result]
--- RAW OUTPUT END ---

(Temp file `_tmp_edit_test.txt` was deleted after this test — see cleanup note at end of document.)

---

## create ("write" tool equivalent) — success

**Call:** `create({path: "...\\_tmp_create_test.txt", file_text: "temp file for create-tool output capture\n"})`

--- RAW OUTPUT START ---
[result]
[name]create[/name]
[output]Created file C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\Subagent\_tmp_create_test.txt with 42 characters[/output]
[/result]
--- RAW OUTPUT END ---

(Temp file `_tmp_create_test.txt` was deleted after this test — see cleanup note at end of document.)

---

## create — error: path already exists

**Call:** `create({path: "...\\_tmp_edit_test.txt", file_text: "this should fail because file already exists\n"})`

--- RAW OUTPUT START ---
[result]
[name]create[/name]
[output]Path C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\GHCP-CLI\Subagent\_tmp_edit_test.txt already exists as a file. Use `view` to check contents then edit it.[/output]
[/result]
--- RAW OUTPUT END ---

---

## powershell (bash equivalent) — success

**Call:** `powershell({command: "Write-Output \"hello from success test\"", description: "Simple success command for output capture"})`

--- RAW OUTPUT START ---
[result]
[name]powershell[/name]
[output]hello from success test
[shellId: 37 completed with exit code 0][/output]
[/result]
--- RAW OUTPUT END ---

Note: successful synchronous output is followed by a bracketed status line `[shellId: {id} completed with exit code {n}]` appended after the command's own stdout.

---

## powershell — error (nonzero exit)

**Call:** `powershell({command: "Write-Error \"deliberate test error\"; exit 7", description: "Simple failing command for output capture"})`

--- RAW OUTPUT START ---
[result]
[name]powershell[/name]
[output]try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch {}; try { if (Get-Variable -Name PSStyle -ErrorAction Ignore) { $PSStyle.OutputRendering = 'PlainText' } } catch {}; $global:LASTEXITCODE = 0; $__copilotCommandFinished = $false; $__copilotSuccess = $false; try { $global:LASTEXITCODE = 0
Write-Error "deliberate test error"; exit 7
$__copilotSuccess = $?
$__copilotCommandFinished = $true
 } finally { if ($__copilotCommandFinished) { if (-not $__copilotSuccess -and $LASTEXITCODE -is [int] -and $LASTEXITCODE -ne 0) { $host.SetShouldExit($LASTEXITCODE) } elseif (-not $__copilotSuccess) { $host.SetShouldExit(1) } else { $host.SetShouldExit(0) } } } : deliberate test error
    + CategoryInfo          : NotSpecified: (:) [Write-Error], WriteErrorException
    + FullyQualifiedErrorId : Microsoft.PowerShell.Commands.WriteErrorException
[shellId: 38 completed with exit code 7][/output]
[/result]
--- RAW OUTPUT END ---

Note: this reveals the harness's internal PowerShell wrapper script that every `command` is actually embedded into — it sets console encoding, disables PSStyle ANSI rendering, tracks `$LASTEXITCODE`/`$?` in wrapper variables (`$__copilotCommandFinished`, `$__copilotSuccess`), and calls `$host.SetShouldExit(...)` in a `finally` block to propagate the real exit code back to the harness. The user's literal command text (`Write-Error "deliberate test error"; exit 7`) is spliced verbatim into the middle of this wrapper. The PowerShell error record (CategoryInfo / FullyQualifiedErrorId) is emitted to the captured output stream, and the final bracketed status line still reports the correct exit code (7) despite the wrapper's own internal `exit 7` call inside the try block.

---

## powershell — long output (exceeds inline size, saved to temp file)

**Call:** `powershell({command: "1..5000 | ForEach-Object { $_ }", description: "Produce 5000 lines of output for truncation capture", initial_wait: 30})`

--- RAW OUTPUT START ---
[result]
[name]powershell[/name]
[output]Output too large to read at once (23.3 KB). Saved to: C:\Users\tgurt\AppData\Local\Temp\1788728251535-copilot-tool-output-50748-2c91da41-88c8-4528-9f8d-26a4d0229217.txt
Consider using tools like grep (for searching), head/tail (for viewing start/end), view with view_range (for specific sections), or jq (for JSON) to examine portions of the output.

Preview (first 500 chars):
1
2
3
4
5
6
7
8
9
10
11
12
13
14
15
16
17
18
19
20
21
22
23
24
25
26
27
28
29
30
31
32
33
34
35
36
37
38
39
40
41
42
43
44
45
46
47
48
49
50
51
52
53
54
55
56
57
58
59
60
61
62
63
64
65
66
67
68
69
70
71
72
73
74
75
76
77
78
79
80
81
82
83
84
85
86
87
88
89
90
91
92
93
94
95
96
97
98
99
100
101
102
103
104
105
106
107
108
109
110
111
112
113
114
115
116
117
118
119
120
121
122
123
124
125
126
127
128
129
130
131
132
133
134
135
136
137
138
139
140
141
142
143
144
145
146
147
148
149
150
151
152

[shellId: 39 completed with exit code 0][/output]
[/result]
--- RAW OUTPUT END ---

Note: this mirrors the `view` tool's own >20KB size cap — for the `powershell` tool, output exceeding a size threshold (here 23.3 KB) is NOT truncated inline. Instead, the full output is saved to an OS temp file (path revealed to the agent, under the invoking user's `AppData\Local\Temp`), and only a "Preview (first 500 chars)" of the output plus suggested follow-up tools (grep, head/tail, view with view_range, jq) is returned inline. This is a distinct behavior from the `view` tool, which refuses outright rather than writing a temp file.

---

## sql — success (bonus capture; not in required list, exercised for completeness)

**Call:** `sql({description: "Test insert todo row", query: "INSERT INTO todos (id, title, description) VALUES ('capture-test-todo', 'Testing sql tool output', 'temporary row for output format capture, will not be relied upon'); SELECT * FROM todos WHERE id='capture-test-todo';"})`

--- RAW OUTPUT START ---
[result]
[name]sql[/name]
[output]Executed 2 statement(s):

1. INSERT
   1 row(s) inserted. Last inserted row ID: 1.

2. SELECT
   1 row(s) returned:
   
   | id | title | description | status | created_at | updated_at |
   | --- | --- | --- | --- | --- | --- |
   | capture-test-todo | Testing sql tool output | temporary row for output format capture, will not be relied upon | pending | 2026-09-06 20:57:37 | 2026-09-06 20:57:37 |[/output]
[/result]
--- RAW OUTPUT END ---

Note: multi-statement query support (semicolon-separated) with per-statement result reporting; SELECT results rendered as a markdown table. (Test row deleted after capture — see cleanup note.)

---

## sql — error (bonus capture)

**Call:** `sql({description: "Test invalid SQL error output", query: "SELECT * FROM this_table_does_not_exist;"})`

--- RAW OUTPUT START ---
[result]
[name]sql[/name]
[output]SQL Error: Error: no such table: this_table_does_not_exist[/output]
[/result]
--- RAW OUTPUT END ---

---

## fetch_copilot_cli_documentation — success (bonus capture)

**Call:** `fetch_copilot_cli_documentation({})`

--- RAW OUTPUT START ---
[result]
[name]fetch_copilot_cli_documentation[/name]
[output]# GitHub Copilot CLI Documentation

## README
[Could not load README: Error: ENOENT: no such file or directory, open 'C:\Users\tgurt\AppData\Local\copilot\pkg\win32-x64\1.0.82\README.md']

## Help Command Output
(... full /help command reference text, slash-command list, keybindings, and instructions-file search order ...)

---

**Note**: If the above documentation doesn't answer the user's request, you can fetch additional documentation from the official GitHub Docs for more information.

Use the web_fetch tool to fetch https://docs.github.com/api/article/body?pathname=/en/copilot/how-tos/use-copilot-agents/use-copilot-cli

You can also follow hyperlinks to related docs pages using the same API pattern.[/output]
[/result]
--- RAW OUTPUT END ---

Note: full verbatim help text elided here for brevity/redundancy (it is workspace/install-specific, e.g. embeds a literal local filesystem path and an installed CLI version number `1.0.82` in a failed-README-load error) — the exact full text was seen once in this session but is not re-transcribed in full a second time since it is not meaningfully different from a stable tool-output-format perspective; the important structural finding is that the CLI's own local README failed to load (ENOENT) and the tool gracefully fell back to embedding the interactive `/help` reference text plus a pointer to fetch further docs via `web_fetch`.

---

## question / ask_user tool — Not captured: tool does not exist in this session's tool set

No tool resembling "ask_user" or "question" (i.e., a tool whose purpose is to send a clarifying question directly to the human user and block for a reply) was present anywhere in this subagent's available function list. This harness's built-in tools for a subagent appear to route user-facing questions through the assistant's own natural-language response text rather than a dedicated tool call. Not exercised — no such tool to call.

## task (spawn sub-agent) tool — Not captured: no schema present / restricted for this agent type

The `task` tool was NOT present as a callable `[function]` in this subagent's context (see BuiltInTools.json `_omittedFromThisSession` note — it is referenced only in the free-text `[task]` guidance block, not granted as an actual schema here). Additionally, per this agent's own instructions ("System Prompt Capturer" agent, now removed from the clean capture), the task tool is explicitly restricted to spawn-testing use only during Tool Output Format Capture, and per the CaptureGuide, task-tool output capture is scoped to the **PrimaryAgent** tier only, not Subagent tier. Not exercised in this Subagent-tier capture for both reasons.

---

## Cleanup

Temporary files created during this capture session and deleted afterward:
- `HarnessKnowledge/SystemPromptCapture/GHCP-CLI/Subagent/_tmp_edit_test.txt` (copy of `varied-content-test.txt`, used for edit success/error tests) — deleted via `powershell` `Remove-Item`.
- `HarnessKnowledge/SystemPromptCapture/GHCP-CLI/Subagent/_tmp_create_test.txt` (new file, used for `create` success test) — deleted via `powershell` `Remove-Item`.
- SQLite `todos` row with id `capture-test-todo` — deleted via `sql` `DELETE` statement.
- The shared `TestArtifacts/` files were only read, never modified or deleted.
- The `powershell` long-output test wrote a harness-managed temp file under the OS temp directory (`...copilot-tool-output-....txt`) — this file is managed by the harness itself, not by this agent, and was not manually deleted (not confirmed whether the harness auto-cleans it).
