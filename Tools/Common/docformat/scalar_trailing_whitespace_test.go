package docformat_test

// Tests for trailing ASCII whitespace on plain scalars. A plain scalar does not include
// trailing spaces or tabs in its value; quoted scalars keep their content; raw bytes of
// unmodified entries are unchanged.

import (
	"bytes"
	"testing"

	"mosaic-common/mosaic"
)

func lineEndingName(nl string) string {
	if nl == "\r\n" {
		return "crlf"
	}
	return "lf"
}

func TestFrontmatterGet_PlainScalarTrailingWhitespace_NotPartOfValue(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"trailing spaces", "version: 1.0.0   ", "1.0.0"},
		{"single trailing space", "version: 1.0.0 ", "1.0.0"},
		{"trailing tab", "version: 1.0.0\t", "1.0.0"},
		{"mixed spaces and tabs", "version: 1.0.0 \t \t", "1.0.0"},
		{"inner spaces kept", "version: one two  ", "one two"},
	}
	for _, tt := range tests {
		for _, nl := range []string{"\n", "\r\n"} {
			t.Run(tt.name+" "+lineEndingName(nl), func(t *testing.T) {
				doc := mustParse(t, "---"+nl+tt.line+nl+"---"+nl+"Body"+nl)

				v, ok := doc.Frontmatter().Get("version")

				if !ok {
					t.Fatalf("version not found")
				}
				if v.Scalar != tt.want {
					t.Errorf("Scalar = %q, want %q", v.Scalar, tt.want)
				}
				if v.Quote != mosaic.QuotePlain {
					t.Errorf("Quote = %v, want plain", v.Quote)
				}
			})
		}
	}
}

func TestFrontmatterGet_PlainScalarTrailingWhitespaceBeforeComment_StillTrimmed(t *testing.T) {
	doc := mustParse(t, "---\nversion: 1.0.0   # note\n---\n")

	v, _ := doc.Frontmatter().Get("version")

	if v.Scalar != "1.0.0" {
		t.Errorf("Scalar = %q, want 1.0.0", v.Scalar)
	}
}

func TestFrontmatterGet_QuotedScalarsKeepContent(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"double quoted inner whitespace", `version: "1.0.0  "`, "1.0.0  "},
		{"single quoted inner whitespace", `version: '1.0.0 '`, "1.0.0 "},
		{"double quoted, trailing whitespace outside", "version: \"1.0.0\"   ", "1.0.0"},
		{"single quoted, trailing whitespace outside", "version: '1.0.0'\t", "1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParse(t, "---\n"+tt.line+"\n---\n")

			v, _ := doc.Frontmatter().Get("version")

			if v.Scalar != tt.want {
				t.Errorf("Scalar = %q, want %q", v.Scalar, tt.want)
			}
		})
	}
}

func TestFrontmatterGet_BlockListPlainItemsTrailingWhitespace_NotPartOfValue(t *testing.T) {
	doc := mustParse(t, "---\ntools:\n  - read  \n  - write\t\n  - \"keep \"\n---\n")

	v, ok := doc.Frontmatter().Get("tools")

	if !ok || len(v.Items) != 3 {
		t.Fatalf("tools = %+v (found=%v), want 3 items", v, ok)
	}
	want := []string{"read", "write", "keep "}
	for i, w := range want {
		if v.Items[i].Scalar != w {
			t.Errorf("Items[%d].Scalar = %q, want %q", i, v.Items[i].Scalar, w)
		}
	}
}

func TestFrontmatterGet_FlowListPlainItemsTrailingWhitespace_NotPartOfValue(t *testing.T) {
	doc := mustParse(t, "---\ntools: [read , write\t]\n---\n")

	v, ok := doc.Frontmatter().Get("tools")

	if !ok || len(v.Items) != 2 {
		t.Fatalf("tools = %+v (found=%v), want 2 items", v, ok)
	}
	if v.Items[0].Scalar != "read" || v.Items[1].Scalar != "write" {
		t.Errorf("items = %q, %q; want read, write", v.Items[0].Scalar, v.Items[1].Scalar)
	}
}

func TestFrontmatterGet_MappingPlainValuesTrailingWhitespace_NotPartOfValue(t *testing.T) {
	doc := mustParse(t, "---\nmodels:\n  fast: haiku  \n  slow: opus\t\n---\n")

	v, ok := doc.Frontmatter().Get("models")

	if !ok || len(v.Pairs) != 2 {
		t.Fatalf("models = %+v (found=%v), want 2 pairs", v, ok)
	}
	if got := v.Pairs[0].Value.Scalar; got != "haiku" {
		t.Errorf("fast = %q, want haiku", got)
	}
	if got := v.Pairs[1].Value.Scalar; got != "opus" {
		t.Errorf("slow = %q, want opus", got)
	}
}

func TestBytes_PlainScalarTrailingWhitespace_RoundTripsByteIdentical(t *testing.T) {
	fixtures := []string{
		"---\nversion: 1.0.0   \n---\nBody\n",
		"---\r\nversion: 1.0.0\t\r\nname: a \r\n---\r\nBody\r\n",
		"---\ntools:\n  - read  \n  - write\t\nmodels:\n  fast: haiku  \n---\n",
		"---\nversion: 1.0.0  \n---\n",
	}
	for _, src := range fixtures {
		doc := mustParse(t, src)

		_, _ = doc.Frontmatter().Get("version")
		got := doc.Bytes()

		if !bytes.Equal(got, []byte(src)) {
			t.Errorf("Bytes() = %q, want %q", got, src)
		}
	}
}
