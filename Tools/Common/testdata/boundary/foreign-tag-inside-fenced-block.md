---
name: test-agent
---

This document demonstrates that the parser is code-fence-unaware. A valid MOSAIC
boundary tag inside a fenced code block is still recognised as a boundary, because
the parser processes lines independently with no knowledge of code-fence context.
```
<Identity type="core">
Code content that is inside the section (and inside the fence from a Markdown view).
</Identity>
```
Text after the fenced block and after the section.
