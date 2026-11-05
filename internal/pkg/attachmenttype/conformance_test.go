package attachmenttype

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestOfficeXMLCharacterEncodings(t *testing.T) {
	for _, name := range []string{"document-utf8-bom.docx", "document-utf16.docx"} {
		t.Run(name, func(t *testing.T) {
			body := fixture(t, name)
			before := bytes.Clone(body)
			if _, err := Validate(name, body); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, before) {
				t.Fatal("document bytes changed")
			}
		})
	}
	parts, err := readZIP(fixture(t, "document.docx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, little := range []bool{true, false} {
		t.Run(fmt.Sprint("UTF16LE=", little), func(t *testing.T) {
			document := strings.Replace(string(parts["word/document.xml"]), "UTF-8", "UTF-16", 1)
			document = strings.Replace(document, "A short campaign document.", "A proposal for Renée 東京 🌍.", 1)
			for _, content := range []struct {
				part  string
				xml   string
				valid bool
			}{
				{"word/document.xml", document, true},
				{"docProps/custom.xml", `<root>東京 🌍</root>`, true},
				{"docProps/custom.xml", `<!DOCTYPE root><root/>`, false},
				{"docProps/custom.xml", strings.Repeat("<a>", 64) + strings.Repeat("</a>", 64), true},
				{"docProps/custom.xml", strings.Repeat("<a>", 65) + strings.Repeat("</a>", 65), false},
				{"docProps/custom.xml", `<a>` + strings.Repeat("<b/>", 100_001) + `</a>`, false},
				{"docProps/custom.xml", `<a>` + strings.Repeat("界", (16<<20)/3) + `</a>`, false},
			} {
				encoded := []byte{0xfe, 0xff}
				var order binary.ByteOrder = binary.BigEndian
				if little {
					encoded = []byte{0xff, 0xfe}
					order = binary.LittleEndian
				}
				for _, unit := range utf16.Encode([]rune(content.xml)) {
					var pair [2]byte
					order.PutUint16(pair[:], unit)
					encoded = append(encoded, pair[:]...)
				}
				pkg := make(map[string][]byte, len(parts))
				for k, v := range parts {
					pkg[k] = v
				}
				pkg[content.part] = encoded
				_, err := Validate("document.docx", zipped(t, pkg))
				if (err == nil) != content.valid {
					t.Fatalf("valid=%v err=%v", content.valid, err)
				}
			}
		})
	}
	for _, invalid := range [][]byte{{0xff, 0xfe, 0x3c}, {0xff, 0xfe, 0x00, 0xd8, 0x3c, 0x00}, {0xfe, 0xff, 0xdc, 0x00}} {
		parts["docProps/custom.xml"] = invalid
		if _, err := Validate("document.docx", zipped(t, parts)); err == nil {
			t.Fatal("accepted incomplete UTF-16 XML")
		}
	}
}

func contentPDF(objects []string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestPDFPageTreeIndirectValues(t *testing.T) {
	for _, name := range []string{"direct-kids-with-content.pdf", "indirect-kids-with-content.pdf"} {
		if _, err := Validate(name, fixture(t, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	text := "BT /F1 12 Tf 50 250 Td (Business proposal) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type 10 0 R /Count 7 0 R /Kids 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox 8 0 R /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", "[3 0 R]", "1", "[0 0 9 0 R 300]", "300", "/Pages",
	}
	for _, tc := range []struct {
		name   string
		object int
		value  string
		valid  bool
	}{
		{"indirect scalars and containers", 6, "1", true},
		{"count cycle", 6, "7 0 R", false},
		{"container cycle", 7, "8 0 R", false},
		{"incorrect count", 6, "2", false},
		{"non numeric coordinate", 8, "/Other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyObjects := append([]string(nil), objects...)
			copyObjects[tc.object] = tc.value
			body := contentPDF(copyObjects)
			before := bytes.Clone(body)
			_, err := Validate("proposal.pdf", body)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if !bytes.Equal(body, before) {
				t.Fatal("PDF bytes changed")
			}
		})
	}
	for _, length := range []int{8, 40} {
		copyObjects := append([]string(nil), objects...)
		copyObjects[6] = "11 0 R"
		for i := 0; i < length; i++ {
			copyObjects = append(copyObjects, fmt.Sprintf("%d 0 R", 12+i))
		}
		copyObjects = append(copyObjects, "1")
		_, err := Validate("proposal.pdf", contentPDF(copyObjects))
		if (err == nil) != (length == 8) {
			t.Fatalf("resolution length=%d err=%v", length, err)
		}
	}
}

func TestZIPMemberNames(t *testing.T) {
	for _, name := range []string{"..", "dir/..", "C:/outside/file", "../file", "/file", "dir/../file"} {
		if _, err := Validate("files.zip", zipped(t, map[string][]byte{name: []byte("content")})); err == nil {
			t.Fatalf("accepted member %q", name)
		}
	}
}
