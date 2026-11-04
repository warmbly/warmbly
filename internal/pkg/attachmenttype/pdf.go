package attachmenttype

import (
	"bytes"
	"strconv"
	"strings"
)

// This bounded structural reader supports unencrypted PDFs with classic xref tables.
type pdfName string
type pdfRef struct{ id, gen int }
type pdfDict map[string]any
type pdfEntry struct{ offset, gen int }
type pdfReader struct {
	data         []byte
	entries      map[int]pdfEntry
	objects      map[int]any
	loading      map[int]bool
	work, tokens int
	resolutions  int
}
type pdfCursor struct {
	p  *pdfReader
	at int
}

func validPDF(b []byte) bool {
	if len(b) < 20 || !bytes.HasPrefix(b, []byte("%PDF-")) || (b[8] != '\r' && b[8] != '\n') || !((b[5] == '1' && b[6] == '.' && b[7] >= '0' && b[7] <= '7') || string(b[5:8]) == "2.0") {
		return false
	}
	end := bytes.TrimRight(b, " \t\r\n\f\x00")
	if !bytes.HasSuffix(end, []byte("%%EOF")) {
		return false
	}
	tail := bytes.LastIndex(end, []byte("startxref"))
	if tail < 0 {
		return false
	}
	offset, err := strconv.Atoi(strings.TrimSpace(string(end[tail+9 : len(end)-5])))
	if err != nil {
		return false
	}
	p := &pdfReader{data: b, entries: map[int]pdfEntry{}, objects: map[int]any{}, loading: map[int]bool{}}
	seen := map[int]bool{}
	var root pdfRef
	newerSize := 100_000
	for revision := 0; ; revision++ {
		if revision >= 32 || offset < 0 || offset >= len(b) || seen[offset] {
			return false
		}
		seen[offset] = true
		c := pdfCursor{p: p, at: offset}
		if c.word() != "xref" {
			return false
		}
		maxInUse := -1
		for {
			w := c.word()
			if w == "trailer" {
				break
			}
			first, e1 := strconv.Atoi(w)
			count, e2 := strconv.Atoi(c.word())
			if e1 != nil || e2 != nil || first < 0 || count < 0 || count > 100_000 || first > 100_000-count {
				return false
			}
			for i := 0; i < count; i++ {
				off, e1 := strconv.Atoi(c.word())
				gen, e2 := strconv.Atoi(c.word())
				state := c.word()
				if e1 != nil || e2 != nil || off < 0 || gen < 0 || gen > 65535 || (state != "n" && state != "f") {
					return false
				}
				if state == "n" && first+i > maxInUse {
					maxInUse = first + i
				}
				if _, exists := p.entries[first+i]; !exists {
					if state == "n" {
						if off >= len(b) {
							return false
						}
						p.entries[first+i] = pdfEntry{off, gen}
					} else {
						p.entries[first+i] = pdfEntry{-1, gen}
					}
				}
			}
		}
		value, ok := c.value(0)
		trailer, dict := value.(pdfDict)
		size, sized := pdfInt(trailer["Size"])
		if !ok || !dict || !sized || size < 1 || size > newerSize || maxInUse >= size || trailer["Encrypt"] != nil || trailer["XRefStm"] != nil {
			return false
		}
		// Allocated object numbers remain in Size across incremental revisions.
		newerSize = size
		if revision == 0 {
			var yes bool
			root, yes = trailer["Root"].(pdfRef)
			if !yes {
				return false
			}
		}
		prev, exists := trailer["Prev"]
		if !exists {
			break
		}
		var yes bool
		offset, yes = pdfInt(prev)
		if !yes {
			return false
		}
	}
	for id, e := range p.entries {
		if e.offset >= 0 {
			v, ok := p.object(pdfRef{id, e.gen})
			if !ok || !p.references(v) {
				return false
			}
		}
	}
	v, ok := p.object(root)
	catalog, dict := v.(pdfDict)
	kind, resolved := p.resolve(catalog["Type"])
	if !ok || !dict || !resolved || kind != pdfName("Catalog") {
		return false
	}
	pages, ok := catalog["Pages"].(pdfRef)
	if !ok {
		return false
	}
	_, ok = p.pages(pages, pdfRef{}, false, map[int]bool{}, 0)
	return ok
}

func pdfInt(v any) (int, bool) { n, ok := v.(int); return n, ok }
func pdfSpace(b byte) bool     { return b == 0 || b == 9 || b == 10 || b == 12 || b == 13 || b == 32 }
func pdfDelim(b byte) bool     { return pdfSpace(b) || strings.ContainsRune("()<>[]{}/%", rune(b)) }
func (c *pdfCursor) skip() {
	for c.at < len(c.p.data) && c.p.work <= maxExpanded {
		c.p.work++
		if pdfSpace(c.p.data[c.at]) {
			c.at++
			continue
		}
		if c.p.data[c.at] == '%' {
			for c.at < len(c.p.data) && c.p.data[c.at] != '\r' && c.p.data[c.at] != '\n' {
				c.at++
				c.p.work++
			}
			continue
		}
		break
	}
}
func (c *pdfCursor) word() string {
	c.skip()
	start := c.at
	for c.at < len(c.p.data) && !pdfDelim(c.p.data[c.at]) {
		c.at++
		c.p.work++
	}
	if c.p.work > maxExpanded || c.at-start > 128 {
		return ""
	}
	return string(c.p.data[start:c.at])
}
func (c *pdfCursor) value(depth int) (any, bool) {
	c.p.tokens++
	if depth > 32 || c.p.tokens > 200_000 || c.p.work > maxExpanded {
		return nil, false
	}
	c.skip()
	b := c.p.data
	if c.at >= len(b) {
		return nil, false
	}
	switch b[c.at] {
	case '/':
		c.at++
		start := c.at
		for c.at < len(b) && !pdfDelim(b[c.at]) {
			c.at++
			c.p.work++
		}
		raw := string(b[start:c.at])
		var name strings.Builder
		for i := 0; i < len(raw); i++ {
			if raw[i] == '#' {
				if i+2 >= len(raw) {
					return nil, false
				}
				n, err := strconv.ParseUint(raw[i+1:i+3], 16, 8)
				if err != nil {
					return nil, false
				}
				name.WriteByte(byte(n))
				i += 2
			} else {
				name.WriteByte(raw[i])
			}
		}
		return pdfName(name.String()), true
	case '(':
		c.at++
		level := 1
		for c.at < len(b) {
			ch := b[c.at]
			c.at++
			c.p.work++
			if ch == '\\' {
				if c.at >= len(b) {
					return nil, false
				}
				c.at++
				continue
			}
			if ch == '(' {
				level++
			}
			if ch == ')' {
				level--
				if level == 0 {
					return nil, true
				}
			}
			if level > 32 {
				return nil, false
			}
		}
		return nil, false
	case '[':
		c.at++
		var a []any
		for {
			c.skip()
			if c.at >= len(b) {
				return nil, false
			}
			if b[c.at] == ']' {
				c.at++
				return a, true
			}
			v, ok := c.value(depth + 1)
			if !ok {
				return nil, false
			}
			a = append(a, v)
		}
	case '<':
		c.at++
		if c.at < len(b) && b[c.at] == '<' {
			c.at++
			d := pdfDict{}
			for {
				c.skip()
				if c.at+1 < len(b) && string(b[c.at:c.at+2]) == ">>" {
					c.at += 2
					return d, true
				}
				k, ok := c.value(depth + 1)
				name, yes := k.(pdfName)
				if !ok || !yes {
					return nil, false
				}
				if _, exists := d[string(name)]; exists {
					return nil, false
				}
				v, ok := c.value(depth + 1)
				if !ok {
					return nil, false
				}
				d[string(name)] = v
			}
		}
		for c.at < len(b) {
			ch := b[c.at]
			c.at++
			c.p.work++
			if ch == '>' {
				return nil, true
			}
			if !pdfSpace(ch) && !strings.ContainsRune("0123456789abcdefABCDEF", rune(ch)) {
				return nil, false
			}
		}
		return nil, false
	default:
		w := c.word()
		if w == "null" {
			return nil, true
		}
		if w == "true" || w == "false" {
			return w, true
		}
		n, err := strconv.Atoi(w)
		if err != nil {
			if w == "" || strings.Trim(w, "+-0123456789.") != "" {
				return nil, false
			}
			f, err := strconv.ParseFloat(w, 64)
			return f, err == nil
		}
		save := c.at
		gen, err := strconv.Atoi(c.word())
		if err == nil && c.word() == "R" {
			return pdfRef{n, gen}, true
		}
		c.at = save
		return n, true
	}
}
func (p *pdfReader) object(ref pdfRef) (any, bool) {
	entry, ok := p.entries[ref.id]
	if !ok || entry.offset < 0 || entry.gen != ref.gen || p.loading[ref.id] || len(p.loading) >= 32 {
		return nil, false
	}
	if v, ok := p.objects[ref.id]; ok {
		return v, true
	}
	p.loading[ref.id] = true
	defer delete(p.loading, ref.id)
	c := pdfCursor{p: p, at: entry.offset}
	if c.word() != strconv.Itoa(ref.id) || c.word() != strconv.Itoa(ref.gen) || c.word() != "obj" {
		return nil, false
	}
	v, ok := c.value(0)
	if !ok {
		return nil, false
	}
	end := c.word()
	if end == "stream" {
		d, ok := v.(pdfDict)
		if !ok {
			return nil, false
		}
		length, ok := p.resolve(d["Length"])
		if !ok {
			return nil, false
		}
		n, ok := pdfInt(length)
		if !ok || n < 0 || c.at >= len(p.data) {
			return nil, false
		}
		if p.data[c.at] == '\r' {
			c.at++
		}
		if c.at < len(p.data) && p.data[c.at] == '\n' {
			c.at++
		} else if c.at == 0 || p.data[c.at-1] != '\r' {
			return nil, false
		}
		if n > len(p.data)-c.at {
			return nil, false
		}
		c.at += n
		if c.word() != "endstream" {
			return nil, false
		}
		end = c.word()
	}
	if end != "endobj" {
		return nil, false
	}
	p.objects[ref.id] = v
	return v, true
}

func (p *pdfReader) resolve(v any) (any, bool) {
	seen := map[pdfRef]bool{}
	for {
		p.resolutions++
		if p.resolutions > 200_000 || p.work > maxExpanded {
			return nil, false
		}
		r, indirect := v.(pdfRef)
		if !indirect {
			return v, true
		}
		if len(seen) >= 32 || seen[r] {
			return nil, false
		}
		seen[r] = true
		var ok bool
		v, ok = p.object(r)
		if !ok {
			return nil, false
		}
	}
}

func (p *pdfReader) pages(ref, parent pdfRef, media bool, seen map[int]bool, depth int) (int, bool) {
	if depth > 32 || seen[ref.id] {
		return 0, false
	}
	seen[ref.id] = true
	v, ok := p.object(ref)
	d, yes := v.(pdfDict)
	if !ok || !yes {
		return 0, false
	}
	if depth > 0 && d["Parent"] != parent {
		return 0, false
	}
	if box, exists := d["MediaBox"]; exists {
		box, ok = p.resolve(box)
		if !ok {
			return 0, false
		}
		coords, array := box.([]any)
		if !array || len(coords) != 4 {
			return 0, false
		}
		var values [4]float64
		for i, v := range coords {
			v, ok = p.resolve(v)
			if !ok {
				return 0, false
			}
			switch n := v.(type) {
			case int:
				values[i] = float64(n)
			case float64:
				values[i] = n
			default:
				return 0, false
			}
		}
		if values[2] <= values[0] || values[3] <= values[1] {
			return 0, false
		}
		media = true
	}
	kind, ok := p.resolve(d["Type"])
	if !ok {
		return 0, false
	}
	if kind == pdfName("Page") {
		return 1, media
	}
	if kind != pdfName("Pages") {
		return 0, false
	}
	children, resolved := p.resolve(d["Kids"])
	kids, ok := children.([]any)
	if !ok || !resolved {
		return 0, false
	}
	total := 0
	for _, k := range kids {
		r, ok := k.(pdfRef)
		if !ok {
			return 0, false
		}
		n, ok := p.pages(r, ref, media, seen, depth+1)
		if !ok {
			return 0, false
		}
		total += n
	}
	value, resolved := p.resolve(d["Count"])
	count, ok := pdfInt(value)
	return total, resolved && ok && total > 0 && count == total
}

func (p *pdfReader) references(v any) bool {
	switch v := v.(type) {
	case pdfRef:
		e, ok := p.entries[v.id]
		return ok && e.offset >= 0 && e.gen == v.gen
	case pdfDict:
		for _, v := range v {
			if !p.references(v) {
				return false
			}
		}
	case []any:
		for _, v := range v {
			if !p.references(v) {
				return false
			}
		}
	}
	return true
}
