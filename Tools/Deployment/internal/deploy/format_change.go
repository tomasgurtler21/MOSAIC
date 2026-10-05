package deploy

import (
	"bytes"
	"os"

	"mosaic-deploy/internal/domain"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DetectFormatChange compares the bytes of an existing file with the bytes about to replace
// it. It returns nil when neither the UTF-8 BOM presence nor the line-ending style changes.
// Line-ending style is classified per side by the terminator-count rules documented on
// domain.LineEndingStyle; a side classified as none never produces a line-ending change.
// A nil or empty existing slice is classified like any other content (no BOM, style none).
func DetectFormatChange(existing, written []byte) *domain.FormatChange {
	fc := domain.FormatChange{
		BOMBefore:         bytes.HasPrefix(existing, utf8BOM),
		BOMAfter:          bytes.HasPrefix(written, utf8BOM),
		LineEndingsBefore: classifyLineEndings(existing),
		LineEndingsAfter:  classifyLineEndings(written),
	}
	if !fc.BOMChanged() && !fc.LineEndingsChanged() {
		return nil
	}
	return &fc
}

// classifyLineEndings counts LF and CRLF terminators and maps the counts to a style.
func classifyLineEndings(b []byte) domain.LineEndingStyle {
	var lf, crlf int
	for i, c := range b {
		if c != '\n' {
			continue
		}
		if i > 0 && b[i-1] == '\r' {
			crlf++
		} else {
			lf++
		}
	}
	switch {
	case lf > 0 && crlf > 0:
		return domain.LineEndingMixed
	case lf > 0:
		return domain.LineEndingLF
	case crlf > 0:
		return domain.LineEndingCRLF
	}
	return domain.LineEndingNone
}

// formatChangeOnOverwrite reads the file currently at path and reports how replacing it with
// content changes its formatting. It returns nil when the file cannot be read (for example
// it does not exist yet) or when the formatting is unchanged.
func formatChangeOnOverwrite(path string, content []byte) *domain.FormatChange {
	existing, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return DetectFormatChange(existing, content)
}

// withProbeFormat returns the formatting change to record for ar. The workspace writability
// probe overwrites probeItem's target before the item is executed, so executing that item
// finds the already-rewritten file and detects nothing; the change read before the probe
// write is used instead.
func withProbeFormat(ar domain.ActionRecord, item, probeItem domain.PlanItem, probeFormat *domain.FormatChange) *domain.FormatChange {
	if ar.FormatChange != nil || probeFormat == nil || item.TargetPath != probeItem.TargetPath {
		return ar.FormatChange
	}
	if ar.Taken != domain.TakenCreated && ar.Taken != domain.TakenUpdated {
		return nil
	}
	return probeFormat
}
