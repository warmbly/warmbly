package attachmenttype

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

const (
	maxEntries  = 2048
	maxExpanded = 64 << 20
	maxPart     = 16 << 20
	packageNS   = "http://schemas.openxmlformats.org/package/2006/"
	officeNS    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

// readZIP verifies every member's compression and CRC within shared expansion limits.
func readZIP(body []byte) (map[string][]byte, error) {
	if !bytes.HasPrefix(body, []byte("PK\x03\x04")) && !bytes.HasPrefix(body, []byte("PK\x05\x06")) {
		return nil, ErrInvalid
	}
	// A bounded, single-volume directory excludes ZIP64 and executable prefixes.
	end := -1
	for i := len(body) - 22; i >= max(0, len(body)-65557); i-- {
		if string(body[i:i+4]) == "PK\x05\x06" && i+22+int(binary.LittleEndian.Uint16(body[i+20:i+22])) == len(body) {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, ErrInvalid
	}
	e := body[end:]
	count := int(binary.LittleEndian.Uint16(e[10:12]))
	if count > maxEntries || binary.LittleEndian.Uint16(e[4:6]) != 0 || binary.LittleEndian.Uint16(e[6:8]) != 0 || int(binary.LittleEndian.Uint16(e[8:10])) != count || uint64(binary.LittleEndian.Uint32(e[12:16]))+uint64(binary.LittleEndian.Uint32(e[16:20])) != uint64(end) {
		return nil, ErrInvalid
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(z.File) != count {
		return nil, ErrInvalid
	}
	parts := make(map[string][]byte, len(z.File))
	total := 0
	for _, f := range z.File {
		if _, duplicate := parts[f.Name]; duplicate || f.Flags&1 != 0 || f.UncompressedSize64 > maxPart {
			return nil, ErrInvalid
		}
		if f.Name == "" || strings.Contains(f.Name, "\\") || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "../") {
			return nil, ErrInvalid
		}
		r, err := f.Open()
		if err != nil {
			return nil, ErrInvalid
		}
		b, readErr := io.ReadAll(io.LimitReader(r, int64(min(maxPart, maxExpanded-total))+1))
		closeErr := r.Close()
		total += len(b)
		if readErr != nil || closeErr != nil || len(b) > maxPart || total > maxExpanded || uint64(len(b)) != f.UncompressedSize64 {
			return nil, ErrInvalid
		}
		parts[f.Name] = b
	}
	return parts, nil
}

type xmlPart struct {
	root     xml.Name
	children []xml.StartElement
	links    map[string]bool
}

func parseXML(b []byte) (xmlPart, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	depth, tokens, roots := 0, 0, 0
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
			for _, a := range v.Attr {
				if a.Name.Space == strings.TrimSuffix(officeNS, "/") && a.Name.Local == "id" {
					out.links[a.Value] = true
				}
			}
			depth++
			if depth > 64 {
				return out, ErrInvalid
			}
			if depth == 1 {
				roots++
				out.root = v.Name
			}
			if depth == 2 {
				out.children = append(out.children, xml.CopyToken(v).(xml.StartElement))
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
	var main, mime, root, ns, child, member, memberRoot, relation string
	switch ext {
	case ".docx":
		main, mime, root, ns, child = "word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document", "http://schemas.openxmlformats.org/wordprocessingml/2006/main", "body"
	case ".xlsx":
		main, mime, root, ns, child, member, memberRoot, relation = "xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "workbook", "http://schemas.openxmlformats.org/spreadsheetml/2006/main", "sheets", "xl/", "worksheet", "worksheet"
	case ".pptx":
		main, mime, root, ns, child, member, memberRoot, relation = "ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "presentation", "http://schemas.openxmlformats.org/presentationml/2006/main", "sldIdLst", "ppt/", "sld", "slide"
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
		if e.Name.Local == "Override" && attr(e, "PartName") == "/"+main && v == mime+".main+xml" {
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
			if strings.HasPrefix(name, member) && part.root == (xml.Name{Space: ns, Local: memberRoot}) && relationship(rels, path.Dir(main), officeNS+relation, name, p.links) {
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
