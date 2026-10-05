package transform

import "bytes"

// ConvertLineEndings returns content with every line terminator rewritten to nl ("\n" or
// "\r\n"). It is used on content the transform inserts into the output document (provider
// blocks, content lifted from a deployed file) so the inserted lines match the source
// document's style. Content already in the requested style is returned unchanged, and any
// nl other than "\n" or "\r\n" leaves content untouched.
func ConvertLineEndings(content []byte, nl string) []byte {
	if len(content) == 0 || (nl != "\n" && nl != "\r\n") {
		return content
	}
	lf := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	if nl == "\r\n" {
		return bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	}
	return lf
}

// sourceLineEnding reports the line ending the output document uses: the source's.
func sourceLineEnding(req Request) string {
	return DetectLineEnding(req.Source)
}
