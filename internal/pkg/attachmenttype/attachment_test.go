package attachmenttype

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func zipped(t *testing.T, parts map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for n, v := range parts {
		w, e := z.Create(n)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(v); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func pdfFixture() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 1 /Kids [3 0 R] >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Contents 4 0 R >>", "<< /Length 0 >>\nstream\n\nendstream"}
	offsets := []int{0}
	for i, v := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, v)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 5\n0000000000 65535 f \n")
	for _, n := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", n)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}
func presentation(t *testing.T) []byte {
	return zipped(t, map[string][]byte{
		"[Content_Types].xml":             []byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/></Types>`),
		"_rels/.rels":                     []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`),
		"ppt/presentation.xml":            []byte(`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`),
		"ppt/_rels/presentation.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`),
		"ppt/slides/slide1.xml":           []byte(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`),
	})
}
func supported(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		"document.docx": fixture(t, "document.docx"), "slide.pptx": presentation(t), "file.pdf": pdfFixture(), "raster.webp": fixture(t, "raster.webp"),
		"data.csv": []byte("\ufeffname;city\r\nRenée;東京\r\n"), "data.tsv": []byte("name\tcity\nRenée\t東京\n"), "notes.txt": []byte("Hello Καλημέρα 東京\n"), "files.zip": zipped(t, map[string][]byte{"notes.txt": []byte("recipient content")}),
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellValue("Sheet1", "A1", "Actual workbook"); err != nil {
		t.Fatal(err)
	}
	b, e := f.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	files["sheet.xlsx"] = b.Bytes()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	for _, ext := range []string{"png", "jpg", "gif"} {
		var b bytes.Buffer
		var err error
		switch ext {
		case "png":
			err = png.Encode(&b, img)
		case "jpg":
			err = jpeg.Encode(&b, img, nil)
		case "gif":
			err = gif.Encode(&b, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		files["raster."+ext] = b.Bytes()
	}
	return files
}

func TestSupportedAttachmentsPreserveBytesAndReturnCanonicalTypes(t *testing.T) {
	want := map[string]string{".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pdf": "application/pdf", ".csv": "text/csv", ".tsv": "text/tab-separated-values", ".txt": "text/plain", ".zip": "application/zip", ".png": "image/png", ".jpg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
	for name, body := range supported(t) {
		t.Run(name, func(t *testing.T) {
			before := bytes.Clone(body)
			got, err := Validate(name, body)
			if err != nil || got.MIME != want[got.Extension] {
				t.Fatalf("type=%+v err=%v", got, err)
			}
			if !bytes.Equal(before, body) {
				t.Fatal("input changed")
			}
			if _, err := Validate(strings.ToUpper(name), body); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestUnsupportedMalformedAndRenamedAttachments(t *testing.T) {
	files := supported(t)
	cases := map[string]struct {
		name string
		body []byte
	}{
		"no extension": {"file", files["notes.txt"]}, "unknown extension": {"file.bin", files["notes.txt"]}, "legacy office": {"file.doc", []byte{0xd0, 0xcf, 0x11, 0xe0}}, "macro workbook": {"file.xlsm", files["sheet.xlsx"]},
		"renamed image": {"file.pdf", files["raster.png"]}, "wrong office family": {"file.docx", files["sheet.xlsx"]}, "zip renamed office": {"file.xlsx", files["files.zip"]},
		"binary text": {"file.txt", []byte("MZ\x00\x01binary")}, "invalid UTF8": {"file.csv", []byte{'n', ',', 0xff}}, "BOM HTML text": {"file.txt", []byte("\ufeff<html><body>content</body></html>")},
		"html text": {"file.txt", []byte("<html><body>content</body></html>")}, "malformed csv": {"file.csv", []byte("a,b\n\"unclosed,b")},
		"forged pdf": {"file.pdf", []byte("%PDF-1.7\nnot a document\n%%EOF")}, "truncated PDF": {"file.pdf", files["file.pdf"][:40]}, "bad xref": {"file.pdf", bytes.Replace(files["file.pdf"], []byte("0000000009"), []byte("0000000010"), 1)},
		"truncated png": {"file.png", files["raster.png"][:35]}, "truncated jpeg": {"file.jpg", files["raster.jpg"][:100]}, "truncated webp": {"file.webp", files["raster.webp"][:30]}, "gif trailer": {"file.gif", files["raster.gif"][:len(files["raster.gif"])-1]},
		"zip prefix": {"file.zip", append([]byte("MZprefix"), files["files.zip"]...)}, "truncated zip": {"file.zip", files["files.zip"][:30]}, "empty": {"file.txt", nil}, "oversize": {"file.txt", bytes.Repeat([]byte("a"), MaxBytes+1)},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			if got, err := Validate(tc.name, tc.body); err == nil || got != (Type{}) {
				t.Fatalf("accepted: %+v %v", got, err)
			}
		})
	}
}
func TestContainerBudgetsAndRequiredOfficeParts(t *testing.T) {
	for label, parts := range map[string]map[string][]byte{
		"expansion": {"large": bytes.Repeat([]byte("a"), maxPart+1)}, "traversal": {"../file": []byte("x")},
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := Validate("file.zip", zipped(t, parts)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	parts, err := readZIP(presentation(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "ppt/slides/slide1.xml", "ppt/_rels/presentation.xml.rels"} {
		t.Run("missing "+name, func(t *testing.T) {
			v := parts[name]
			delete(parts, name)
			defer func() { parts[name] = v }()
			if _, err := Validate("file.pptx", zipped(t, parts)); err == nil {
				t.Fatal("accepted missing part")
			}
		})
	}
	parts["ppt/slides/slide1.xml"] = []byte(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">`)
	if _, err := Validate("file.pptx", zipped(t, parts)); err == nil {
		t.Fatal("accepted malformed slide")
	}
}

func TestOfficeRequiresDeclaredRelationshipsAndCompleteXML(t *testing.T) {
	original, err := readZIP(presentation(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, replacement := range map[string][]byte{
		"ppt/presentation.xml":  []byte(`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:sldIdLst/></p:presentation>`),
		"_rels/.rels":           []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml" TargetMode="External"/></Relationships>`),
		"ppt/slides/slide1.xml": []byte(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/><second/>`),
	} {
		t.Run(name, func(t *testing.T) {
			parts := make(map[string][]byte, len(original))
			for k, v := range original {
				parts[k] = v
			}
			parts[name] = replacement
			if _, err := Validate("file.pptx", zipped(t, parts)); err == nil {
				t.Fatal("accepted inconsistent package")
			}
		})
	}
}

func TestPDFRejectsIncompleteObjectGraphAndStreamLengths(t *testing.T) {
	b := pdfFixture()
	// Equal-width changes keep the original xref offsets meaningful.
	for _, pair := range [][2]string{{"/Size 5", "/Size 0"}, {"/Pages 2 0 R", "/Pages 9 0 R"}, {"/Parent 2 0 R", "/Parent 1 0 R"}, {"/Length 0", "/Length 9"}, {"/Count 1", "/Count 2"}, {"/MediaBox", "/OtherBox"}} {
		t.Run(pair[0], func(t *testing.T) {
			changed := bytes.Replace(b, []byte(pair[0]), []byte(pair[1]), 1)
			if _, err := Validate("file.pdf", changed); err == nil {
				t.Fatal("accepted incomplete document")
			}
		})
	}
}

func TestZIPChecksCRCEntryCountAndAggregateExpansion(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateHeader(&zip.FileHeader{Name: "file", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("unique-member-payload")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	corrupted := bytes.Replace(buf.Bytes(), []byte("unique-member-payload"), []byte("broken-member-payload"), 1)
	if _, err := Validate("file.zip", corrupted); err == nil {
		t.Fatal("accepted bad CRC")
	}
	parts := map[string][]byte{}
	for i := 0; i <= maxEntries; i++ {
		parts[fmt.Sprint(i)] = []byte("x")
	}
	if _, err := Validate("file.zip", zipped(t, parts)); err == nil {
		t.Fatal("accepted too many entries")
	}
	large := bytes.Repeat([]byte("a"), maxPart)
	parts = map[string][]byte{}
	for i := 0; i < 5; i++ {
		parts[fmt.Sprint(i)] = large
	}
	if _, err := Validate("file.zip", zipped(t, parts)); err == nil {
		t.Fatal("accepted aggregate expansion")
	}
}

func TestRasterAndXMLResourceLimits(t *testing.T) {
	var large bytes.Buffer
	if err := png.Encode(&large, image.NewGray(image.Rect(0, 0, 8193, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate("large.png", large.Bytes()); err == nil {
		t.Fatal("accepted oversized raster dimensions")
	}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	animated := &gif.GIF{}
	for i := 0; i < 257; i++ {
		animated.Image = append(animated.Image, frame)
		animated.Delay = append(animated.Delay, 1)
	}
	var frames bytes.Buffer
	if err := gif.EncodeAll(&frames, animated); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate("animation.gif", frames.Bytes()); err == nil {
		t.Fatal("accepted excessive animation frames")
	}
	parts, err := readZIP(presentation(t))
	if err != nil {
		t.Fatal(err)
	}
	parts["ppt/slides/slide1.xml"] = []byte(strings.Repeat("<a>", 65) + strings.Repeat("</a>", 65))
	if _, err := Validate("file.pptx", zipped(t, parts)); err == nil {
		t.Fatal("accepted excessive XML depth")
	}
}
