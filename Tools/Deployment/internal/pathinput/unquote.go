package pathinput

// Unquote removes exactly one surrounding pair of double-quote characters from s when
// both the first and last characters are '"' and len(s) >= 2. Every other input is
// returned unchanged, including: a string with only a leading quote, only a trailing
// quote, a single '"' character, the empty string, a single-quoted string, and a string
// containing interior quotes. Nested pairs are not unwrapped recursively — exactly one
// pair is removed.
//
// Unquote does not trim whitespace. Callers that trim today keep trimming at the call site.
func Unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
