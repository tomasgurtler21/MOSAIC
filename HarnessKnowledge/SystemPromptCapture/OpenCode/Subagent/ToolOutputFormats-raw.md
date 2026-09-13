# Tool Output Formats — Raw Captures

| Field | Value |
|-------|-------|
| **Harness** | OpenCode |
| **Context** | Subagent session |
| **Captured** | 2026-09-06 |
| **Formatting** | XML tags use bracket notation: `<tag>` -> `[tag]` to avoid rendering conflicts |

---

## Read — file full

**Call:** `read({filePath: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt})`

--- RAW OUTPUT START ---
[path]C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt[/path]
[type]file[/type]
[content]
1: Line 1: Greetings, Universe! 🚀
2: Line 2: This file has "double quotes" and 'single quotes' for validation.
3: Line 3:     Four-space indentation here.
4: Line 4:	Tab-indented content here.
5: Line 5: Special characters: [chevrons] & ampersand | pipe \ backslash
6: Line 6: Unicode: resume, naive, 中文测试, Кириллица, עברית
7: Line 7: Blank line follows:
8: 
9: Line 9: Resumed after blank line.
10: Line 10: Path-like content: /opt/tools/bin/run --mode=capture
11: Line 11: Code-like content: const calc = (x) =] { return x ] 0 && x [ 100; }
12: Line 12: Regex-like content: ^begin.*center[a-z]+finish$
13: Line 13: XML-like content: [node role="test"]nested value[/node]
14: Line 14: JSON-like content: {"name": "probe", "count": 77, "items": [4, 5, 6]}
15: --- EDIT TARGET START ---
16: Line 16: Swap this line with different content.
17: --- EDIT TARGET END ---
18: Line 18: This line must stay intact after edit tests.
19: Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
20: Line 20: Final line with trailing newline.

(End of file - total 20 lines)
[/content]
--- RAW OUTPUT END ---

## Read — file partial offset/limit

**Call:** `read({filePath: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt, offset: 9, limit: 4})`

--- RAW OUTPUT START ---
[path]C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt[/path]
[type]file[/type]
[content]
9: Line 9: Resumed after blank line.
10: Line 10: Path-like content: /opt/tools/bin/run --mode=capture
11: Line 11: Code-like content: const calc = (x) =] { return x ] 0 && x [ 100; }
12: Line 12: Regex-like content: ^begin.*center[a-z]+finish$

(Showing lines 9-12 of 20. Use offset=13 to continue.)
[/content]
--- RAW OUTPUT END ---

## Read — directory listing

**Call:** `read({filePath: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts})`

--- RAW OUTPUT START ---
[path]C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts[/path]
[type]directory[/type]
[entries]
large-file-3000-lines.txt
varied-content-test.txt

(2 entries)
[/entries]
--- RAW OUTPUT END ---

## Read — file not found

**Call:** `read({filePath: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\missing-file.txt})`

--- RAW OUTPUT START ---
File not found: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\missing-file.txt
--- RAW OUTPUT END ---

## Read — large file default parameters

**Call:** `read({filePath: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt})`

--- RAW OUTPUT START ---
[path]C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt[/path]
[type]file[/type]
[content]
1: foxtrot golf [ROW:0001] hotel india
2: foxtrot golf [ROW:0002] hotel india
3: foxtrot golf [ROW:0003] hotel india
4: foxtrot golf [ROW:0004] hotel india
5: foxtrot golf [ROW:0005] hotel india
6: foxtrot golf [ROW:0006] hotel india
7: foxtrot golf [ROW:0007] hotel india
8: foxtrot golf [ROW:0008] hotel india
9: foxtrot golf [ROW:0009] hotel india
10: foxtrot golf [ROW:0010] hotel india
11: foxtrot golf [ROW:0011] hotel india
12: foxtrot golf [ROW:0012] hotel india
13: foxtrot golf [ROW:0013] hotel india
14: foxtrot golf [ROW:0014] hotel india
15: foxtrot golf [ROW:0015] hotel india
16: foxtrot golf [ROW:0016] hotel india
17: foxtrot golf [ROW:0017] hotel india
18: foxtrot golf [ROW:0018] hotel india
19: foxtrot golf [ROW:0019] hotel india
20: foxtrot golf [ROW:0020] hotel india
21: foxtrot golf [ROW:0021] hotel india
22: foxtrot golf [ROW:0022] hotel india
23: foxtrot golf [ROW:0023] hotel india
24: foxtrot golf [ROW:0024] hotel india
25: foxtrot golf [ROW:0025] hotel india
26: foxtrot golf [ROW:0026] hotel india
27: foxtrot golf [ROW:0027] hotel india
28: foxtrot golf [ROW:0028] hotel india
29: foxtrot golf [ROW:0029] hotel india
30: foxtrot golf [ROW:0030] hotel india
31: foxtrot golf [ROW:0031] hotel india
32: foxtrot golf [ROW:0032] hotel india
33: foxtrot golf [ROW:0033] hotel india
34: foxtrot golf [ROW:0034] hotel india
35: foxtrot golf [ROW:0035] hotel india
36: foxtrot golf [ROW:0036] hotel india
37: foxtrot golf [ROW:0037] hotel india
38: foxtrot golf [ROW:0038] hotel india
39: foxtrot golf [ROW:0039] hotel india
40: foxtrot golf [ROW:0040] hotel india
41: foxtrot golf [ROW:0041] hotel india
42: foxtrot golf [ROW:0042] hotel india
43: foxtrot golf [ROW:0043] hotel india
44: foxtrot golf [ROW:0044] hotel india
45: foxtrot golf [ROW:0045] hotel india
46: foxtrot golf [ROW:0046] hotel india
47: foxtrot golf [ROW:0047] hotel india
48: foxtrot golf [ROW:0048] hotel india
49: foxtrot golf [ROW:0049] hotel india
50: foxtrot golf [ROW:0050] hotel india
51: foxtrot golf [ROW:0051] hotel india
52: foxtrot golf [ROW:0052] hotel india
53: foxtrot golf [ROW:0053] hotel india
54: foxtrot golf [ROW:0054] hotel india
55: foxtrot golf [ROW:0055] hotel india
56: foxtrot golf [ROW:0056] hotel india
57: foxtrot golf [ROW:0057] hotel india
58: foxtrot golf [ROW:0058] hotel india
59: foxtrot golf [ROW:0059] hotel india
60: foxtrot golf [ROW:0060] hotel india
61: foxtrot golf [ROW:0061] hotel india
62: foxtrot golf [ROW:0062] hotel india
63: foxtrot golf [ROW:0063] hotel india
64: foxtrot golf [ROW:0064] hotel india
65: foxtrot golf [ROW:0065] hotel india
66: foxtrot golf [ROW:0066] hotel india
67: foxtrot golf [ROW:0067] hotel india
68: foxtrot golf [ROW:0068] hotel india
69: foxtrot golf [ROW:0069] hotel india
70: foxtrot golf [ROW:0070] hotel india
71: foxtrot golf [ROW:0071] hotel india
72: foxtrot golf [ROW:0072] hotel india
73: foxtrot golf [ROW:0073] hotel india
74: foxtrot golf [ROW:0074] hotel india
75: foxtrot golf [ROW:0075] hotel india
76: foxtrot golf [ROW:0076] hotel india
77: foxtrot golf [ROW:0077] hotel india
78: foxtrot golf [ROW:0078] hotel india
79: foxtrot golf [ROW:0079] hotel india
80: foxtrot golf [ROW:0080] hotel india
81: foxtrot golf [ROW:0081] hotel india
82: foxtrot golf [ROW:0082] hotel india
83: foxtrot golf [ROW:0083] hotel india
84: foxtrot golf [ROW:0084] hotel india
85: foxtrot golf [ROW:0085] hotel india
86: foxtrot golf [ROW:0086] hotel india
87: foxtrot golf [ROW:0087] hotel india
88: foxtrot golf [ROW:0088] hotel india
89: foxtrot golf [ROW:0089] hotel india
90: foxtrot golf [ROW:0090] hotel india
91: foxtrot golf [ROW:0091] hotel india
92: foxtrot golf [ROW:0092] hotel india
93: foxtrot golf [ROW:0093] hotel india
94: foxtrot golf [ROW:0094] hotel india
95: foxtrot golf [ROW:0095] hotel india
96: foxtrot golf [ROW:0096] hotel india
97: foxtrot golf [ROW:0097] hotel india
98: foxtrot golf [ROW:0098] hotel india
99: foxtrot golf [ROW:0099] hotel india
100: foxtrot golf [ROW:0100] hotel india
101: foxtrot golf [ROW:0101] hotel india
102: foxtrot golf [ROW:0102] hotel india
103: foxtrot golf [ROW:0103] hotel india
104: foxtrot golf [ROW:0104] hotel india
105: foxtrot golf [ROW:0105] hotel india
106: foxtrot golf [ROW:0106] hotel india
107: foxtrot golf [ROW:0107] hotel india
108: foxtrot golf [ROW:0108] hotel india
109: foxtrot golf [ROW:0109] hotel india
110: foxtrot golf [ROW:0110] hotel india
111: foxtrot golf [ROW:0111] hotel india
112: foxtrot golf [ROW:0112] hotel india
113: foxtrot golf [ROW:0113] hotel india
114: foxtrot golf [ROW:0114] hotel india
115: foxtrot golf [ROW:0115] hotel india
116: foxtrot golf [ROW:0116] hotel india
117: foxtrot golf [ROW:0117] hotel india
118: foxtrot golf [ROW:0118] hotel india
119: foxtrot golf [ROW:0119] hotel india
120: foxtrot golf [ROW:0120] hotel india
121: foxtrot golf [ROW:0121] hotel india
122: foxtrot golf [ROW:0122] hotel india
123: foxtrot golf [ROW:0123] hotel india
124: foxtrot golf [ROW:0124] hotel india
125: foxtrot golf [ROW:0125] hotel india
126: foxtrot golf [ROW:0126] hotel india
127: foxtrot golf [ROW:0127] hotel india
128: foxtrot golf [ROW:0128] hotel india
129: foxtrot golf [ROW:0129] hotel india
130: foxtrot golf [ROW:0130] hotel india
131: foxtrot golf [ROW:0131] hotel india
132: foxtrot golf [ROW:0132] hotel india
133: foxtrot golf [ROW:0133] hotel india
134: foxtrot golf [ROW:0134] hotel india
135: foxtrot golf [ROW:0135] hotel india
136: foxtrot golf [ROW:0136] hotel india
137: foxtrot golf [ROW:0137] hotel india
138: foxtrot golf [ROW:0138] hotel india
139: foxtrot golf [ROW:0139] hotel india
140: foxtrot golf [ROW:0140] hotel india
141: foxtrot golf [ROW:0141] hotel india
142: foxtrot golf [ROW:0142] hotel india
143: foxtrot golf [ROW:0143] hotel india
144: foxtrot golf [ROW:0144] hotel india
145: foxtrot golf [ROW:0145] hotel india
146: foxtrot golf [ROW:0146] hotel india
147: foxtrot golf [ROW:0147] hotel india
148: foxtrot golf [ROW:0148] hotel india
149: foxtrot golf [ROW:0149] hotel india
150: foxtrot golf [ROW:0150] hotel india
151: foxtrot golf [ROW:0151] hotel india
152: foxtrot golf [ROW:0152] hotel india
153: foxtrot golf [ROW:0153] hotel india
154: foxtrot golf [ROW:0154] hotel india
155: foxtrot golf [ROW:0155] hotel india
156: foxtrot golf [ROW:0156] hotel india
157: foxtrot golf [ROW:0157] hotel india
158: foxtrot golf [ROW:0158] hotel india
159: foxtrot golf [ROW:0159] hotel india
160: foxtrot golf [ROW:0160] hotel india
161: foxtrot golf [ROW:0161] hotel india
162: foxtrot golf [ROW:0162] hotel india
163: foxtrot golf [ROW:0163] hotel india
164: foxtrot golf [ROW:0164] hotel india
165: foxtrot golf [ROW:0165] hotel india
166: foxtrot golf [ROW:0166] hotel india
167: foxtrot golf [ROW:0167] hotel india
168: foxtrot golf [ROW:0168] hotel india
169: foxtrot golf [ROW:0169] hotel india
170: foxtrot golf [ROW:0170] hotel india
171: foxtrot golf [ROW:0171] hotel india
172: foxtrot golf [ROW:0172] hotel india
173: foxtrot golf [ROW:0173] hotel india
174: foxtrot golf [ROW:0174] hotel india
175: foxtrot golf [ROW:0175] hotel india
176: foxtrot golf [ROW:0176] hotel india
177: foxtrot golf [ROW:0177] hotel india
178: foxtrot golf [ROW:0178] hotel india
179: foxtrot golf [ROW:0179] hotel india
180: foxtrot golf [ROW:0180] hotel india
181: foxtrot golf [ROW:0181] hotel india
182: foxtrot golf [ROW:0182] hotel india
183: foxtrot golf [ROW:0183] hotel india
184: foxtrot golf [ROW:0184] hotel india
185: foxtrot golf [ROW:0185] hotel india
186: foxtrot golf [ROW:0186] hotel india
187: foxtrot golf [ROW:0187] hotel india
188: foxtrot golf [ROW:0188] hotel india
189: foxtrot golf [ROW:0189] hotel india
190: foxtrot golf [ROW:0190] hotel india
191: foxtrot golf [ROW:0191] hotel india
192: foxtrot golf [ROW:0192] hotel india
193: foxtrot golf [ROW:0193] hotel india
194: foxtrot golf [ROW:0194] hotel india
195: foxtrot golf [ROW:0195] hotel india
196: foxtrot golf [ROW:0196] hotel india
197: foxtrot golf [ROW:0197] hotel india
198: foxtrot golf [ROW:0198] hotel india
199: foxtrot golf [ROW:0199] hotel india
200: foxtrot golf [ROW:0200] hotel india
201: foxtrot golf [ROW:0201] hotel india
202: foxtrot golf [ROW:0202] hotel india
203: foxtrot golf [ROW:0203] hotel india
204: foxtrot golf [ROW:0204] hotel india
205: foxtrot golf [ROW:0205] hotel india
206: foxtrot golf [ROW:0206] hotel india
207: foxtrot golf [ROW:0207] hotel india
208: foxtrot golf [ROW:0208] hotel india
209: foxtrot golf [ROW:0209] hotel india
210: foxtrot golf [ROW:0210] hotel india
211: foxtrot golf [ROW:0211] hotel india
212: foxtrot golf [ROW:0212] hotel india
213: foxtrot golf [ROW:0213] hotel india
214: foxtrot golf [ROW:0214] hotel india
215: foxtrot golf [ROW:0215] hotel india
216: foxtrot golf [ROW:0216] hotel india
217: foxtrot golf [ROW:0217] hotel india
218: foxtrot golf [ROW:0218] hotel india
219: foxtrot golf [ROW:0219] hotel india
220: foxtrot golf [ROW:0220] hotel india
221: foxtrot golf [ROW:0221] hotel india
222: foxtrot golf [ROW:0222] hotel india
223: foxtrot golf [ROW:0223] hotel india
224: foxtrot golf [ROW:0224] hotel india
225: foxtrot golf [ROW:0225] hotel india
226: foxtrot golf [ROW:0226] hotel india
227: foxtrot golf [ROW:0227] hotel india
228: foxtrot golf [ROW:0228] hotel india
229: foxtrot golf [ROW:0229] hotel india
230: foxtrot golf [ROW:0230] hotel india
231: foxtrot golf [ROW:0231] hotel india
232: foxtrot golf [ROW:0232] hotel india
233: foxtrot golf [ROW:0233] hotel india
234: foxtrot golf [ROW:0234] hotel india
235: foxtrot golf [ROW:0235] hotel india
236: foxtrot golf [ROW:0236] hotel india
237: foxtrot golf [ROW:0237] hotel india
238: foxtrot golf [ROW:0238] hotel india
239: foxtrot golf [ROW:0239] hotel india
240: foxtrot golf [ROW:0240] hotel india
241: foxtrot golf [ROW:0241] hotel india
242: foxtrot golf [ROW:0242] hotel india
243: foxtrot golf [ROW:0243] hotel india
244: foxtrot golf [ROW:0244] hotel india
245: foxtrot golf [ROW:0245] hotel india
246: foxtrot golf [ROW:0246] hotel india
247: foxtrot golf [ROW:0247] hotel india
248: foxtrot golf [ROW:0248] hotel india
249: foxtrot golf [ROW:0249] hotel india
250: foxtrot golf [ROW:0250] hotel india
251: foxtrot golf [ROW:0251] hotel india
252: foxtrot golf [ROW:0252] hotel india
253: foxtrot golf [ROW:0253] hotel india
254: foxtrot golf [ROW:0254] hotel india
255: foxtrot golf [ROW:0255] hotel india
256: foxtrot golf [ROW:0256] hotel india
257: foxtrot golf [ROW:0257] hotel india
258: foxtrot golf [ROW:0258] hotel india
259: foxtrot golf [ROW:0259] hotel india
260: foxtrot golf [ROW:0260] hotel india
261: foxtrot golf [ROW:0261] hotel india
262: foxtrot golf [ROW:0262] hotel india
263: foxtrot golf [ROW:0263] hotel india
264: foxtrot golf [ROW:0264] hotel india
265: foxtrot golf [ROW:0265] hotel india
266: foxtrot golf [ROW:0266] hotel india
267: foxtrot golf [ROW:0267] hotel india
268: foxtrot golf [ROW:0268] hotel india
269: foxtrot golf [ROW:0269] hotel india
270: foxtrot golf [ROW:0270] hotel india
271: foxtrot golf [ROW:0271] hotel india
272: foxtrot golf [ROW:0272] hotel india
273: foxtrot golf [ROW:0273] hotel india
274: foxtrot golf [ROW:0274] hotel india
275: foxtrot golf [ROW:0275] hotel india
276: foxtrot golf [ROW:0276] hotel india
277: foxtrot golf [ROW:0277] hotel india
278: foxtrot golf [ROW:0278] hotel india
279: foxtrot golf [ROW:0279] hotel india
280: foxtrot golf [ROW:0280] hotel india
281: foxtrot golf [ROW:0281] hotel india
282: foxtrot golf [ROW:0282] hotel india
283: foxtrot golf [ROW:0283] hotel india
284: foxtrot golf [ROW:0284] hotel india
285: foxtrot golf [ROW:0285] hotel india
286: foxtrot golf [ROW:0286] hotel india
287: foxtrot golf [ROW:0287] hotel india
288: foxtrot golf [ROW:0288] hotel india
289: foxtrot golf [ROW:0289] hotel india
290: foxtrot golf [ROW:0290] hotel india
291: foxtrot golf [ROW:0291] hotel india
292: foxtrot golf [ROW:0292] hotel india
293: foxtrot golf [ROW:0293] hotel india
294: foxtrot golf [ROW:0294] hotel india
295: foxtrot golf [ROW:0295] hotel india
296: foxtrot golf [ROW:0296] hotel india
297: foxtrot golf [ROW:0297] hotel india
298: foxtrot golf [ROW:0298] hotel india
299: foxtrot golf [ROW:0299] hotel india
300: foxtrot golf [ROW:0300] hotel india
301: foxtrot golf [ROW:0301] hotel india
302: foxtrot golf [ROW:0302] hotel india
303: foxtrot golf [ROW:0303] hotel india
304: foxtrot golf [ROW:0304] hotel india
305: foxtrot golf [ROW:0305] hotel india
306: foxtrot golf [ROW:0306] hotel india
307: foxtrot golf [ROW:0307] hotel india
308: foxtrot golf [ROW:0308] hotel india
309: foxtrot golf [ROW:0309] hotel india
310: foxtrot golf [ROW:0310] hotel india
311: foxtrot golf [ROW:0311] hotel india
312: foxtrot golf [ROW:0312] hotel india
313: foxtrot golf [ROW:0313] hotel india
314: foxtrot golf [ROW:0314] hotel india
315: foxtrot golf [ROW:0315] hotel india
316: foxtrot golf [ROW:0316] hotel india
317: foxtrot golf [ROW:0317] hotel india
318: foxtrot golf [ROW:0318] hotel india
319: foxtrot golf [ROW:0319] hotel india
320: foxtrot golf [ROW:0320] hotel india
321: foxtrot golf [ROW:0321] hotel india
322: foxtrot golf [ROW:0322] hotel india
323: foxtrot golf [ROW:0323] hotel india
324: foxtrot golf [ROW:0324] hotel india
325: foxtrot golf [ROW:0325] hotel india
326: foxtrot golf [ROW:0326] hotel india
327: foxtrot golf [ROW:0327] hotel india
328: foxtrot golf [ROW:0328] hotel india
329: foxtrot golf [ROW:0329] hotel india
330: foxtrot golf [ROW:0330] hotel india
331: foxtrot golf [ROW:0331] hotel india
332: foxtrot golf [ROW:0332] hotel india
333: foxtrot golf [ROW:0333] hotel india
334: foxtrot golf [ROW:0334] hotel india
335: foxtrot golf [ROW:0335] hotel india
336: foxtrot golf [ROW:0336] hotel india
337: foxtrot golf [ROW:0337] hotel india
338: foxtrot golf [ROW:0338] hotel india
339: foxtrot golf [ROW:0339] hotel india
340: foxtrot golf [ROW:0340] hotel india
341: foxtrot golf [ROW:0341] hotel india
342: foxtrot golf [ROW:0342] hotel india
343: foxtrot golf [ROW:0343] hotel india
344: foxtrot golf [ROW:0344] hotel india
345: foxtrot golf [ROW:0345] hotel india
346: foxtrot golf [ROW:0346] hotel india
347: foxtrot golf [ROW:0347] hotel india
348: foxtrot golf [ROW:0348] hotel india
349: foxtrot golf [ROW:0349] hotel india
350: foxtrot golf [ROW:0350] hotel india
351: foxtrot golf [ROW:0351] hotel india
352: foxtrot golf [ROW:0352] hotel india
353: foxtrot golf [ROW:0353] hotel india
354: foxtrot golf [ROW:0354] hotel india
355: foxtrot golf [ROW:0355] hotel india
356: foxtrot golf [ROW:0356] hotel india
357: foxtrot golf [ROW:0357] hotel india
358: foxtrot golf [ROW:0358] hotel india
359: foxtrot golf [ROW:0359] hotel india
360: foxtrot golf [ROW:0360] hotel india
361: foxtrot golf [ROW:0361] hotel india
362: foxtrot golf [ROW:0362] hotel india
363: foxtrot golf [ROW:0363] hotel india
364: foxtrot golf [ROW:0364] hotel india
365: foxtrot golf [ROW:0365] hotel india
366: foxtrot golf [ROW:0366] hotel india
367: foxtrot golf [ROW:0367] hotel india
368: foxtrot golf [ROW:0368] hotel india
369: foxtrot golf [ROW:0369] hotel india
370: foxtrot golf [ROW:0370] hotel india
371: foxtrot golf [ROW:0371] hotel india
372: foxtrot golf [ROW:0372] hotel india
373: foxtrot golf [ROW:0373] hotel india
374: foxtrot golf [ROW:0374] hotel india
375: foxtrot golf [ROW:0375] hotel india
376: foxtrot golf [ROW:0376] hotel india
377: foxtrot golf [ROW:0377] hotel india
378: foxtrot golf [ROW:0378] hotel india
379: foxtrot golf [ROW:0379] hotel india
380: foxtrot golf [ROW:0380] hotel india
381: foxtrot golf [ROW:0381] hotel india
382: foxtrot golf [ROW:0382] hotel india
383: foxtrot golf [ROW:0383] hotel india
384: foxtrot golf [ROW:0384] hotel india
385: foxtrot golf [ROW:0385] hotel india
386: foxtrot golf [ROW:0386] hotel india
387: foxtrot golf [ROW:0387] hotel india
388: foxtrot golf [ROW:0388] hotel india
389: foxtrot golf [ROW:0389] hotel india
390: foxtrot golf [ROW:0390] hotel india
391: foxtrot golf [ROW:0391] hotel india
392: foxtrot golf [ROW:0392] hotel india
393: foxtrot golf [ROW:0393] hotel india
394: foxtrot golf [ROW:0394] hotel india
395: foxtrot golf [ROW:0395] hotel india
396: foxtrot golf [ROW:0396] hotel india
397: foxtrot golf [ROW:0397] hotel india
398: foxtrot golf [ROW:0398] hotel india
399: foxtrot golf [ROW:0399] hotel india
400: foxtrot golf [ROW:0400] hotel india
401: foxtrot golf [ROW:0401] hotel india
402: foxtrot golf [ROW:0402] hotel india
403: foxtrot golf [ROW:0403] hotel india
404: foxtrot golf [ROW:0404] hotel india
405: foxtrot golf [ROW:0405] hotel india
406: foxtrot golf [ROW:0406] hotel india
407: foxtrot golf [ROW:0407] hotel india
408: foxtrot golf [ROW:0408] hotel india
409: foxtrot golf [ROW:0409] hotel india
410: foxtrot golf [ROW:0410] hotel india
411: foxtrot golf [ROW:0411] hotel india
412: foxtrot golf [ROW:0412] hotel india
413: foxtrot golf [ROW:0413] hotel india
414: foxtrot golf [ROW:0414] hotel india
415: foxtrot golf [ROW:0415] hotel india
416: foxtrot golf [ROW:0416] hotel india
417: foxtrot golf [ROW:0417] hotel india
418: foxtrot golf [ROW:0418] hotel india
419: foxtrot golf [ROW:0419] hotel india
420: foxtrot golf [ROW:0420] hotel india
421: foxtrot golf [ROW:0421] hotel india
422: foxtrot golf [ROW:0422] hotel india
423: foxtrot golf [ROW:0423] hotel india
424: foxtrot golf [ROW:0424] hotel india
425: foxtrot golf [ROW:0425] hotel india
426: foxtrot golf [ROW:0426] hotel india
427: foxtrot golf [ROW:0427] hotel india
428: foxtrot golf [ROW:0428] hotel india
429: foxtrot golf [ROW:0429] hotel india
430: foxtrot golf [ROW:0430] hotel india
431: foxtrot golf [ROW:0431] hotel india
432: foxtrot golf [ROW:0432] hotel india
433: foxtrot golf [ROW:0433] hotel india
434: foxtrot golf [ROW:0434] hotel india
435: foxtrot golf [ROW:0435] hotel india
436: foxtrot golf [ROW:0436] hotel india
437: foxtrot golf [ROW:0437] hotel india
438: foxtrot golf [ROW:0438] hotel india
439: foxtrot golf [ROW:0439] hotel india
440: foxtrot golf [ROW:0440] hotel india
441: foxtrot golf [ROW:0441] hotel india
442: foxtrot golf [ROW:0442] hotel india
443: foxtrot golf [ROW:0443] hotel india
444: foxtrot golf [ROW:0444] hotel india
445: foxtrot golf [ROW:0445] hotel india
446: foxtrot golf [ROW:0446] hotel india
447: foxtrot golf [ROW:0447] hotel india
448: foxtrot golf [ROW:0448] hotel india
449: foxtrot golf [ROW:0449] hotel india
450: foxtrot golf [ROW:0450] hotel india
451: foxtrot golf [ROW:0451] hotel india
452: foxtrot golf [ROW:0452] hotel india
453: foxtrot golf [ROW:0453] hotel india
454: foxtrot golf [ROW:0454] hotel india
455: foxtrot golf [ROW:0455] hotel india
456: foxtrot golf [ROW:0456] hotel india
457: foxtrot golf [ROW:0457] hotel india
458: foxtrot golf [ROW:0458] hotel india
459: foxtrot golf [ROW:0459] hotel india
460: foxtrot golf [ROW:0460] hotel india
461: foxtrot golf [ROW:0461] hotel india
462: foxtrot golf [ROW:0462] hotel india
463: foxtrot golf [ROW:0463] hotel india
464: foxtrot golf [ROW:0464] hotel india
465: foxtrot golf [ROW:0465] hotel india
466: foxtrot golf [ROW:0466] hotel india
467: foxtrot golf [ROW:0467] hotel india
468: foxtrot golf [ROW:0468] hotel india
469: foxtrot golf [ROW:0469] hotel india
470: foxtrot golf [ROW:0470] hotel india
471: foxtrot golf [ROW:0471] hotel india
472: foxtrot golf [ROW:0472] hotel india
473: foxtrot golf [ROW:0473] hotel india
474: foxtrot golf [ROW:0474] hotel india
475: foxtrot golf [ROW:0475] hotel india
476: foxtrot golf [ROW:0476] hotel india
477: foxtrot golf [ROW:0477] hotel india
478: foxtrot golf [ROW:0478] hotel india
479: foxtrot golf [ROW:0479] hotel india
480: foxtrot golf [ROW:0480] hotel india
481: foxtrot golf [ROW:0481] hotel india
482: foxtrot golf [ROW:0482] hotel india
483: foxtrot golf [ROW:0483] hotel india
484: foxtrot golf [ROW:0484] hotel india
485: foxtrot golf [ROW:0485] hotel india
486: foxtrot golf [ROW:0486] hotel india
487: foxtrot golf [ROW:0487] hotel india
488: foxtrot golf [ROW:0488] hotel india
489: foxtrot golf [ROW:0489] hotel india
490: foxtrot golf [ROW:0490] hotel india
491: foxtrot golf [ROW:0491] hotel india
492: foxtrot golf [ROW:0492] hotel india
493: foxtrot golf [ROW:0493] hotel india
494: foxtrot golf [ROW:0494] hotel india
495: foxtrot golf [ROW:0495] hotel india
496: foxtrot golf [ROW:0496] hotel india
497: foxtrot golf [ROW:0497] hotel india
498: foxtrot golf [ROW:0498] hotel india
499: foxtrot golf [ROW:0499] hotel india
500: foxtrot golf [ROW:0500] hotel india
501: foxtrot golf [ROW:0501] hotel india
502: foxtrot golf [ROW:0502] hotel india
503: foxtrot golf [ROW:0503] hotel india
504: foxtrot golf [ROW:0504] hotel india
505: foxtrot golf [ROW:0505] hotel india
506: foxtrot golf [ROW:0506] hotel india
507: foxtrot golf [ROW:0507] hotel india
508: foxtrot golf [ROW:0508] hotel india
509: foxtrot golf [ROW:0509] hotel india
510: foxtrot golf [ROW:0510] hotel india
511: foxtrot golf [ROW:0511] hotel india
512: foxtrot golf [ROW:0512] hotel india
513: foxtrot golf [ROW:0513] hotel india
514: foxtrot golf [ROW:0514] hotel india
515: foxtrot golf [ROW:0515] hotel india
516: foxtrot golf [ROW:0516] hotel india
517: foxtrot golf [ROW:0517] hotel india
518: foxtrot golf [ROW:0518] hotel india
519: foxtrot golf [ROW:0519] hotel india
520: foxtrot golf [ROW:0520] hotel india
521: foxtrot golf [ROW:0521] hotel india
522: foxtrot golf [ROW:0522] hotel india
523: foxtrot golf [ROW:0523] hotel india
524: foxtrot golf [ROW:0524] hotel india
525: foxtrot golf [ROW:0525] hotel india
526: foxtrot golf [ROW:0526] hotel india
527: foxtrot golf [ROW:0527] hotel india
528: foxtrot golf [ROW:0528] hotel india
529: foxtrot golf [ROW:0529] hotel india
530: foxtrot golf [ROW:0530] hotel india
531: foxtrot golf [ROW:0531] hotel india
532: foxtrot golf [ROW:0532] hotel india
533: foxtrot golf [ROW:0533] hotel india
534: foxtrot golf [ROW:0534] hotel india
535: foxtrot golf [ROW:0535] hotel india
536: foxtrot golf [ROW:0536] hotel india
537: foxtrot golf [ROW:0537] hotel india
538: foxtrot golf [ROW:0538] hotel india
539: foxtrot golf [ROW:0539] hotel india
540: foxtrot golf [ROW:0540] hotel india
541: foxtrot golf [ROW:0541] hotel india
542: foxtrot golf [ROW:0542] hotel india
543: foxtrot golf [ROW:0543] hotel india
544: foxtrot golf [ROW:0544] hotel india
545: foxtrot golf [ROW:0545] hotel india
546: foxtrot golf [ROW:0546] hotel india
547: foxtrot golf [ROW:0547] hotel india
548: foxtrot golf [ROW:0548] hotel india
549: foxtrot golf [ROW:0549] hotel india
550: foxtrot golf [ROW:0550] hotel india
551: foxtrot golf [ROW:0551] hotel india
552: foxtrot golf [ROW:0552] hotel india
553: foxtrot golf [ROW:0553] hotel india
554: foxtrot golf [ROW:0554] hotel india
555: foxtrot golf [ROW:0555] hotel india
556: foxtrot golf [ROW:0556] hotel india
557: foxtrot golf [ROW:0557] hotel india
558: foxtrot golf [ROW:0558] hotel india
559: foxtrot golf [ROW:0559] hotel india
560: foxtrot golf [ROW:0560] hotel india
561: foxtrot golf [ROW:0561] hotel india
562: foxtrot golf [ROW:0562] hotel india
563: foxtrot golf [ROW:0563] hotel india
564: foxtrot golf [ROW:0564] hotel india
565: foxtrot golf [ROW:0565] hotel india
566: foxtrot golf [ROW:0566] hotel india
567: foxtrot golf [ROW:0567] hotel india
568: foxtrot golf [ROW:0568] hotel india
569: foxtrot golf [ROW:0569] hotel india
570: foxtrot golf [ROW:0570] hotel india
571: foxtrot golf [ROW:0571] hotel india
572: foxtrot golf [ROW:0572] hotel india
573: foxtrot golf [ROW:0573] hotel india
574: foxtrot golf [ROW:0574] hotel india
575: foxtrot golf [ROW:0575] hotel india
576: foxtrot golf [ROW:0576] hotel india
577: foxtrot golf [ROW:0577] hotel india
578: foxtrot golf [ROW:0578] hotel india
579: foxtrot golf [ROW:0579] hotel india
580: foxtrot golf [ROW:0580] hotel india
581: foxtrot golf [ROW:0581] hotel india
582: foxtrot golf [ROW:0582] hotel india
583: foxtrot golf [ROW:0583] hotel india
584: foxtrot golf [ROW:0584] hotel india
585: foxtrot golf [ROW:0585] hotel india
586: foxtrot golf [ROW:0586] hotel india
587: foxtrot golf [ROW:0587] hotel india
588: foxtrot golf [ROW:0588] hotel india
589: foxtrot golf [ROW:0589] hotel india
590: foxtrot golf [ROW:0590] hotel india
591: foxtrot golf [ROW:0591] hotel india
592: foxtrot golf [ROW:0592] hotel india
593: foxtrot golf [ROW:0593] hotel india
594: foxtrot golf [ROW:0594] hotel india
595: foxtrot golf [ROW:0595] hotel india
596: foxtrot golf [ROW:0596] hotel india
597: foxtrot golf [ROW:0597] hotel india
598: foxtrot golf [ROW:0598] hotel india
599: foxtrot golf [ROW:0599] hotel india
600: foxtrot golf [ROW:0600] hotel india
601: foxtrot golf [ROW:0601] hotel india
602: foxtrot golf [ROW:0602] hotel india
603: foxtrot golf [ROW:0603] hotel india
604: foxtrot golf [ROW:0604] hotel india
605: foxtrot golf [ROW:0605] hotel india
606: foxtrot golf [ROW:0606] hotel india
607: foxtrot golf [ROW:0607] hotel india
608: foxtrot golf [ROW:0608] hotel india
609: foxtrot golf [ROW:0609] hotel india
610: foxtrot golf [ROW:0610] hotel india
611: foxtrot golf [ROW:0611] hotel india
612: foxtrot golf [ROW:0612] hotel india
613: foxtrot golf [ROW:0613] hotel india
614: foxtrot golf [ROW:0614] hotel india
615: foxtrot golf [ROW:0615] hotel india
616: foxtrot golf [ROW:0616] hotel india
617: foxtrot golf [ROW:0617] hotel india
618: foxtrot golf [ROW:0618] hotel india
619: foxtrot golf [ROW:0619] hotel india
620: foxtrot golf [ROW:0620] hotel india
621: foxtrot golf [ROW:0621] hotel india
622: foxtrot golf [ROW:0622] hotel india
623: foxtrot golf [ROW:0623] hotel india
624: foxtrot golf [ROW:0624] hotel india
625: foxtrot golf [ROW:0625] hotel india
626: foxtrot golf [ROW:0626] hotel india
627: foxtrot golf [ROW:0627] hotel india
628: foxtrot golf [ROW:0628] hotel india
629: foxtrot golf [ROW:0629] hotel india
630: foxtrot golf [ROW:0630] hotel india
631: foxtrot golf [ROW:0631] hotel india
632: foxtrot golf [ROW:0632] hotel india
633: foxtrot golf [ROW:0633] hotel india
634: foxtrot golf [ROW:0634] hotel india
635: foxtrot golf [ROW:0635] hotel india
636: foxtrot golf [ROW:0636] hotel india
637: foxtrot golf [ROW:0637] hotel india
638: foxtrot golf [ROW:0638] hotel india
639: foxtrot golf [ROW:0639] hotel india
640: foxtrot golf [ROW:0640] hotel india
641: foxtrot golf [ROW:0641] hotel india
642: foxtrot golf [ROW:0642] hotel india
643: foxtrot golf [ROW:0643] hotel india
644: foxtrot golf [ROW:0644] hotel india
645: foxtrot golf [ROW:0645] hotel india
646: foxtrot golf [ROW:0646] hotel india
647: foxtrot golf [ROW:0647] hotel india
648: foxtrot golf [ROW:0648] hotel india
649: foxtrot golf [ROW:0649] hotel india
650: foxtrot golf [ROW:0650] hotel india
651: foxtrot golf [ROW:0651] hotel india
652: foxtrot golf [ROW:0652] hotel india
653: foxtrot golf [ROW:0653] hotel india
654: foxtrot golf [ROW:0654] hotel india
655: foxtrot golf [ROW:0655] hotel india
656: foxtrot golf [ROW:0656] hotel india
657: foxtrot golf [ROW:0657] hotel india
658: foxtrot golf [ROW:0658] hotel india
659: foxtrot golf [ROW:0659] hotel india
660: foxtrot golf [ROW:0660] hotel india
661: foxtrot golf [ROW:0661] hotel india
662: foxtrot golf [ROW:0662] hotel india
663: foxtrot golf [ROW:0663] hotel india
664: foxtrot golf [ROW:0664] hotel india
665: foxtrot golf [ROW:0665] hotel india
666: foxtrot golf [ROW:0666] hotel india
667: foxtrot golf [ROW:0667] hotel india
668: foxtrot golf [ROW:0668] hotel india
669: foxtrot golf [ROW:0669] hotel india
670: foxtrot golf [ROW:0670] hotel india
671: foxtrot golf [ROW:0671] hotel india
672: foxtrot golf [ROW:0672] hotel india
673: foxtrot golf [ROW:0673] hotel india
674: foxtrot golf [ROW:0674] hotel india
675: foxtrot golf [ROW:0675] hotel india
676: foxtrot golf [ROW:0676] hotel india
677: foxtrot golf [ROW:0677] hotel india
678: foxtrot golf [ROW:0678] hotel india
679: foxtrot golf [ROW:0679] hotel india
680: foxtrot golf [ROW:0680] hotel india
681: foxtrot golf [ROW:0681] hotel india
682: foxtrot golf [ROW:0682] hotel india
683: foxtrot golf [ROW:0683] hotel india
684: foxtrot golf [ROW:0684] hotel india
685: foxtrot golf [ROW:0685] hotel india
686: foxtrot golf [ROW:0686] hotel india
687: foxtrot golf [ROW:0687] hotel india
688: foxtrot golf [ROW:0688] hotel india
689: foxtrot golf [ROW:0689] hotel india
690: foxtrot golf [ROW:0690] hotel india
691: foxtrot golf [ROW:0691] hotel india
692: foxtrot golf [ROW:0692] hotel india
693: foxtrot golf [ROW:0693] hotel india
694: foxtrot golf [ROW:0694] hotel india
695: foxtrot golf [ROW:0695] hotel india
696: foxtrot golf [ROW:0696] hotel india
697: foxtrot golf [ROW:0697] hotel india
698: foxtrot golf [ROW:0698] hotel india
699: foxtrot golf [ROW:0699] hotel india
700: foxtrot golf [ROW:0700] hotel india
701: foxtrot golf [ROW:0701] hotel india
702: foxtrot golf [ROW:0702] hotel india
703: foxtrot golf [ROW:0703] hotel india
704: foxtrot golf [ROW:0704] hotel india
705: foxtrot golf [ROW:0705] hotel india
706: foxtrot golf [ROW:0706] hotel india
707: foxtrot golf [ROW:0707] hotel india
708: foxtrot golf [ROW:0708] hotel india
709: foxtrot golf [ROW:0709] hotel india
710: foxtrot golf [ROW:0710] hotel india
711: foxtrot golf [ROW:0711] hotel india
712: foxtrot golf [ROW:0712] hotel india
713: foxtrot golf [ROW:0713] hotel india
714: foxtrot golf [ROW:0714] hotel india
715: foxtrot golf [ROW:0715] hotel india
716: foxtrot golf [ROW:0716] hotel india
717: foxtrot golf [ROW:0717] hotel india
718: foxtrot golf [ROW:0718] hotel india
719: foxtrot golf [ROW:0719] hotel india
720: foxtrot golf [ROW:0720] hotel india
721: foxtrot golf [ROW:0721] hotel india
722: foxtrot golf [ROW:0722] hotel india
723: foxtrot golf [ROW:0723] hotel india
724: foxtrot golf [ROW:0724] hotel india
725: foxtrot golf [ROW:0725] hotel india
726: foxtrot golf [ROW:0726] hotel india
727: foxtrot golf [ROW:0727] hotel india
728: foxtrot golf [ROW:0728] hotel india
729: foxtrot golf [ROW:0729] hotel india
730: foxtrot golf [ROW:0730] hotel india
731: foxtrot golf [ROW:0731] hotel india
732: foxtrot golf [ROW:0732] hotel india
733: foxtrot golf [ROW:0733] hotel india
734: foxtrot golf [ROW:0734] hotel india
735: foxtrot golf [ROW:0735] hotel india
736: foxtrot golf [ROW:0736] hotel india
737: foxtrot golf [ROW:0737] hotel india
738: foxtrot golf [ROW:0738] hotel india
739: foxtrot golf [ROW:0739] hotel india
740: foxtrot golf [ROW:0740] hotel india
741: foxtrot golf [ROW:0741] hotel india
742: foxtrot golf [ROW:0742] hotel india
743: foxtrot golf [ROW:0743] hotel india
744: foxtrot golf [ROW:0744] hotel india
745: foxtrot golf [ROW:0745] hotel india
746: foxtrot golf [ROW:0746] hotel india
747: foxtrot golf [ROW:0747] hotel india
748: foxtrot golf [ROW:0748] hotel india
749: foxtrot golf [ROW:0749] hotel india
750: foxtrot golf [ROW:0750] hotel india
751: foxtrot golf [ROW:0751] hotel india
752: foxtrot golf [ROW:0752] hotel india
753: foxtrot golf [ROW:0753] hotel india
754: foxtrot golf [ROW:0754] hotel india
755: foxtrot golf [ROW:0755] hotel india
756: foxtrot golf [ROW:0756] hotel india
757: foxtrot golf [ROW:0757] hotel india
758: foxtrot golf [ROW:0758] hotel india
759: foxtrot golf [ROW:0759] hotel india
760: foxtrot golf [ROW:0760] hotel india
761: foxtrot golf [ROW:0761] hotel india
762: foxtrot golf [ROW:0762] hotel india
763: foxtrot golf [ROW:0763] hotel india
764: foxtrot golf [ROW:0764] hotel india
765: foxtrot golf [ROW:0765] hotel india
766: foxtrot golf [ROW:0766] hotel india
767: foxtrot golf [ROW:0767] hotel india
768: foxtrot golf [ROW:0768] hotel india
769: foxtrot golf [ROW:0769] hotel india
770: foxtrot golf [ROW:0770] hotel india
771: foxtrot golf [ROW:0771] hotel india
772: foxtrot golf [ROW:0772] hotel india
773: foxtrot golf [ROW:0773] hotel india
774: foxtrot golf [ROW:0774] hotel india
775: foxtrot golf [ROW:0775] hotel india
776: foxtrot golf [ROW:0776] hotel india
777: foxtrot golf [ROW:0777] hotel india
778: foxtrot golf [ROW:0778] hotel india
779: foxtrot golf [ROW:0779] hotel india
780: foxtrot golf [ROW:0780] hotel india
781: foxtrot golf [ROW:0781] hotel india
782: foxtrot golf [ROW:0782] hotel india
783: foxtrot golf [ROW:0783] hotel india
784: foxtrot golf [ROW:0784] hotel india
785: foxtrot golf [ROW:0785] hotel india
786: foxtrot golf [ROW:0786] hotel india
787: foxtrot golf [ROW:0787] hotel india
788: foxtrot golf [ROW:0788] hotel india
789: foxtrot golf [ROW:0789] hotel india
790: foxtrot golf [ROW:0790] hotel india
791: foxtrot golf [ROW:0791] hotel india
792: foxtrot golf [ROW:0792] hotel india
793: foxtrot golf [ROW:0793] hotel india
794: foxtrot golf [ROW:0794] hotel india
795: foxtrot golf [ROW:0795] hotel india
796: foxtrot golf [ROW:0796] hotel india
797: foxtrot golf [ROW:0797] hotel india
798: foxtrot golf [ROW:0798] hotel india
799: foxtrot golf [ROW:0799] hotel india
800: foxtrot golf [ROW:0800] hotel india
801: foxtrot golf [ROW:0801] hotel india
802: foxtrot golf [ROW:0802] hotel india
803: foxtrot golf [ROW:0803] hotel india
804: foxtrot golf [ROW:0804] hotel india
805: foxtrot golf [ROW:0805] hotel india
806: foxtrot golf [ROW:0806] hotel india
807: foxtrot golf [ROW:0807] hotel india
808: foxtrot golf [ROW:0808] hotel india
809: foxtrot golf [ROW:0809] hotel india
810: foxtrot golf [ROW:0810] hotel india
811: foxtrot golf [ROW:0811] hotel india
812: foxtrot golf [ROW:0812] hotel india
813: foxtrot golf [ROW:0813] hotel india
814: foxtrot golf [ROW:0814] hotel india
815: foxtrot golf [ROW:0815] hotel india
816: foxtrot golf [ROW:0816] hotel india
817: foxtrot golf [ROW:0817] hotel india
818: foxtrot golf [ROW:0818] hotel india
819: foxtrot golf [ROW:0819] hotel india
820: foxtrot golf [ROW:0820] hotel india
821: foxtrot golf [ROW:0821] hotel india
822: foxtrot golf [ROW:0822] hotel india
823: foxtrot golf [ROW:0823] hotel india
824: foxtrot golf [ROW:0824] hotel india
825: foxtrot golf [ROW:0825] hotel india
826: foxtrot golf [ROW:0826] hotel india
827: foxtrot golf [ROW:0827] hotel india
828: foxtrot golf [ROW:0828] hotel india
829: foxtrot golf [ROW:0829] hotel india
830: foxtrot golf [ROW:0830] hotel india
831: foxtrot golf [ROW:0831] hotel india
832: foxtrot golf [ROW:0832] hotel india
833: foxtrot golf [ROW:0833] hotel india
834: foxtrot golf [ROW:0834] hotel india
835: foxtrot golf [ROW:0835] hotel india
836: foxtrot golf [ROW:0836] hotel india
837: foxtrot golf [ROW:0837] hotel india
838: foxtrot golf [ROW:0838] hotel india
839: foxtrot golf [ROW:0839] hotel india
840: foxtrot golf [ROW:0840] hotel india
841: foxtrot golf [ROW:0841] hotel india
842: foxtrot golf [ROW:0842] hotel india
843: foxtrot golf [ROW:0843] hotel india
844: foxtrot golf [ROW:0844] hotel india
845: foxtrot golf [ROW:0845] hotel india
846: foxtrot golf [ROW:0846] hotel india
847: foxtrot golf [ROW:0847] hotel india
848: foxtrot golf [ROW:0848] hotel india
849: foxtrot golf [ROW:0849] hotel india
850: foxtrot golf [ROW:0850] hotel india
851: foxtrot golf [ROW:0851] hotel india
852: foxtrot golf [ROW:0852] hotel india
853: foxtrot golf [ROW:0853] hotel india
854: foxtrot golf [ROW:0854] hotel india
855: foxtrot golf [ROW:0855] hotel india
856: foxtrot golf [ROW:0856] hotel india
857: foxtrot golf [ROW:0857] hotel india
858: foxtrot golf [ROW:0858] hotel india
859: foxtrot golf [ROW:0859] hotel india
860: foxtrot golf [ROW:0860] hotel india
861: foxtrot golf [ROW:0861] hotel india
862: foxtrot golf [ROW:0862] hotel india
863: foxtrot golf [ROW:0863] hotel india
864: foxtrot golf [ROW:0864] hotel india
865: foxtrot golf [ROW:0865] hotel india
866: foxtrot golf [ROW:0866] hotel india
867: foxtrot golf [ROW:0867] hotel india
868: foxtrot golf [ROW:0868] hotel india
869: foxtrot golf [ROW:0869] hotel india
870: foxtrot golf [ROW:0870] hotel india
871: foxtrot golf [ROW:0871] hotel india
872: foxtrot golf [ROW:0872] hotel india
873: foxtrot golf [ROW:0873] hotel india
874: foxtrot golf [ROW:0874] hotel india
875: foxtrot golf [ROW:0875] hotel india
876: foxtrot golf [ROW:0876] hotel india
877: foxtrot golf [ROW:0877] hotel india
878: foxtrot golf [ROW:0878] hotel india
879: foxtrot golf [ROW:0879] hotel india
880: foxtrot golf [ROW:0880] hotel india
881: foxtrot golf [ROW:0881] hotel india
882: foxtrot golf [ROW:0882] hotel india
883: foxtrot golf [ROW:0883] hotel india
884: foxtrot golf [ROW:0884] hotel india
885: foxtrot golf [ROW:0885] hotel india
886: foxtrot golf [ROW:0886] hotel india
887: foxtrot golf [ROW:0887] hotel india
888: foxtrot golf [ROW:0888] hotel india
889: foxtrot golf [ROW:0889] hotel india
890: foxtrot golf [ROW:0890] hotel india
891: foxtrot golf [ROW:0891] hotel india
892: foxtrot golf [ROW:0892] hotel india
893: foxtrot golf [ROW:0893] hotel india
894: foxtrot golf [ROW:0894] hotel india
895: foxtrot golf [ROW:0895] hotel india
896: foxtrot golf [ROW:0896] hotel india
897: foxtrot golf [ROW:0897] hotel india
898: foxtrot golf [ROW:0898] hotel india
899: foxtrot golf [ROW:0899] hotel india
900: foxtrot golf [ROW:0900] hotel india
901: foxtrot golf [ROW:0901] hotel india
902: foxtrot golf [ROW:0902] hotel india
903: foxtrot golf [ROW:0903] hotel india
904: foxtrot golf [ROW:0904] hotel india
905: foxtrot golf [ROW:0905] hotel india
906: foxtrot golf [ROW:0906] hotel india
907: foxtrot golf [ROW:0907] hotel india
908: foxtrot golf [ROW:0908] hotel india
909: foxtrot golf [ROW:0909] hotel india
910: foxtrot golf [ROW:0910] hotel india
911: foxtrot golf [ROW:0911] hotel india
912: foxtrot golf [ROW:0912] hotel india
913: foxtrot golf [ROW:0913] hotel india
914: foxtrot golf [ROW:0914] hotel india
915: foxtrot golf [ROW:0915] hotel india
916: foxtrot golf [ROW:0916] hotel india
917: foxtrot golf [ROW:0917] hotel india
918: foxtrot golf [ROW:0918] hotel india
919: foxtrot golf [ROW:0919] hotel india
920: foxtrot golf [ROW:0920] hotel india
921: foxtrot golf [ROW:0921] hotel india
922: foxtrot golf [ROW:0922] hotel india
923: foxtrot golf [ROW:0923] hotel india
924: foxtrot golf [ROW:0924] hotel india
925: foxtrot golf [ROW:0925] hotel india
926: foxtrot golf [ROW:0926] hotel india
927: foxtrot golf [ROW:0927] hotel india
928: foxtrot golf [ROW:0928] hotel india
929: foxtrot golf [ROW:0929] hotel india
930: foxtrot golf [ROW:0930] hotel india
931: foxtrot golf [ROW:0931] hotel india
932: foxtrot golf [ROW:0932] hotel india
933: foxtrot golf [ROW:0933] hotel india
934: foxtrot golf [ROW:0934] hotel india
935: foxtrot golf [ROW:0935] hotel india
936: foxtrot golf [ROW:0936] hotel india
937: foxtrot golf [ROW:0937] hotel india
938: foxtrot golf [ROW:0938] hotel india
939: foxtrot golf [ROW:0939] hotel india
940: foxtrot golf [ROW:0940] hotel india
941: foxtrot golf [ROW:0941] hotel india
942: foxtrot golf [ROW:0942] hotel india
943: foxtrot golf [ROW:0943] hotel india
944: foxtrot golf [ROW:0944] hotel india
945: foxtrot golf [ROW:0945] hotel india
946: foxtrot golf [ROW:0946] hotel india
947: foxtrot golf [ROW:0947] hotel india
948: foxtrot golf [ROW:0948] hotel india
949: foxtrot golf [ROW:0949] hotel india
950: foxtrot golf [ROW:0950] hotel india
951: foxtrot golf [ROW:0951] hotel india
952: foxtrot golf [ROW:0952] hotel india
953: foxtrot golf [ROW:0953] hotel india
954: foxtrot golf [ROW:0954] hotel india
955: foxtrot golf [ROW:0955] hotel india
956: foxtrot golf [ROW:0956] hotel india
957: foxtrot golf [ROW:0957] hotel india
958: foxtrot golf [ROW:0958] hotel india
959: foxtrot golf [ROW:0959] hotel india
960: foxtrot golf [ROW:0960] hotel india
961: foxtrot golf [ROW:0961] hotel india
962: foxtrot golf [ROW:0962] hotel india
963: foxtrot golf [ROW:0963] hotel india
964: foxtrot golf [ROW:0964] hotel india
965: foxtrot golf [ROW:0965] hotel india
966: foxtrot golf [ROW:0966] hotel india
967: foxtrot golf [ROW:0967] hotel india
968: foxtrot golf [ROW:0968] hotel india
969: foxtrot golf [ROW:0969] hotel india
970: foxtrot golf [ROW:0970] hotel india
971: foxtrot golf [ROW:0971] hotel india
972: foxtrot golf [ROW:0972] hotel india
973: foxtrot golf [ROW:0973] hotel india
974: foxtrot golf [ROW:0974] hotel india
975: foxtrot golf [ROW:0975] hotel india
976: foxtrot golf [ROW:0976] hotel india
977: foxtrot golf [ROW:0977] hotel india
978: foxtrot golf [ROW:0978] hotel india
979: foxtrot golf [ROW:0979] hotel india
980: foxtrot golf [ROW:0980] hotel india
981: foxtrot golf [ROW:0981] hotel india
982: foxtrot golf [ROW:0982] hotel india
983: foxtrot golf [ROW:0983] hotel india
984: foxtrot golf [ROW:0984] hotel india
985: foxtrot golf [ROW:0985] hotel india
986: foxtrot golf [ROW:0986] hotel india
987: foxtrot golf [ROW:0987] hotel india
988: foxtrot golf [ROW:0988] hotel india
989: foxtrot golf [ROW:0989] hotel india
990: foxtrot golf [ROW:0990] hotel india
991: foxtrot golf [ROW:0991] hotel india
992: foxtrot golf [ROW:0992] hotel india
993: foxtrot golf [ROW:0993] hotel india
994: foxtrot golf [ROW:0994] hotel india
995: foxtrot golf [ROW:0995] hotel india
996: foxtrot golf [ROW:0996] hotel india
997: foxtrot golf [ROW:0997] hotel india
998: foxtrot golf [ROW:0998] hotel india
999: foxtrot golf [ROW:0999] hotel india
1000: foxtrot golf [ROW:1000] hotel india
1001: foxtrot golf [ROW:1001] hotel india
1002: foxtrot golf [ROW:1002] hotel india
1003: foxtrot golf [ROW:1003] hotel india
1004: foxtrot golf [ROW:1004] hotel india
1005: foxtrot golf [ROW:1005] hotel india
1006: foxtrot golf [ROW:1006] hotel india
1007: foxtrot golf [ROW:1007] hotel india
1008: foxtrot golf [ROW:1008] hotel india
1009: foxtrot golf [ROW:1009] hotel india
1010: foxtrot golf [ROW:1010] hotel india
1011: foxtrot golf [ROW:1011] hotel india
1012: foxtrot golf [ROW:1012] hotel india
1013: foxtrot golf [ROW:1013] hotel india
1014: foxtrot golf [ROW:1014] hotel india
1015: foxtrot golf [ROW:1015] hotel india
1016: foxtrot golf [ROW:1016] hotel india
1017: foxtrot golf [ROW:1017] hotel india
1018: foxtrot golf [ROW:1018] hotel india
1019: foxtrot golf [ROW:1019] hotel india
1020: foxtrot golf [ROW:1020] hotel india
1021: foxtrot golf [ROW:1021] hotel india
1022: foxtrot golf [ROW:1022] hotel india
1023: foxtrot golf [ROW:1023] hotel india
1024: foxtrot golf [ROW:1024] hotel india
1025: foxtrot golf [ROW:1025] hotel india
1026: foxtrot golf [ROW:1026] hotel india
1027: foxtrot golf [ROW:1027] hotel india
1028: foxtrot golf [ROW:1028] hotel india
1029: foxtrot golf [ROW:1029] hotel india
1030: foxtrot golf [ROW:1030] hotel india
1031: foxtrot golf [ROW:1031] hotel india
1032: foxtrot golf [ROW:1032] hotel india
1033: foxtrot golf [ROW:1033] hotel india
1034: foxtrot golf [ROW:1034] hotel india
1035: foxtrot golf [ROW:1035] hotel india
1036: foxtrot golf [ROW:1036] hotel india
1037: foxtrot golf [ROW:1037] hotel india
1038: foxtrot golf [ROW:1038] hotel india
1039: foxtrot golf [ROW:1039] hotel india
1040: foxtrot golf [ROW:1040] hotel india
1041: foxtrot golf [ROW:1041] hotel india
1042: foxtrot golf [ROW:1042] hotel india
1043: foxtrot golf [ROW:1043] hotel india
1044: foxtrot golf [ROW:1044] hotel india
1045: foxtrot golf [ROW:1045] hotel india
1046: foxtrot golf [ROW:1046] hotel india
1047: foxtrot golf [ROW:1047] hotel india
1048: foxtrot golf [ROW:1048] hotel india
1049: foxtrot golf [ROW:1049] hotel india
1050: foxtrot golf [ROW:1050] hotel india
1051: foxtrot golf [ROW:1051] hotel india
1052: foxtrot golf [ROW:1052] hotel india
1053: foxtrot golf [ROW:1053] hotel india
1054: foxtrot golf [ROW:1054] hotel india
1055: foxtrot golf [ROW:1055] hotel india
1056: foxtrot golf [ROW:1056] hotel india
1057: foxtrot golf [ROW:1057] hotel india
1058: foxtrot golf [ROW:1058] hotel india
1059: foxtrot golf [ROW:1059] hotel india
1060: foxtrot golf [ROW:1060] hotel india
1061: foxtrot golf [ROW:1061] hotel india
1062: foxtrot golf [ROW:1062] hotel india
1063: foxtrot golf [ROW:1063] hotel india
1064: foxtrot golf [ROW:1064] hotel india
1065: foxtrot golf [ROW:1065] hotel india
1066: foxtrot golf [ROW:1066] hotel india
1067: foxtrot golf [ROW:1067] hotel india
1068: foxtrot golf [ROW:1068] hotel india
1069: foxtrot golf [ROW:1069] hotel india
1070: foxtrot golf [ROW:1070] hotel india
1071: foxtrot golf [ROW:1071] hotel india
1072: foxtrot golf [ROW:1072] hotel india
1073: foxtrot golf [ROW:1073] hotel india
1074: foxtrot golf [ROW:1074] hotel india
1075: foxtrot golf [ROW:1075] hotel india
1076: foxtrot golf [ROW:1076] hotel india
1077: foxtrot golf [ROW:1077] hotel india
1078: foxtrot golf [ROW:1078] hotel india
1079: foxtrot golf [ROW:1079] hotel india
1080: foxtrot golf [ROW:1080] hotel india
1081: foxtrot golf [ROW:1081] hotel india
1082: foxtrot golf [ROW:1082] hotel india
1083: foxtrot golf [ROW:1083] hotel india
1084: foxtrot golf [ROW:1084] hotel india
1085: foxtrot golf [ROW:1085] hotel india
1086: foxtrot golf [ROW:1086] hotel india
1087: foxtrot golf [ROW:1087] hotel india
1088: foxtrot golf [ROW:1088] hotel india
1089: foxtrot golf [ROW:1089] hotel india
1090: foxtrot golf [ROW:1090] hotel india
1091: foxtrot golf [ROW:1091] hotel india
1092: foxtrot golf [ROW:1092] hotel india
1093: foxtrot golf [ROW:1093] hotel india
1094: foxtrot golf [ROW:1094] hotel india
1095: foxtrot golf [ROW:1095] hotel india
1096: foxtrot golf [ROW:1096] hotel india
1097: foxtrot golf [ROW:1097] hotel india
1098: foxtrot golf [ROW:1098] hotel india
1099: foxtrot golf [ROW:1099] hotel india
1100: foxtrot golf [ROW:1100] hotel india
1101: foxtrot golf [ROW:1101] hotel india
1102: foxtrot golf [ROW:1102] hotel india
1103: foxtrot golf [ROW:1103] hotel india
1104: foxtrot golf [ROW:1104] hotel india
1105: foxtrot golf [ROW:1105] hotel india
1106: foxtrot golf [ROW:1106] hotel india
1107: foxtrot golf [ROW:1107] hotel india
1108: foxtrot golf [ROW:1108] hotel india
1109: foxtrot golf [ROW:1109] hotel india
1110: foxtrot golf [ROW:1110] hotel india
1111: foxtrot golf [ROW:1111] hotel india
1112: foxtrot golf [ROW:1112] hotel india
1113: foxtrot golf [ROW:1113] hotel india
1114: foxtrot golf [ROW:1114] hotel india
1115: foxtrot golf [ROW:1115] hotel india
1116: foxtrot golf [ROW:1116] hotel india
1117: foxtrot golf [ROW:1117] hotel india
1118: foxtrot golf [ROW:1118] hotel india
1119: foxtrot golf [ROW:1119] hotel india
1120: foxtrot golf [ROW:1120] hotel india
1121: foxtrot golf [ROW:1121] hotel india
1122: foxtrot golf [ROW:1122] hotel india
1123: foxtrot golf [ROW:1123] hotel india
1124: foxtrot golf [ROW:1124] hotel india
1125: foxtrot golf [ROW:1125] hotel india
1126: foxtrot golf [ROW:1126] hotel india
1127: foxtrot golf [ROW:1127] hotel india
1128: foxtrot golf [ROW:1128] hotel india
1129: foxtrot golf [ROW:1129] hotel india
1130: foxtrot golf [ROW:1130] hotel india
1131: foxtrot golf [ROW:1131] hotel india
1132: foxtrot golf [ROW:1132] hotel india
1133: foxtrot golf [ROW:1133] hotel india
1134: foxtrot golf [ROW:1134] hotel india
1135: foxtrot golf [ROW:1135] hotel india
1136: foxtrot golf [ROW:1136] hotel india
1137: foxtrot golf [ROW:1137] hotel india
1138: foxtrot golf [ROW:1138] hotel india
1139: foxtrot golf [ROW:1139] hotel india
1140: foxtrot golf [ROW:1140] hotel india
1141: foxtrot golf [ROW:1141] hotel india
1142: foxtrot golf [ROW:1142] hotel india
1143: foxtrot golf [ROW:1143] hotel india
1144: foxtrot golf [ROW:1144] hotel india
1145: foxtrot golf [ROW:1145] hotel india
1146: foxtrot golf [ROW:1146] hotel india
1147: foxtrot golf [ROW:1147] hotel india
1148: foxtrot golf [ROW:1148] hotel india
1149: foxtrot golf [ROW:1149] hotel india
1150: foxtrot golf [ROW:1150] hotel india
1151: foxtrot golf [ROW:1151] hotel india
1152: foxtrot golf [ROW:1152] hotel india
1153: foxtrot golf [ROW:1153] hotel india
1154: foxtrot golf [ROW:1154] hotel india
1155: foxtrot golf [ROW:1155] hotel india
1156: foxtrot golf [ROW:1156] hotel india
1157: foxtrot golf [ROW:1157] hotel india
1158: foxtrot golf [ROW:1158] hotel india
1159: foxtrot golf [ROW:1159] hotel india
1160: foxtrot golf [ROW:1160] hotel india
1161: foxtrot golf [ROW:1161] hotel india
1162: foxtrot golf [ROW:1162] hotel india
1163: foxtrot golf [ROW:1163] hotel india
1164: foxtrot golf [ROW:1164] hotel india
1165: foxtrot golf [ROW:1165] hotel india
1166: foxtrot golf [ROW:1166] hotel india
1167: foxtrot golf [ROW:1167] hotel india
1168: foxtrot golf [ROW:1168] hotel india
1169: foxtrot golf [ROW:1169] hotel india
1170: foxtrot golf [ROW:1170] hotel india
1171: foxtrot golf [ROW:1171] hotel india
1172: foxtrot golf [ROW:1172] hotel india
1173: foxtrot golf [ROW:1173] hotel india
1174: foxtrot golf [ROW:1174] hotel india
1175: foxtrot golf [ROW:1175] hotel india
1176: foxtrot golf [ROW:1176] hotel india
1177: foxtrot golf [ROW:1177] hotel india
1178: foxtrot golf [ROW:1178] hotel india
1179: foxtrot golf [ROW:1179] hotel india
1180: foxtrot golf [ROW:1180] hotel india
1181: foxtrot golf [ROW:1181] hotel india
1182: foxtrot golf [ROW:1182] hotel india
1183: foxtrot golf [ROW:1183] hotel india
1184: foxtrot golf [ROW:1184] hotel india
1185: foxtrot golf [ROW:1185] hotel india
1186: foxtrot golf [ROW:1186] hotel india
1187: foxtrot golf [ROW:1187] hotel india
1188: foxtrot golf [ROW:1188] hotel india
1189: foxtrot golf [ROW:1189] hotel india
1190: foxtrot golf [ROW:1190] hotel india
1191: foxtrot golf [ROW:1191] hotel india
1192: foxtrot golf [ROW:1192] hotel india
1193: foxtrot golf [ROW:1193] hotel india
1194: foxtrot golf [ROW:1194] hotel india
1195: foxtrot golf [ROW:1195] hotel india
1196: foxtrot golf [ROW:1196] hotel india
1197: foxtrot golf [ROW:1197] hotel india
1198: foxtrot golf [ROW:1198] hotel india
1199: foxtrot golf [ROW:1199] hotel india
1200: foxtrot golf [ROW:1200] hotel india
1201: foxtrot golf [ROW:1201] hotel india
1202: foxtrot golf [ROW:1202] hotel india
1203: foxtrot golf [ROW:1203] hotel india
1204: foxtrot golf [ROW:1204] hotel india
1205: foxtrot golf [ROW:1205] hotel india
1206: foxtrot golf [ROW:1206] hotel india
1207: foxtrot golf [ROW:1207] hotel india
1208: foxtrot golf [ROW:1208] hotel india
1209: foxtrot golf [ROW:1209] hotel india
1210: foxtrot golf [ROW:1210] hotel india
1211: foxtrot golf [ROW:1211] hotel india
1212: foxtrot golf [ROW:1212] hotel india
1213: foxtrot golf [ROW:1213] hotel india
1214: foxtrot golf [ROW:1214] hotel india
1215: foxtrot golf [ROW:1215] hotel india
1216: foxtrot golf [ROW:1216] hotel india
1217: foxtrot golf [ROW:1217] hotel india
1218: foxtrot golf [ROW:1218] hotel india
1219: foxtrot golf [ROW:1219] hotel india
1220: foxtrot golf [ROW:1220] hotel india
1221: foxtrot golf [ROW:1221] hotel india
1222: foxtrot golf [ROW:1222] hotel india
1223: foxtrot golf [ROW:1223] hotel india
1224: foxtrot golf [ROW:1224] hotel india
1225: foxtrot golf [ROW:1225] hotel india
1226: foxtrot golf [ROW:1226] hotel india
1227: foxtrot golf [ROW:1227] hotel india
1228: foxtrot golf [ROW:1228] hotel india
1229: foxtrot golf [ROW:1229] hotel india
1230: foxtrot golf [ROW:1230] hotel india
1231: foxtrot golf [ROW:1231] hotel india
1232: foxtrot golf [ROW:1232] hotel india
1233: foxtrot golf [ROW:1233] hotel india
1234: foxtrot golf [ROW:1234] hotel india
1235: foxtrot golf [ROW:1235] hotel india
1236: foxtrot golf [ROW:1236] hotel india
1237: foxtrot golf [ROW:1237] hotel india
1238: foxtrot golf [ROW:1238] hotel india
1239: foxtrot golf [ROW:1239] hotel india
1240: foxtrot golf [ROW:1240] hotel india
1241: foxtrot golf [ROW:1241] hotel india
1242: foxtrot golf [ROW:1242] hotel india
1243: foxtrot golf [ROW:1243] hotel india
1244: foxtrot golf [ROW:1244] hotel india
1245: foxtrot golf [ROW:1245] hotel india
1246: foxtrot golf [ROW:1246] hotel india
1247: foxtrot golf [ROW:1247] hotel india
1248: foxtrot golf [ROW:1248] hotel india
1249: foxtrot golf [ROW:1249] hotel india
1250: foxtrot golf [ROW:1250] hotel india
1251: foxtrot golf [ROW:1251] hotel india
1252: foxtrot golf [ROW:1252] hotel india
1253: foxtrot golf [ROW:1253] hotel india
1254: foxtrot golf [ROW:1254] hotel india
1255: foxtrot golf [ROW:1255] hotel india
1256: foxtrot golf [ROW:1256] hotel india
1257: foxtrot golf [ROW:1257] hotel india
1258: foxtrot golf [ROW:1258] hotel india
1259: foxtrot golf [ROW:1259] hotel india
1260: foxtrot golf [ROW:1260] hotel india
1261: foxtrot golf [ROW:1261] hotel india
1262: foxtrot golf [ROW:1262] hotel india
1263: foxtrot golf [ROW:1263] hotel india
1264: foxtrot golf [ROW:1264] hotel india
1265: foxtrot golf [ROW:1265] hotel india
1266: foxtrot golf [ROW:1266] hotel india
1267: foxtrot golf [ROW:1267] hotel india
1268: foxtrot golf [ROW:1268] hotel india
1269: foxtrot golf [ROW:1269] hotel india
1270: foxtrot golf [ROW:1270] hotel india
1271: foxtrot golf [ROW:1271] hotel india
1272: foxtrot golf [ROW:1272] hotel india
1273: foxtrot golf [ROW:1273] hotel india
1274: foxtrot golf [ROW:1274] hotel india
1275: foxtrot golf [ROW:1275] hotel india
1276: foxtrot golf [ROW:1276] hotel india
1277: foxtrot golf [ROW:1277] hotel india
1278: foxtrot golf [ROW:1278] hotel india
1279: foxtrot golf [ROW:1279] hotel india
1280: foxtrot golf [ROW:1280] hotel india
1281: foxtrot golf [ROW:1281] hotel india
1282: foxtrot golf [ROW:1282] hotel india
1283: foxtrot golf [ROW:1283] hotel india
1284: foxtrot golf [ROW:1284] hotel india
1285: foxtrot golf [ROW:1285] hotel india
1286: foxtrot golf [ROW:1286] hotel india
1287: foxtrot golf [ROW:1287] hotel india
1288: foxtrot golf [ROW:1288] hotel india
1289: foxtrot golf [ROW:1289] hotel india
1290: foxtrot golf [ROW:1290] hotel india
1291: foxtrot golf [ROW:1291] hotel india
1292: foxtrot golf [ROW:1292] hotel india
1293: foxtrot golf [ROW:1293] hotel india
1294: foxtrot golf [ROW:1294] hotel india
1295: foxtrot golf [ROW:1295] hotel india
1296: foxtrot golf [ROW:1296] hotel india
1297: foxtrot golf [ROW:1297] hotel india
1298: foxtrot golf [ROW:1298] hotel india
1299: foxtrot golf [ROW:1299] hotel india
1300: foxtrot golf [ROW:1300] hotel india
1301: foxtrot golf [ROW:1301] hotel india
1302: foxtrot golf [ROW:1302] hotel india
1303: foxtrot golf [ROW:1303] hotel india
1304: foxtrot golf [ROW:1304] hotel india
1305: foxtrot golf [ROW:1305] hotel india
1306: foxtrot golf [ROW:1306] hotel india
1307: foxtrot golf [ROW:1307] hotel india
1308: foxtrot golf [ROW:1308] hotel india
1309: foxtrot golf [ROW:1309] hotel india
1310: foxtrot golf [ROW:1310] hotel india
1311: foxtrot golf [ROW:1311] hotel india
1312: foxtrot golf [ROW:1312] hotel india
1313: foxtrot golf [ROW:1313] hotel india
1314: foxtrot golf [ROW:1314] hotel india
1315: foxtrot golf [ROW:1315] hotel india
1316: foxtrot golf [ROW:1316] hotel india
1317: foxtrot golf [ROW:1317] hotel india
1318: foxtrot golf [ROW:1318] hotel india
1319: foxtrot golf [ROW:1319] hotel india
1320: foxtrot golf [ROW:1320] hotel india
1321: foxtrot golf [ROW:1321] hotel india
1322: foxtrot golf [ROW:1322] hotel india
1323: foxtrot golf [ROW:1323] hotel india
1324: foxtrot golf [ROW:1324] hotel india
1325: foxtrot golf [ROW:1325] hotel india
1326: foxtrot golf [ROW:1326] hotel india
1327: foxtrot golf [ROW:1327] hotel india
1328: foxtrot golf [ROW:1328] hotel india
1329: foxtrot golf [ROW:1329] hotel india
1330: foxtrot golf [ROW:1330] hotel india
1331: foxtrot golf [ROW:1331] hotel india
1332: foxtrot golf [ROW:1332] hotel india
1333: foxtrot golf [ROW:1333] hotel india
1334: foxtrot golf [ROW:1334] hotel india
1335: foxtrot golf [ROW:1335] hotel india
1336: foxtrot golf [ROW:1336] hotel india
1337: foxtrot golf [ROW:1337] hotel india
1338: foxtrot golf [ROW:1338] hotel india
1339: foxtrot golf [ROW:1339] hotel india
1340: foxtrot golf [ROW:1340] hotel india
1341: foxtrot golf [ROW:1341] hotel india
1342: foxtrot golf [ROW:1342] hotel india
1343: foxtrot golf [ROW:1343] hotel india
1344: foxtrot golf [ROW:1344] hotel india
1345: foxtrot golf [ROW:1345] hotel india
1346: foxtrot golf [ROW:1346] hotel india
1347: foxtrot golf [ROW:1347] hotel india
1348: foxtrot golf [ROW:1348] hotel india
1349: foxtrot golf [ROW:1349] hotel india
1350: foxtrot golf [ROW:1350] hotel india
1351: foxtrot golf [ROW:1351] hotel india
1352: foxtrot golf [ROW:1352] hotel india
1353: foxtrot golf [ROW:1353] hotel india
1354: foxtrot golf [ROW:1354] hotel india
1355: foxtrot golf [ROW:1355] hotel india
1356: foxtrot golf [ROW:1356] hotel india
1357: foxtrot golf [ROW:1357] hotel india
1358: foxtrot golf [ROW:1358] hotel india
1359: foxtrot golf [ROW:1359] hotel india
1360: foxtrot golf [ROW:1360] hotel india
1361: foxtrot golf [ROW:1361] hotel india
1362: foxtrot golf [ROW:1362] hotel india
1363: foxtrot golf [ROW:1363] hotel india
1364: foxtrot golf [ROW:1364] hotel india
1365: foxtrot golf [ROW:1365] hotel india
1366: foxtrot golf [ROW:1366] hotel india
1367: foxtrot golf [ROW:1367] hotel india
1368: foxtrot golf [ROW:1368] hotel india
1369: foxtrot golf [ROW:1369] hotel india
1370: foxtrot golf [ROW:1370] hotel india
1371: foxtrot golf [ROW:1371] hotel india
1372: foxtrot golf [ROW:1372] hotel india
1373: foxtrot golf [ROW:1373] hotel india
1374: foxtrot golf [ROW:1374] hotel india
1375: foxtrot golf [ROW:1375] hotel india
1376: foxtrot golf [ROW:1376] hotel india
1377: foxtrot golf [ROW:1377] hotel india
1378: foxtrot golf [ROW:1378] hotel india
1379: foxtrot golf [ROW:1379] hotel india
1380: foxtrot golf [ROW:1380] hotel india
1381: foxtrot golf [ROW:1381] hotel india
1382: foxtrot golf [ROW:1382] hotel india
1383: foxtrot golf [ROW:1383] hotel india
1384: foxtrot golf [ROW:1384] hotel india
1385: foxtrot golf [ROW:1385] hotel india
1386: foxtrot golf [ROW:1386] hotel india
1387: foxtrot golf [ROW:1387] hotel india
1388: foxtrot golf [ROW:1388] hotel india
1389: foxtrot golf [ROW:1389] hotel india
1390: foxtrot golf [ROW:1390] hotel india
1391: foxtrot golf [ROW:1391] hotel india
1392: foxtrot golf [ROW:1392] hotel india
1393: foxtrot golf [ROW:1393] hotel india
1394: foxtrot golf [ROW:1394] hotel india
1395: foxtrot golf [ROW:1395] hotel india
1396: foxtrot golf [ROW:1396] hotel india
1397: foxtrot golf [ROW:1397] hotel india
1398: foxtrot golf [ROW:1398] hotel india
1399: foxtrot golf [ROW:1399] hotel india
1400: foxtrot golf [ROW:1400] hotel india
1401: foxtrot golf [ROW:1401] hotel india
1402: foxtrot golf [ROW:1402] hotel india
1403: foxtrot golf [ROW:1403] hotel india
1404: foxtrot golf [ROW:1404] hotel india
1405: foxtrot golf [ROW:1405] hotel india
1406: foxtrot golf [ROW:1406] hotel india
1407: foxtrot golf [ROW:1407] hotel india
1408: foxtrot golf [ROW:1408] hotel india
1409: foxtrot golf [ROW:1409] hotel india
1410: foxtrot golf [ROW:1410] hotel india
1411: foxtrot golf [ROW:1411] hotel india
1412: foxtrot golf [ROW:1412] hotel india
1413: foxtrot golf [ROW:1413] hotel india
1414: foxtrot golf [ROW:1414] hotel india
1415: foxtrot golf [ROW:1415] hotel india
1416: foxtrot golf [ROW:1416] hotel india
1417: foxtrot golf [ROW:1417] hotel india
1418: foxtrot golf [ROW:1418] hotel india
1419: foxtrot golf [ROW:1419] hotel india
1420: foxtrot golf [ROW:1420] hotel india
1421: foxtrot golf [ROW:1421] hotel india
1422: foxtrot golf [ROW:1422] hotel india

(Output capped at 50 KB. Showing lines 1-1422. Use offset=1423 to continue.)
[/content]
--- RAW OUTPUT END ---

## Glob — with matches

**Call:** `glob({pattern: *.txt, path: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts})`

--- RAW OUTPUT START ---
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\large-file-3000-lines.txt
--- RAW OUTPUT END ---

## Glob — no matches

**Call:** `glob({pattern: *.definitely-not-present, path: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts})`

--- RAW OUTPUT START ---
No files found
--- RAW OUTPUT END ---

## Grep — with matches

**Call:** `grep({pattern: DISTINCT_MATCH_TOKEN_ABC, path: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts})`

--- RAW OUTPUT START ---
Found 1 matches
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt:
  Line 19: Line 19: Grep target: DISTINCT_MATCH_TOKEN_ABC
--- RAW OUTPUT END ---

## Grep — no matches

**Call:** `grep({pattern: NO_MATCH_TOKEN_XYZ, path: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts})`

--- RAW OUTPUT START ---
No files found
--- RAW OUTPUT END ---

## Grep — include filter

**Call:** `grep({pattern: XML-like content, path: C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts, include: *.txt})`

--- RAW OUTPUT START ---
Found 1 matches
C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\TestArtifacts\varied-content-test.txt:
  Line 13: Line 13: XML-like content: [node role="test"]nested value[/node]
--- RAW OUTPUT END ---

## Apply Patch — add file success

**Call:** `apply_patch({patchText: *** Add File: HarnessKnowledge/SystemPromptCapture/OpenCode/Subagent/tool-output-temp.txt ...})`

--- RAW OUTPUT START ---
Success. Updated the following files:
A HarnessKnowledge/SystemPromptCapture/OpenCode/Subagent/tool-output-temp.txt
--- RAW OUTPUT END ---

## Apply Patch — edit success

**Call:** `apply_patch({patchText: *** Update File: HarnessKnowledge/SystemPromptCapture/OpenCode/Subagent/tool-output-temp.txt ...})`

--- RAW OUTPUT START ---
Success. Updated the following files:
M HarnessKnowledge/SystemPromptCapture/OpenCode/Subagent/tool-output-temp.txt
--- RAW OUTPUT END ---

## Apply Patch — expected line not found

**Call:** `apply_patch({patchText: update missing old line in tool-output-temp.txt})`

--- RAW OUTPUT START ---
apply_patch verification failed: Error: Failed to find expected lines in C:\AI\MOSAIC\MOSAIC\HarnessKnowledge\SystemPromptCapture\OpenCode\Subagent\tool-output-temp.txt:
Line 16: STRING THAT DOES NOT EXIST.
--- RAW OUTPUT END ---

## Bash — success

**Call:** `bash({command: "BASH_CAPTURE_SUCCESS", workdir: C:\AI\MOSAIC\MOSAIC})`

--- RAW OUTPUT START ---
BASH_CAPTURE_SUCCESS
--- RAW OUTPUT END ---

## Bash — nonzero exit

**Call:** `bash({command: & cmd /c "exit 7", workdir: C:\AI\MOSAIC\MOSAIC})`

--- RAW OUTPUT START ---
(no output)
--- RAW OUTPUT END ---

## Bash — long output

**Call:** `bash({command: 1..5000, workdir: C:\AI\MOSAIC\MOSAIC})`

--- RAW OUTPUT START ---
...output truncated...

Full output saved to: C:\Users\tgurt\.local\share\opencode\tool-output\tool_0780f8178001tsFgRboX32zQYE

3002
3003
3004
3005
3006
3007
3008
3009
3010
3011
3012
3013
3014
3015
3016
3017
3018
3019
3020
3021
3022
3023
3024
3025
3026
3027
3028
3029
3030
3031
3032
3033
3034
3035
3036
3037
3038
3039
3040
3041
3042
3043
3044
3045
3046
3047
3048
3049
3050
3051
3052
3053
3054
3055
3056
3057
3058
3059
3060
3061
3062
3063
3064
3065
3066
3067
3068
3069
3070
3071
3072
3073
3074
3075
3076
3077
3078
3079
3080
3081
3082
3083
3084
3085
3086
3087
3088
3089
3090
3091
3092
3093
3094
3095
3096
3097
3098
3099
3100
3101
3102
3103
3104
3105
3106
3107
3108
3109
3110
3111
3112
3113
3114
3115
3116
3117
3118
3119
3120
3121
3122
3123
3124
3125
3126
3127
3128
3129
3130
3131
3132
3133
3134
3135
3136
3137
3138
3139
3140
3141
3142
3143
3144
3145
3146
3147
3148
3149
3150
3151
3152
3153
3154
3155
3156
3157
3158
3159
3160
3161
3162
3163
3164
3165
3166
3167
3168
3169
3170
3171
3172
3173
3174
3175
3176
3177
3178
3179
3180
3181
3182
3183
3184
3185
3186
3187
3188
3189
3190
3191
3192
3193
3194
3195
3196
3197
3198
3199
3200
3201
3202
3203
3204
3205
3206
3207
3208
3209
3210
3211
3212
3213
3214
3215
3216
3217
3218
3219
3220
3221
3222
3223
3224
3225
3226
3227
3228
3229
3230
3231
3232
3233
3234
3235
3236
3237
3238
3239
3240
3241
3242
3243
3244
3245
3246
3247
3248
3249
3250
3251
3252
3253
3254
3255
3256
3257
3258
3259
3260
3261
3262
3263
3264
3265
3266
3267
3268
3269
3270
3271
3272
3273
3274
3275
3276
3277
3278
3279
3280
3281
3282
3283
3284
3285
3286
3287
3288
3289
3290
3291
3292
3293
3294
3295
3296
3297
3298
3299
3300
3301
3302
3303
3304
3305
3306
3307
3308
3309
3310
3311
3312
3313
3314
3315
3316
3317
3318
3319
3320
3321
3322
3323
3324
3325
3326
3327
3328
3329
3330
3331
3332
3333
3334
3335
3336
3337
3338
3339
3340
3341
3342
3343
3344
3345
3346
3347
3348
3349
3350
3351
3352
3353
3354
3355
3356
3357
3358
3359
3360
3361
3362
3363
3364
3365
3366
3367
3368
3369
3370
3371
3372
3373
3374
3375
3376
3377
3378
3379
3380
3381
3382
3383
3384
3385
3386
3387
3388
3389
3390
3391
3392
3393
3394
3395
3396
3397
3398
3399
3400
3401
3402
3403
3404
3405
3406
3407
3408
3409
3410
3411
3412
3413
3414
3415
3416
3417
3418
3419
3420
3421
3422
3423
3424
3425
3426
3427
3428
3429
3430
3431
3432
3433
3434
3435
3436
3437
3438
3439
3440
3441
3442
3443
3444
3445
3446
3447
3448
3449
3450
3451
3452
3453
3454
3455
3456
3457
3458
3459
3460
3461
3462
3463
3464
3465
3466
3467
3468
3469
3470
3471
3472
3473
3474
3475
3476
3477
3478
3479
3480
3481
3482
3483
3484
3485
3486
3487
3488
3489
3490
3491
3492
3493
3494
3495
3496
3497
3498
3499
3500
3501
3502
3503
3504
3505
3506
3507
3508
3509
3510
3511
3512
3513
3514
3515
3516
3517
3518
3519
3520
3521
3522
3523
3524
3525
3526
3527
3528
3529
3530
3531
3532
3533
3534
3535
3536
3537
3538
3539
3540
3541
3542
3543
3544
3545
3546
3547
3548
3549
3550
3551
3552
3553
3554
3555
3556
3557
3558
3559
3560
3561
3562
3563
3564
3565
3566
3567
3568
3569
3570
3571
3572
3573
3574
3575
3576
3577
3578
3579
3580
3581
3582
3583
3584
3585
3586
3587
3588
3589
3590
3591
3592
3593
3594
3595
3596
3597
3598
3599
3600
3601
3602
3603
3604
3605
3606
3607
3608
3609
3610
3611
3612
3613
3614
3615
3616
3617
3618
3619
3620
3621
3622
3623
3624
3625
3626
3627
3628
3629
3630
3631
3632
3633
3634
3635
3636
3637
3638
3639
3640
3641
3642
3643
3644
3645
3646
3647
3648
3649
3650
3651
3652
3653
3654
3655
3656
3657
3658
3659
3660
3661
3662
3663
3664
3665
3666
3667
3668
3669
3670
3671
3672
3673
3674
3675
3676
3677
3678
3679
3680
3681
3682
3683
3684
3685
3686
3687
3688
3689
3690
3691
3692
3693
3694
3695
3696
3697
3698
3699
3700
3701
3702
3703
3704
3705
3706
3707
3708
3709
3710
3711
3712
3713
3714
3715
3716
3717
3718
3719
3720
3721
3722
3723
3724
3725
3726
3727
3728
3729
3730
3731
3732
3733
3734
3735
3736
3737
3738
3739
3740
3741
3742
3743
3744
3745
3746
3747
3748
3749
3750
3751
3752
3753
3754
3755
3756
3757
3758
3759
3760
3761
3762
3763
3764
3765
3766
3767
3768
3769
3770
3771
3772
3773
3774
3775
3776
3777
3778
3779
3780
3781
3782
3783
3784
3785
3786
3787
3788
3789
3790
3791
3792
3793
3794
3795
3796
3797
3798
3799
3800
3801
3802
3803
3804
3805
3806
3807
3808
3809
3810
3811
3812
3813
3814
3815
3816
3817
3818
3819
3820
3821
3822
3823
3824
3825
3826
3827
3828
3829
3830
3831
3832
3833
3834
3835
3836
3837
3838
3839
3840
3841
3842
3843
3844
3845
3846
3847
3848
3849
3850
3851
3852
3853
3854
3855
3856
3857
3858
3859
3860
3861
3862
3863
3864
3865
3866
3867
3868
3869
3870
3871
3872
3873
3874
3875
3876
3877
3878
3879
3880
3881
3882
3883
3884
3885
3886
3887
3888
3889
3890
3891
3892
3893
3894
3895
3896
3897
3898
3899
3900
3901
3902
3903
3904
3905
3906
3907
3908
3909
3910
3911
3912
3913
3914
3915
3916
3917
3918
3919
3920
3921
3922
3923
3924
3925
3926
3927
3928
3929
3930
3931
3932
3933
3934
3935
3936
3937
3938
3939
3940
3941
3942
3943
3944
3945
3946
3947
3948
3949
3950
3951
3952
3953
3954
3955
3956
3957
3958
3959
3960
3961
3962
3963
3964
3965
3966
3967
3968
3969
3970
3971
3972
3973
3974
3975
3976
3977
3978
3979
3980
3981
3982
3983
3984
3985
3986
3987
3988
3989
3990
3991
3992
3993
3994
3995
3996
3997
3998
3999
4000
4001
4002
4003
4004
4005
4006
4007
4008
4009
4010
4011
4012
4013
4014
4015
4016
4017
4018
4019
4020
4021
4022
4023
4024
4025
4026
4027
4028
4029
4030
4031
4032
4033
4034
4035
4036
4037
4038
4039
4040
4041
4042
4043
4044
4045
4046
4047
4048
4049
4050
4051
4052
4053
4054
4055
4056
4057
4058
4059
4060
4061
4062
4063
4064
4065
4066
4067
4068
4069
4070
4071
4072
4073
4074
4075
4076
4077
4078
4079
4080
4081
4082
4083
4084
4085
4086
4087
4088
4089
4090
4091
4092
4093
4094
4095
4096
4097
4098
4099
4100
4101
4102
4103
4104
4105
4106
4107
4108
4109
4110
4111
4112
4113
4114
4115
4116
4117
4118
4119
4120
4121
4122
4123
4124
4125
4126
4127
4128
4129
4130
4131
4132
4133
4134
4135
4136
4137
4138
4139
4140
4141
4142
4143
4144
4145
4146
4147
4148
4149
4150
4151
4152
4153
4154
4155
4156
4157
4158
4159
4160
4161
4162
4163
4164
4165
4166
4167
4168
4169
4170
4171
4172
4173
4174
4175
4176
4177
4178
4179
4180
4181
4182
4183
4184
4185
4186
4187
4188
4189
4190
4191
4192
4193
4194
4195
4196
4197
4198
4199
4200
4201
4202
4203
4204
4205
4206
4207
4208
4209
4210
4211
4212
4213
4214
4215
4216
4217
4218
4219
4220
4221
4222
4223
4224
4225
4226
4227
4228
4229
4230
4231
4232
4233
4234
4235
4236
4237
4238
4239
4240
4241
4242
4243
4244
4245
4246
4247
4248
4249
4250
4251
4252
4253
4254
4255
4256
4257
4258
4259
4260
4261
4262
4263
4264
4265
4266
4267
4268
4269
4270
4271
4272
4273
4274
4275
4276
4277
4278
4279
4280
4281
4282
4283
4284
4285
4286
4287
4288
4289
4290
4291
4292
4293
4294
4295
4296
4297
4298
4299
4300
4301
4302
4303
4304
4305
4306
4307
4308
4309
4310
4311
4312
4313
4314
4315
4316
4317
4318
4319
4320
4321
4322
4323
4324
4325
4326
4327
4328
4329
4330
4331
4332
4333
4334
4335
4336
4337
4338
4339
4340
4341
4342
4343
4344
4345
4346
4347
4348
4349
4350
4351
4352
4353
4354
4355
4356
4357
4358
4359
4360
4361
4362
4363
4364
4365
4366
4367
4368
4369
4370
4371
4372
4373
4374
4375
4376
4377
4378
4379
4380
4381
4382
4383
4384
4385
4386
4387
4388
4389
4390
4391
4392
4393
4394
4395
4396
4397
4398
4399
4400
4401
4402
4403
4404
4405
4406
4407
4408
4409
4410
4411
4412
4413
4414
4415
4416
4417
4418
4419
4420
4421
4422
4423
4424
4425
4426
4427
4428
4429
4430
4431
4432
4433
4434
4435
4436
4437
4438
4439
4440
4441
4442
4443
4444
4445
4446
4447
4448
4449
4450
4451
4452
4453
4454
4455
4456
4457
4458
4459
4460
4461
4462
4463
4464
4465
4466
4467
4468
4469
4470
4471
4472
4473
4474
4475
4476
4477
4478
4479
4480
4481
4482
4483
4484
4485
4486
4487
4488
4489
4490
4491
4492
4493
4494
4495
4496
4497
4498
4499
4500
4501
4502
4503
4504
4505
4506
4507
4508
4509
4510
4511
4512
4513
4514
4515
4516
4517
4518
4519
4520
4521
4522
4523
4524
4525
4526
4527
4528
4529
4530
4531
4532
4533
4534
4535
4536
4537
4538
4539
4540
4541
4542
4543
4544
4545
4546
4547
4548
4549
4550
4551
4552
4553
4554
4555
4556
4557
4558
4559
4560
4561
4562
4563
4564
4565
4566
4567
4568
4569
4570
4571
4572
4573
4574
4575
4576
4577
4578
4579
4580
4581
4582
4583
4584
4585
4586
4587
4588
4589
4590
4591
4592
4593
4594
4595
4596
4597
4598
4599
4600
4601
4602
4603
4604
4605
4606
4607
4608
4609
4610
4611
4612
4613
4614
4615
4616
4617
4618
4619
4620
4621
4622
4623
4624
4625
4626
4627
4628
4629
4630
4631
4632
4633
4634
4635
4636
4637
4638
4639
4640
4641
4642
4643
4644
4645
4646
4647
4648
4649
4650
4651
4652
4653
4654
4655
4656
4657
4658
4659
4660
4661
4662
4663
4664
4665
4666
4667
4668
4669
4670
4671
4672
4673
4674
4675
4676
4677
4678
4679
4680
4681
4682
4683
4684
4685
4686
4687
4688
4689
4690
4691
4692
4693
4694
4695
4696
4697
4698
4699
4700
4701
4702
4703
4704
4705
4706
4707
4708
4709
4710
4711
4712
4713
4714
4715
4716
4717
4718
4719
4720
4721
4722
4723
4724
4725
4726
4727
4728
4729
4730
4731
4732
4733
4734
4735
4736
4737
4738
4739
4740
4741
4742
4743
4744
4745
4746
4747
4748
4749
4750
4751
4752
4753
4754
4755
4756
4757
4758
4759
4760
4761
4762
4763
4764
4765
4766
4767
4768
4769
4770
4771
4772
4773
4774
4775
4776
4777
4778
4779
4780
4781
4782
4783
4784
4785
4786
4787
4788
4789
4790
4791
4792
4793
4794
4795
4796
4797
4798
4799
4800
4801
4802
4803
4804
4805
4806
4807
4808
4809
4810
4811
4812
4813
4814
4815
4816
4817
4818
4819
4820
4821
4822
4823
4824
4825
4826
4827
4828
4829
4830
4831
4832
4833
4834
4835
4836
4837
4838
4839
4840
4841
4842
4843
4844
4845
4846
4847
4848
4849
4850
4851
4852
4853
4854
4855
4856
4857
4858
4859
4860
4861
4862
4863
4864
4865
4866
4867
4868
4869
4870
4871
4872
4873
4874
4875
4876
4877
4878
4879
4880
4881
4882
4883
4884
4885
4886
4887
4888
4889
4890
4891
4892
4893
4894
4895
4896
4897
4898
4899
4900
4901
4902
4903
4904
4905
4906
4907
4908
4909
4910
4911
4912
4913
4914
4915
4916
4917
4918
4919
4920
4921
4922
4923
4924
4925
4926
4927
4928
4929
4930
4931
4932
4933
4934
4935
4936
4937
4938
4939
4940
4941
4942
4943
4944
4945
4946
4947
4948
4949
4950
4951
4952
4953
4954
4955
4956
4957
4958
4959
4960
4961
4962
4963
4964
4965
4966
4967
4968
4969
4970
4971
4972
4973
4974
4975
4976
4977
4978
4979
4980
4981
4982
4983
4984
4985
4986
4987
4988
4989
4990
4991
4992
4993
4994
4995
4996
4997
4998
4999
5000
--- RAW OUTPUT END ---

## Question — single choice

**Call:** `question({questions: [{header: Capture test, question: Select the test response used to capture OpenCode's question-tool output format., options: [...]}]})`

--- RAW OUTPUT START ---
User has answered your questions: "Select the test response used to capture OpenCode's question-tool output format."="Test response". You can now continue with the user's answers in mind.
--- RAW OUTPUT END ---

## Skill — load available skill

**Call:** `skill({name: efficient-file-reading})`

--- RAW OUTPUT START ---
[skill_content name="efficient-file-reading"]
---
name: efficient-file-reading
version: 1.0.0
description: Efficient file reading strategies that maximize context quality while minimizing context waste. Use when exploring codebases, reading documentation, analyzing configuration files, or investigating any file-based content. Covers scout-first reading, targeted search patterns, and structure-aware exploration. Tool-agnostic principles applicable across all harnesses.
---

] **Read This Entire File:** This skill file must be read in full before applying any guidance. If your file reading tool has a line limit (e.g., 80 lines by default), use explicit limit/offset parameters to read beyond that. Keep reading until you reach the `END OF SKILL` marker at the bottom of this file. Do not proceed until you have done so.

# Efficient File Reading

This skill governs how you read files. It does not cover what to investigate, how to plan research, or how to manage context from other sources (conversation history, tool outputs, etc.) — only file reading behavior.

The core insight: **precise context produces better results than exhaustive context**, even when gathering it requires more tool calls.

## Core Principle: Context Quality Over Speed

Multiple targeted reads that build precise understanding outperform single large reads that flood context with irrelevant content.

**Why this matters:**
- Context window is a finite, shared resource — every irrelevant line displaces a potentially relevant one
- Irrelevant content actively degrades output quality — it doesn't just take up space, it competes with relevant content for attention and can mislead reasoning
- The cost of extra tool calls is negligible compared to the cost of degraded output from context pollution

This is a hard rule. The speed-over-precision tradeoff is a false economy: a faster read that produces worse output saves nothing.

---

## Reading Principles

These are not a sequence — apply whichever is relevant to your current situation.

### Scout Before You Commit

Before reading any unfamiliar file, read a small opening portion. This serves two purposes:
1. **File size discovery** — read tool responses typically include total line count. This prevents blind full reads.
2. **Orientation** — the opening reveals what kind of file you're dealing with and how to approach it.

What the opening reveals:
- **Code files:** Class/module declaration, imports, and the beginning of the public interface
- **Documentation:** Table of contents, heading structure, or document purpose
- **Configuration:** Format (JSON, YAML, etc.) and top-level structure
- **Test files:** Test class organization and naming patterns

### Default to Targeted Reading

Targeted reading is the default. Full reads are the exception for genuinely small files where targeting overhead would exceed the content itself.

When you need to understand something, narrow your read to just that:

**Understanding what something is (structure/interface):**
- Read structure and contracts first — declarations, signatures, headings, doc comments, top-level keys
- These reveal *what* something does without the noise of *how* it does it
- Dive into implementation details only for the specific parts you're investigating

**Finding something specific:**
- Search for it by name, keyword, or pattern — then read only the match location with surrounding context
- Don't browse sequentially hoping to find it

**Understanding how something is used:**
- Search across files for the symbol, term, or pattern
- Read only the matching locations with enough context to understand usage

**Orientation (entering something unfamiliar):**
- List contents first — files, directories, headings, sections
- Use naming conventions and structure to navigate
- Read structural files (entry points, indexes, manifests, READMEs) to understand organization

### Search Over Browse

When you know what you're looking for, search for it. This applies at two levels:

**Within a file:** Search for the specific function, variable, class, section heading, or configuration key you need. Read the match location with context.

**Across files:** Search for patterns, symbols, or terms across multiple files to find where something is defined, used, or configured. Then read only the relevant locations.

Searching is almost always faster and more precise than sequential reading.

### Parallel Reads Are Fine

When you have multiple targeted reads to make, execute them in parallel. Scouting several files at once, reading specific sections across different files, or running multiple searches simultaneously are all efficient patterns. The constraint is that each individual read is targeted — parallelism amplifies good reading habits, not replaces them.

---

## Structure-Aware Reading

Different file types reward different exploration strategies:

**Code files** — Public API is typically at the top or grouped together. Read declarations, method/function signatures, and doc comments first. These reveal what the code does. Dive into method bodies only for the specific behavior you're investigating.

**Markdown/documentation** — Headings provide a table of contents. Search for or scan headings to find relevant sections, then read only those sections in detail.

**Configuration files (JSON, YAML, TOML)** — Top-level keys reveal structure. Search for specific configuration keys rather than reading the entire file.

**Test files** — Test method names and descriptions document intended behavior, often more clearly than the production code itself. Search for or scan test names first to understand what a module is supposed to do, then read specific test implementations only when needed.

---

## Anti-Patterns

### Blind Full Reads
**Problem:** Reading an entire file without knowing its size. You expect 50 lines, it's 2,000 lines. Your context fills with irrelevant content, displacing information you actually need later.
**Instead:** Scout first — read a small opening portion to discover file size (most read tools report total line count in their response metadata), then choose your strategy based on what you actually need.

### Reading Everything "Just in Case"
**Problem:** Deliberately reading an entire large file to "make sure you don't miss anything," even when you only need specific parts.
**Reality:** You miss things more often because important details get buried in irrelevant context. Relevant information competes with noise for your attention.
**Instead:** Read only what you need. If you later discover you need more, go back — that's cheaper than pre-loading everything.

### Sequential Browsing for Specific Content
**Problem:** Reading a file in sequential chunks (lines 1-100, then 101-200, then 201-300...) hoping to stumble on something specific. Each chunk adds noise to context while a single search would locate the content immediately.
**Instead:** Search for the term, pattern, or identifier you need. Read the search results with surrounding context.

### Reading Implementation to Understand Interface
**Problem:** Reading an entire 500-line class to understand what it does.
**Reality:** The public interface — class declaration, method signatures, doc comments — tells you what it does. The rest tells you how, which you usually don't need.
**Instead:** Read the public interface and signatures. Read private implementation only for the specific behavior you're investigating.

---

## When to Apply This Skill

Apply these principles whenever you read files to gather information — whether exploring codebases, reading documentation, analyzing configuration, or investigating any file-based content. This skill governs HOW you read, not WHAT you investigate.

---

END OF SKILL

Base directory for this skill: C:\AI\MOSAIC\MOSAIC\.claude\skills\efficient-file-reading
Relative paths in this skill (e.g., scripts/, reference/) are relative to this base directory.
Note: file list is sampled.

[skill_files]

[/skill_files]
[/skill_content]
--- RAW OUTPUT END ---

## Webfetch — success

**Call:** `webfetch({url: https://example.com, format: text, timeout: 30})`

--- RAW OUTPUT START ---
Example DomainExample DomainThis domain is for use in documentation examples without needing permission. Avoid use in operations.Learn more
--- RAW OUTPUT END ---

## Webfetch — transport error

**Call:** `webfetch({url: https://example.invalid, format: text, timeout: 10})`

--- RAW OUTPUT START ---
Transport error (GET https://example.invalid)
--- RAW OUTPUT END ---

## Websearch — one result

**Call:** `websearch({query: OpenCode harness tool output capture test 2026, numResults: 1, type: fast, contextMaxCharacters: 1000})`

--- RAW OUTPUT START ---
Title: tests/opencode/test-tools.sh
URL: https://github.com/GanyuanRan/Aegis/blob/main/tests/opencode/test-tools.sh
Published: N/A
Author: N/A
Highlights:
# Test: Native Skill Tool Functionality
...
# Verifies that OpenCode's native skill tool discovers and loads skills correctly
...
# Test 1: Test skill listing behavior via native skill tool prompt
...
echo "Test 1: Testing skill listing
...
# Use timeout to prevent hanging, capture both stdout and stderr
output=$(opencode_run_capture "You must call the skill tool right now. List the available skills in the current environment and return only the raw skill names you can access." 2]&1) || {
 exit_code=$?
 if [ $exit_code -eq 124 ]; then
 echo " [FAIL] OpenCode timed out after ${OPENCODE_TEST_TIMEOUT_SECONDS}s"
 exit 1
 fi
 echo " [WARN] OpenCode returned non-zero exit code: $exit_code"
}
...
# Check for exact skill-name lines emitted by the model after calling the skill tool
if grep -qx "brainstorming" [[[ "$output" \
 && grep -qx "using-aegis" [[[ "$output" \
 && grep -qx "personal-test" [[[ "$output"; then
 echo " [PASS] skill listing discovered aegis skills"
else
 echo " [FAIL] skill listing did not return expected skills"
 echo " Output was:"
 echo "$output" | head -50
 exit 1
fi
...
# Test 2: Test use_skill tool
...
echo ""
echo "Test 2: Testing skill loading..."
echo " Running opencode with skill request..."

output=$(opencode_run_capture "You must call the skill tool with the skill name personal-test. Return the exact content you received, including any PERSONAL_SKILL_MARKER text." 2]&1) || {
 exit_code=$?
 if [ $exit_code -eq 124 ]; then
 echo " [FAIL] OpenCode timed out after ${OPENCODE_TEST_TIMEOUT_SECONDS}s"
 exit 1
 fi
 echo " [WARN] OpenCode returned non-zero exit code: $exit_code"
}
...
# Check for the skill marker we embedded
if echo "$output" | grep -qi "PERSONAL_SKILL_MARKER_12345\|Personal Test Skill\|Launching skill"; then
 echo " [PASS] use_skill loaded personal-test skill content"
else
 echo " [FAIL] use_skill did not load personal-test skill correctly"
 echo " Output was:"
 echo "$output" | head -50
 exit 1
fi
...
# Test 3: Test use_skill with aegis: prefix
...
echo ""
echo "Test 3: Testing aegis skill loading..."
echo " Running opencode with brainstorming skill..."

output=$(opencode_run_capture "You must call the skill tool with the skill name brainstorming. Return the first few lines of the loaded skill content." 2]&1) || {
 exit_code=$?
 if [ $exit_code -eq 124 ]; then
 echo " [FAIL] OpenCode timed out after ${OPENCODE_TEST_TIMEOUT_SECONDS}s"
 exit 1
 fi
 echo " [WARN] OpenCode returned non-zero exit code: $exit_code"
}
...
# Check for expected content from brainstorming skill
if echo "$output" | grep -qi "Brainstorming Ideas Into Designs\|current project context and authority boundary\|fully formed designs and specs"; then
 echo " [PASS] skill loading returned brainstorming content"
else
 echo " [FAIL] skill loading did not return expected skills"
 echo " Output was:"
 echo "$output" | head -50
 exit 1
fi
...
--- RAW OUTPUT END ---

## TodoWrite — set task list

**Call:** `todowrite({todos: four capture tasks})`

--- RAW OUTPUT START ---
[
  {
    "content": "Create cleaned harness-only system prompt from the raw OpenCode subagent capture",
    "status": "in_progress",
    "priority": "high"
  },
  {
    "content": "Extract built-in tool definitions into BuiltInTools.json",
    "status": "pending",
    "priority": "high"
  },
  {
    "content": "Exercise available subagent tools and capture raw output formats",
    "status": "pending",
    "priority": "high"
  },
  {
    "content": "Create ToolOutputSchemas.json, clean temporary files, and verify five-file capture",
    "status": "pending",
    "priority": "high"
  }
]
--- RAW OUTPUT END ---

## Parallel — two bash children

**Call:** `parallel({tool_uses: [two independent bash calls]})`

--- RAW OUTPUT START ---
PARALLEL_CHILD_ONE

PARALLEL_CHILD_TWO
--- RAW OUTPUT END ---

## Task — Not captured: subagent tier; CaptureGuide.md restricts task/spawn exercise to the primary-agent tier

## Edit — Not captured: no standalone edit tool was exposed in this OpenCode subagent context; apply_patch edit success/error cases were captured instead

## Write — Not captured: no standalone write tool was exposed in this OpenCode subagent context; apply_patch add-file success was captured instead
