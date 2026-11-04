package attachmenttype

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxEntries  = 2048
	maxExpanded = 64 << 20
	maxPart     = 16 << 20
	packageNS   = "http://schemas.openxmlformats.org/package/2006/"
	officeNS    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

type xmlPart struct {
	root     xml.Name
	children []xml.StartElement
	links    map[string]bool
}

func parseXML(b []byte) (xmlPart, error) {
	decoded, encoding, err := xmlBytes(b)
	if err != nil {
		return xmlPart{}, err
	}
	d := xml.NewDecoder(bytes.NewReader(decoded))
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		label = strings.ToLower(label)
		if encoding != "" && (label == "utf-16" || label == encoding) {
			return input, nil
		}
		return nil, ErrInvalid
	}
	depth, tokens, roots := 0, 0, 0
	var list xml.Name
	out := xmlPart{links: map[string]bool{}}
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, ErrInvalid
		}
		tokens++
		if tokens > 200_000 {
			return out, ErrInvalid
		}
		switch v := t.(type) {
		case xml.StartElement:
			depth++
			if depth > 64 {
				return out, ErrInvalid
			}
			if depth == 1 {
				roots++
				out.root = v.Name
			}
			if depth == 2 {
				list = v.Name
				out.children = append(out.children, xml.CopyToken(v).(xml.StartElement))
			}
			listedSlide := depth == 3 && list == (xml.Name{Space: out.root.Space, Local: "sldIdLst"}) && v.Name == (xml.Name{Space: out.root.Space, Local: "sldId"})
			if out.root.Local != "presentation" || listedSlide {
				for _, a := range v.Attr {
					if a.Name.Space == strings.TrimSuffix(officeNS, "/") && a.Name.Local == "id" {
						out.links[a.Value] = true
					}
				}
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return out, ErrInvalid
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(v)) != 0 {
				return out, ErrInvalid
			}
		case xml.ProcInst:
			if v.Target != "xml" {
				return out, ErrInvalid
			}
		}
	}
	if roots != 1 || depth != 0 {
		return out, ErrInvalid
	}
	return out, nil
}

func xmlBytes(b []byte) ([]byte, string, error) {
	if len(b) > maxPart {
		return nil, "", ErrInvalid
	}
	if bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
		return b[3:], "", nil
	}
	var order binary.ByteOrder
	encoding := ""
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
		order, encoding, b = binary.LittleEndian, "utf-16le", b[2:]
	case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		order, encoding, b = binary.BigEndian, "utf-16be", b[2:]
	case bytes.HasPrefix(b, []byte{'<', 0, '?', 0}):
		order, encoding = binary.LittleEndian, "utf-16le"
	case bytes.HasPrefix(b, []byte{0, '<', 0, '?'}):
		order, encoding = binary.BigEndian, "utf-16be"
	default:
		return b, "", nil
	}
	if len(b)%2 != 0 {
		return nil, "", ErrInvalid
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i += 2 {
		r := rune(order.Uint16(b[i:]))
		if utf16.IsSurrogate(r) {
			if i+4 > len(b) {
				return nil, "", ErrInvalid
			}
			r = utf16.DecodeRune(r, rune(order.Uint16(b[i+2:])))
			if r == utf8.RuneError {
				return nil, "", ErrInvalid
			}
			i += 2
		}
		out = utf8.AppendRune(out, r)
		if len(out) > maxPart {
			return nil, "", ErrInvalid
		}
	}
	return out, encoding, nil
}

func attr(e xml.StartElement, key string) string {
	for _, a := range e.Attr {
		if a.Name.Space == "" && a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}

func officeType(body []byte, ext string) (string, bool) {
	parts, err := readZIP(body)
	if err != nil {
		return "", false
	}
	parsed := make(map[string]xmlPart)
	for name, b := range parts {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "vbaproject") {
			return "", false
		}
		if strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels") {
			p, err := parseXML(b)
			if err != nil {
				return "", false
			}
			parsed[name] = p
		}
	}
	var main, mime, root, ns, child, member, memberRoot, memberType, relation string
	switch ext {
	case ".docx":
		main, mime, root, ns, child = "word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document", "http://schemas.openxmlformats.org/wordprocessingml/2006/main", "body"
	case ".xlsx":
		main, mime, root, ns, child, member, memberRoot, relation = "xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "workbook", "http://schemas.openxmlformats.org/spreadsheetml/2006/main", "sheets", "xl/", "worksheet", "worksheet"
		memberType = "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"
	case ".pptx":
		main, mime, root, ns, child, member, memberRoot, relation = "ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "presentation", "http://schemas.openxmlformats.org/presentationml/2006/main", "sldIdLst", "ppt/", "sld", "slide"
		memberType = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"
	default:
		return "", false
	}
	ct := parsed["[Content_Types].xml"]
	if ct.root != (xml.Name{Space: packageNS + "content-types", Local: "Types"}) {
		return "", false
	}
	found := false
	for _, e := range ct.children {
		v := attr(e, "ContentType")
		if strings.Contains(strings.ToLower(v), "macroenabled") || strings.Contains(strings.ToLower(v), "vbaproject") {
			return "", false
		}
		if e.Name == (xml.Name{Space: ct.root.Space, Local: "Override"}) && attr(e, "PartName") == "/"+main && v == mime+".main+xml" {
			found = true
		}
	}
	if !found || !relationship(parsed["_rels/.rels"], "", officeNS+"officeDocument", main, nil) {
		return "", false
	}
	p := parsed[main]
	if p.root != (xml.Name{Space: ns, Local: root}) {
		return "", false
	}
	found = false
	for _, e := range p.children {
		if e.Name == (xml.Name{Space: ns, Local: child}) {
			found = true
		}
	}
	if !found {
		return "", false
	}
	if member != "" {
		found = false
		rels := parsed[path.Dir(main)+"/_rels/"+path.Base(main)+".rels"]
		for name, part := range parsed {
			if strings.HasPrefix(name, member) && part.root == (xml.Name{Space: ns, Local: memberRoot}) && relationship(rels, path.Dir(main), officeNS+relation, name, p.links) && partContentType(ct, name) == memberType {
				found = true
				break
			}
		}
		if !found {
			return "", false
		}
	}
	return mime, true
}

func partContentType(p xmlPart, name string) string {
	override, fallback := "", ""
	for _, e := range p.children {
		switch e.Name {
		case xml.Name{Space: p.root.Space, Local: "Override"}:
			if attr(e, "PartName") == "/"+name {
				if override != "" {
					return ""
				}
				override = attr(e, "ContentType")
				if override == "" {
					return ""
				}
			}
		case xml.Name{Space: p.root.Space, Local: "Default"}:
			if strings.EqualFold(attr(e, "Extension"), strings.TrimPrefix(path.Ext(name), ".")) {
				if fallback != "" {
					return ""
				}
				fallback = attr(e, "ContentType")
			}
		}
	}
	if override != "" {
		return override
	}
	return fallback
}

func relationship(p xmlPart, base, kind, target string, declared map[string]bool) bool {
	if p.root != (xml.Name{Space: packageNS + "relationships", Local: "Relationships"}) {
		return false
	}
	matches := 0
	for _, e := range p.children {
		if e.Name.Space != p.root.Space || e.Name.Local != "Relationship" || attr(e, "Type") != kind {
			continue
		}
		if declared != nil && !declared[attr(e, "Id")] {
			continue
		}
		raw := attr(e, "Target")
		if attr(e, "TargetMode") == "External" || raw == "" {
			continue
		}
		resolved := path.Clean(path.Join(base, raw))
		if strings.HasPrefix(raw, "/") {
			resolved = strings.TrimPrefix(path.Clean(raw), "/")
		}
		if resolved == target {
			matches++
		}
	}
	return matches == 1
}
