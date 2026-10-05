package docformat

// HasBOM reports whether the document currently carries a leading UTF-8 BOM, i.e. whether
// Bytes() will emit one. True after Parse of a BOM'd input; false after StripBOM.
func (d *Document) HasBOM() bool {
	return d.bom
}

// StripBOM removes the leading UTF-8 BOM from the document so that Bytes() does not emit it.
// It does not change frontmatter or body content. No-op when HasBOM() is false.
func (d *Document) StripBOM() {
	d.bom = false
}
