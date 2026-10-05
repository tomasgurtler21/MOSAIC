package transform_test

// project_region_bom_crlf_test.go guards that re-rendering a deployed agent keeps the content
// of a filled type="project" region whatever BOM and line endings the existing deployed file
// has, and that the deployed file's BOM never reaches the output.

import (
	"bytes"
	"testing"
)

const userRegionBody = "Team context line one.\n\nTeam context line two, with trailing text.\n"

// TestApply_ProjectRegion_BOMCRLFDeployed_ContentPreserved verifies that a filled project
// region survives re-rendering against a deployed file saved with BOM and CRLF line endings.
func TestApply_ProjectRegion_BOMCRLFDeployed_ContentPreserved(t *testing.T) {
	deployed := withBOM(filledDeployed(t, userRegionBody, "\r\n"))
	cases := []struct {
		name string
		src  []byte
		want []byte // expected region content in the output
	}{
		{"CRLF source keeps content byte for byte", crlfBytes(fullSourceLF), crlfBytes(userRegionBody)},
		{"LF source keeps content after line-ending normalisation", []byte(fullSourceLF), []byte(userRegionBody)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mustApply(t, fidelityRequest(t, tc.src, deployed))

			got := injectionContent(t, out, "ContextLimits")
			if !bytes.Equal(got, tc.want) {
				t.Errorf("ContextLimits region content\ngot:  %q\nwant: %q", got, tc.want)
			}
			if bytes.HasPrefix(out, []byte(utf8BOM)) {
				t.Error("output starts with the deployed file's BOM; the output must be BOM-less")
			}
		})
	}
}

// TestApply_ProjectRegion_BOMCRLFDeployed_LFSourceOutputIsLF verifies that the deployed
// file's CRLF endings do not leak into the output of an LF source.
func TestApply_ProjectRegion_BOMCRLFDeployed_LFSourceOutputIsLF(t *testing.T) {
	deployed := withBOM(filledDeployed(t, userRegionBody, "\r\n"))

	out := mustApply(t, fidelityRequest(t, []byte(fullSourceLF), deployed))

	assertNoCR(t, out)
}

// TestApply_ProjectRegion_BOMCRLFDeployed_CRLFSourceOutputIsCRLF verifies that the whole
// output of a CRLF source against a BOM + CRLF deployed file is single-style CRLF.
func TestApply_ProjectRegion_BOMCRLFDeployed_CRLFSourceOutputIsCRLF(t *testing.T) {
	deployed := withBOM(filledDeployed(t, userRegionBody, "\r\n"))

	out := mustApply(t, fidelityRequest(t, crlfBytes(fullSourceLF), deployed))

	assertNoBareLF(t, out)
}

// TestApply_ProjectRegion_BOMLFDeployed_ContentPreserved verifies preservation against a
// deployed file with a BOM and LF line endings.
func TestApply_ProjectRegion_BOMLFDeployed_ContentPreserved(t *testing.T) {
	deployed := withBOM(filledDeployed(t, userRegionBody, "\n"))

	out := mustApply(t, fidelityRequest(t, []byte(fullSourceLF), deployed))

	got := injectionContent(t, out, "ContextLimits")
	if !bytes.Equal(got, []byte(userRegionBody)) {
		t.Errorf("ContextLimits region content\ngot:  %q\nwant: %q", got, userRegionBody)
	}
	if bytes.HasPrefix(out, []byte(utf8BOM)) {
		t.Error("output starts with the deployed file's BOM; the output must be BOM-less")
	}
}
