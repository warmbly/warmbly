package attachmenttype

import (
	"bytes"
	"maps"
	"regexp"
	"strings"
	"testing"
)

func TestPresentationRequiresListedSlides(t *testing.T) {
	original, err := readZIP(presentation(t))
	if err != nil {
		t.Fatal(err)
	}
	const list = `<p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst>`
	for _, tc := range []struct {
		name, list, rootAttr string
		valid                bool
	}{
		{"listed slide", list, "", true},
		{"empty list", `<p:sldIdLst/>`, "", false},
		{"unrelated relationship", `<p:sldIdLst/><p:extLst r:id="rId1"/>`, "", false},
		{"root relationship", `<p:sldIdLst/>`, ` r:id="rId1"`, false},
		{"unlisted slide entry", `<p:sldIdLst/><p:sldId id="256" r:id="rId1"/>`, "", false},
		{"nested slide entry", `<p:sldIdLst><p:extLst><p:sldId id="256" r:id="rId1"/></p:extLst></p:sldIdLst>`, "", false},
		{"foreign list", `<sldIdLst xmlns="urn:other"><p:sldId id="256" r:id="rId1"/></sldIdLst><p:sldIdLst/>`, "", false},
		{"foreign entry", `<p:sldIdLst><sldId xmlns="urn:other" r:id="rId1"/></p:sldIdLst>`, "", false},
		{"different prefix", `<p:sldIdLst><s:sldId xmlns:s="http://schemas.openxmlformats.org/presentationml/2006/main" id="256" r:id="rId1"/></p:sldIdLst>`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := maps.Clone(original)
			parts["ppt/presentation.xml"] = []byte(strings.Replace(string(parts["ppt/presentation.xml"]), list, tc.list, 1))
			parts["ppt/presentation.xml"] = bytes.Replace(parts["ppt/presentation.xml"], []byte("<p:presentation "), []byte("<p:presentation"+tc.rootAttr+" "), 1)
			body := zipped(t, parts)
			before := bytes.Clone(body)
			got, err := Validate("slides.pptx", body)
			if (err == nil) != tc.valid || (tc.valid && got.MIME != "application/vnd.openxmlformats-officedocument.presentationml.presentation") {
				t.Fatalf("valid=%v type=%+v err=%v", tc.valid, got, err)
			}
			if !bytes.Equal(body, before) {
				t.Fatal("presentation bytes changed")
			}
		})
	}
}

func TestOfficeRelatedPartContentTypes(t *testing.T) {
	files := supported(t)
	for _, format := range []struct {
		name, member, contentType string
	}{
		{"sheet.xlsx", "xl/worksheets/sheet1.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"},
		{"slide.pptx", "ppt/slides/slide1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"},
	} {
		original, err := readZIP(files[format.name])
		if err != nil {
			t.Fatal(err)
		}
		declaration := `<Override PartName="/` + format.member + `" ContentType="` + format.contentType + `"/>`
		memberOverride := regexp.MustCompile(`<Override\b[^>]*PartName="/` + regexp.QuoteMeta(format.member) + `"[^>]*(/>|></Override>)`)
		contentTypes := memberOverride.ReplaceAllString(string(original["[Content_Types].xml"]), "")
		if contentTypes == string(original["[Content_Types].xml"]) {
			t.Fatal("fixture has no member override")
		}
		defaultXML := regexp.MustCompile(`<Default\b[^>]*Extension="xml"[^>]*(/>|></Default>)`)
		contentTypes = defaultXML.ReplaceAllString(contentTypes, "")
		for _, tc := range []struct {
			name, declarations string
			valid              bool
		}{
			{"override", declaration, true},
			{"default", `<Default Extension="xml" ContentType="` + format.contentType + `"/>`, true},
			{"case insensitive extension", `<Default Extension="XML" ContentType="` + format.contentType + `"/>`, true},
			{"override takes precedence", declaration + `<Default Extension="xml" ContentType="application/xml"/>`, true},
			{"wrong override takes precedence", `<Override PartName="/` + format.member + `" ContentType="application/xml"/><Default Extension="xml" ContentType="` + format.contentType + `"/>`, false},
			{"wrong default", `<Default Extension="xml" ContentType="application/xml"/>`, false},
			{"missing declaration", "", false},
			{"foreign declaration", strings.Replace(declaration, "<Override ", `<Override xmlns="urn:other" `, 1), false},
			{"duplicate override", declaration + declaration, false},
		} {
			t.Run(format.name+"/"+tc.name, func(t *testing.T) {
				parts := maps.Clone(original)
				parts["[Content_Types].xml"] = []byte(strings.Replace(contentTypes, "</Types>", tc.declarations+"</Types>", 1))
				body := zipped(t, parts)
				before := bytes.Clone(body)
				_, err := Validate(format.name, body)
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%v err=%v", tc.valid, err)
				}
				if !bytes.Equal(body, before) {
					t.Fatal("Office bytes changed")
				}
			})
		}
	}
}
