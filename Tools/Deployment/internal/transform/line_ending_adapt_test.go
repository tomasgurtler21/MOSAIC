package transform

import "testing"

func TestConvertLineEndings(t *testing.T) {
	cases := []struct {
		name, in, nl, want string
	}{
		{"lf to crlf", "a\nb\n", "\r\n", "a\r\nb\r\n"},
		{"crlf to lf", "a\r\nb\r\n", "\n", "a\nb\n"},
		{"crlf idempotent", "a\r\nb\r\n", "\r\n", "a\r\nb\r\n"},
		{"lf idempotent", "a\nb\n", "\n", "a\nb\n"},
		{"lone cr kept", "a\rb\n", "\r\n", "a\rb\r\n"},
		{"invalid nl unchanged", "a\r\nb\n", "\r", "a\r\nb\n"},
		{"empty nl unchanged", "a\nb\n", "", "a\nb\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(ConvertLineEndings([]byte(tc.in), tc.nl)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
