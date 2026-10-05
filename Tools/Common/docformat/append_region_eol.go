package docformat

import "bytes"

// hostLineEnding returns the line terminator of host, the bytes a new region is appended
// to: CRLF when the first terminator in host is CRLF, LF otherwise (including when host
// has no terminator). New tag lines and seam newlines follow it so an appended region does
// not introduce a second line-ending style into the document.
func hostLineEnding(host []byte) string {
	if i := bytes.IndexByte(host, '\n'); i > 0 && host[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// withLineEnding swaps the trailing LF of a canonical tag line for eol.
func withLineEnding(line []byte, eol string) []byte {
	if eol == "\n" {
		return line
	}
	return append(line[:len(line)-1:len(line)-1], eol...)
}
