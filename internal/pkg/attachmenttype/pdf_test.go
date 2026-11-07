package attachmenttype

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func pdfRevision(body []byte, size, id int, inUse bool) []byte {
	previous := bytes.LastIndex(body, []byte("startxref"))
	prev, err := strconv.Atoi(strings.Fields(string(body[previous+9:]))[0])
	if err != nil {
		panic(err)
	}
	var b bytes.Buffer
	b.Write(body)
	offset, state := 0, "f"
	if inUse {
		offset, state = b.Len(), "n"
		fmt.Fprintf(&b, "%d 0 obj\n42\nendobj\n", id)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n%d 1\n%010d 00000 %s \ntrailer\n<< /Size %d /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", id, offset, state, size, prev, xref)
	return b.Bytes()
}

func TestPDFCrossReferenceRevisionSizes(t *testing.T) {
	original := pdfFixture()
	for _, tc := range []struct {
		name  string
		body  []byte
		valid bool
	}{
		{"base", original, true},
		{"largest object below size", bytes.Replace(original, []byte("/Size 5"), []byte("/Size 6"), 1), true},
		{"largest object equals size", bytes.Replace(original, []byte("/Size 5"), []byte("/Size 4"), 1), false},
		{"root outside size", bytes.Replace(original, []byte("/Size 5"), []byte("/Size 1"), 1), false},
		{"growing revision", pdfRevision(original, 6, 5, true), true},
		{"two growing revisions", pdfRevision(pdfRevision(original, 6, 5, true), 7, 6, true), true},
		{"new object at size", pdfRevision(original, 5, 5, true), false},
		{"previous revision size", pdfRevision(bytes.Replace(original, []byte("/Size 5"), []byte("/Size 4"), 1), 6, 5, true), false},
		{"replacement preserves size", pdfRevision(pdfRevision(original, 6, 5, true), 6, 5, true), true},
		{"freeing preserves size", pdfRevision(pdfRevision(original, 6, 5, true), 6, 5, false), true},
		{"revision retains allocated numbers", pdfRevision(pdfRevision(original, 6, 5, true), 5, 5, false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := bytes.Clone(tc.body)
			got, err := Validate("document.pdf", tc.body)
			if (err == nil) != tc.valid || (tc.valid && got.MIME != "application/pdf") {
				t.Fatalf("valid=%v type=%+v err=%v", tc.valid, got, err)
			}
			if !bytes.Equal(before, tc.body) {
				t.Fatal("PDF bytes changed")
			}
		})
	}
}
