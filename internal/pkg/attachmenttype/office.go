package attachmenttype

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"path"
	"sort"
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

// readZIP verifies member identity, compressed work, expansion and CRC within shared limits.
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
	if !zipLayout(body, int(binary.LittleEndian.Uint32(e[16:20])), end, count) {
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
		if f.Name == "" || strings.Contains(f.Name, "\\") || strings.HasPrefix(f.Name, "/") || (len(f.Name) >= 2 && f.Name[1] == ':') {
			return nil, ErrInvalid
		}
		for _, segment := range strings.Split(f.Name, "/") {
			if segment == ".." {
				return nil, ErrInvalid
			}
		}
		// Open confines decompression to this member's preflighted compressed section.
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

func zipLayout(body []byte, start, end, count int) bool {
	type span struct{ start, end int }
	spans := make([]span, 0, count)
	at, compressed := start, uint64(0)
	for i := 0; i < count; i++ {
		if at < 0 || end-at < 46 || string(body[at:at+4]) != "PK\x01\x02" {
			return false
		}
		c := body[at : at+46]
		names, extra, comment := int(binary.LittleEndian.Uint16(c[28:30])), int(binary.LittleEndian.Uint16(c[30:32])), int(binary.LittleEndian.Uint16(c[32:34]))
		if names+extra+comment > end-at-46 || binary.LittleEndian.Uint16(c[34:36]) != 0 {
			return false
		}
		local := int(binary.LittleEndian.Uint32(c[42:46]))
		if local < 0 || local > start || start-local < 30 || string(body[local:local+4]) != "PK\x03\x04" {
			return false
		}
		l := body[local : local+30]
		ln, le := int(binary.LittleEndian.Uint16(l[26:28])), int(binary.LittleEndian.Uint16(l[28:30]))
		if ln != names || ln+le > start-local-30 || !bytes.Equal(c[8:12], l[6:10]) || !bytes.Equal(body[at+46:at+46+names], body[local+30:local+30+ln]) {
			return false
		}
		size := uint64(binary.LittleEndian.Uint32(c[20:24]))
		compressed += size
		data := local + 30 + ln + le
		if compressed > MaxBytes || size > uint64(start-data) {
			return false
		}
		finish := data + int(size)
		if binary.LittleEndian.Uint16(c[8:10])&8 == 0 {
			if !bytes.Equal(c[16:28], l[14:26]) {
				return false
			}
		} else {
			for field := 0; field < 3; field++ {
				v := binary.LittleEndian.Uint32(l[14+field*4:])
				if v != 0 && v != binary.LittleEndian.Uint32(c[16+field*4:]) {
					return false
				}
			}
			if start-finish >= 4 && string(body[finish:finish+4]) == "PK\x07\x08" {
				finish += 4
			}
			if start-finish < 12 || !bytes.Equal(body[finish:finish+12], c[16:28]) {
				return false
			}
			finish += 12
		}
		spans = append(spans, span{local, finish})
		at += 46 + names + extra + comment
	}
	if at != end {
		return false
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return false
		}
	}
	return true
}

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
