package attachmenttype

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
	"sort"
	"strings"
)

type zipMember struct {
	record     []byte
	dataOffset int64
}

// readZIP opens members only after the entire reader matches the validated directory and ranges.
func readZIP(body []byte) (map[string][]byte, error) {
	z, err := validatedZIP(body)
	if err != nil {
		return nil, err
	}
	// Both supported decompressors share this reader-local compressed-input budget.
	remaining := int64(MaxBytes)
	decompressors := map[uint16]zip.Decompressor{
		zip.Store: func(r io.Reader) io.ReadCloser {
			return io.NopCloser(&zipBudgetReader{r, &remaining})
		},
		zip.Deflate: func(r io.Reader) io.ReadCloser {
			return flate.NewReader(&zipBudgetReader{r, &remaining})
		},
	}
	for method, decompressor := range decompressors {
		z.RegisterDecompressor(method, decompressor)
	}
	parts := make(map[string][]byte, len(z.File))
	total := 0
	for _, f := range z.File {
		var r io.ReadCloser
		if strings.HasSuffix(f.Name, "/") {
			// Directory entries must validate their physical streams as empty content.
			if f.UncompressedSize64 != 0 {
				return nil, ErrInvalid
			}
			raw, openErr := f.OpenRaw()
			if openErr != nil {
				return nil, ErrInvalid
			}
			r = decompressors[f.Method](raw)
		} else {
			r, err = f.Open()
		}
		if err != nil {
			return nil, ErrInvalid
		}
		b, readErr := io.ReadAll(io.LimitReader(r, int64(min(maxPart, maxExpanded-total))+1))
		closeErr := r.Close()
		total += len(b)
		if readErr != nil || closeErr != nil || len(b) > maxPart || total > maxExpanded || uint64(len(b)) != f.UncompressedSize64 || crc32.ChecksumIEEE(b) != f.CRC32 {
			return nil, ErrInvalid
		}
		parts[f.Name] = b
	}
	return parts, nil
}

func validatedZIP(body []byte) (*zip.Reader, error) {
	return newValidatedZIP(body, zip.NewReader)
}

func newValidatedZIP(body []byte, newReader func(io.ReaderAt, int64) (*zip.Reader, error)) (*zip.Reader, error) {
	if len(body) > MaxBytes || (!bytes.HasPrefix(body, []byte("PK\x03\x04")) && !bytes.HasPrefix(body, []byte("PK\x05\x06"))) {
		return nil, ErrInvalid
	}
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
	members, ok := zipDirectory(body, int(binary.LittleEndian.Uint32(e[16:20])), end, count)
	if !ok {
		return nil, ErrInvalid
	}
	z, err := newReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || !sameZIPView(z, members, string(e[22:])) {
		return nil, ErrInvalid
	}
	return z, nil
}

func sameZIPView(z *zip.Reader, members []zipMember, comment string) bool {
	if z == nil || len(z.File) != len(members) || z.Comment != comment {
		return false
	}
	for i, m := range members {
		f, c := z.File[i], m.record
		n, x := int(binary.LittleEndian.Uint16(c[28:30])), int(binary.LittleEndian.Uint16(c[30:32]))
		offset, err := f.DataOffset()
		if err != nil || offset != m.dataOffset ||
			f.CreatorVersion != binary.LittleEndian.Uint16(c[4:6]) || f.ReaderVersion != binary.LittleEndian.Uint16(c[6:8]) ||
			f.Flags != binary.LittleEndian.Uint16(c[8:10]) || f.Method != binary.LittleEndian.Uint16(c[10:12]) ||
			f.ModifiedTime != binary.LittleEndian.Uint16(c[12:14]) || f.ModifiedDate != binary.LittleEndian.Uint16(c[14:16]) ||
			f.CRC32 != binary.LittleEndian.Uint32(c[16:20]) ||
			f.CompressedSize != binary.LittleEndian.Uint32(c[20:24]) || f.CompressedSize64 != uint64(binary.LittleEndian.Uint32(c[20:24])) ||
			f.UncompressedSize != binary.LittleEndian.Uint32(c[24:28]) || f.UncompressedSize64 != uint64(binary.LittleEndian.Uint32(c[24:28])) ||
			f.ExternalAttrs != binary.LittleEndian.Uint32(c[38:42]) ||
			f.Name != string(c[46:46+n]) || !bytes.Equal(f.Extra, c[46+n:46+n+x]) || f.Comment != string(c[46+n+x:]) {
			return false
		}
	}
	return true
}

type zipBudgetReader struct {
	r         io.Reader
	remaining *int64
}

func (r *zipBudgetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if *r.remaining <= 0 {
		return 0, ErrInvalid
	}
	p = p[:min(int64(len(p)), *r.remaining)]
	n, err := r.r.Read(p)
	*r.remaining -= int64(n)
	return n, err
}

func zipDirectory(body []byte, start, end, count int) ([]zipMember, bool) {
	if start < 0 || end < start || end > len(body) || count < 0 || count > maxEntries {
		return nil, false
	}
	type span struct{ start, end int }
	spans := make([]span, 0, count)
	members := make([]zipMember, 0, count)
	namesSeen := make(map[string]bool, count)
	at, compressed, expanded := start, uint64(0), uint64(0)
	for i := 0; i < count; i++ {
		if end-at < 46 || string(body[at:at+4]) != "PK\x01\x02" {
			return nil, false
		}
		c := body[at : at+46]
		names, extra, comment := int(binary.LittleEndian.Uint16(c[28:30])), int(binary.LittleEndian.Uint16(c[30:32])), int(binary.LittleEndian.Uint16(c[32:34]))
		if names+extra+comment > end-at-46 || binary.LittleEndian.Uint16(c[34:36]) != 0 {
			return nil, false
		}
		name := string(body[at+46 : at+46+names])
		flags, method := binary.LittleEndian.Uint16(c[8:10]), binary.LittleEndian.Uint16(c[10:12])
		if namesSeen[name] || name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || (len(name) >= 2 && name[1] == ':') || flags&1 != 0 || (method != zip.Store && method != zip.Deflate) {
			return nil, false
		}
		for _, segment := range strings.Split(name, "/") {
			if segment == ".." {
				return nil, false
			}
		}
		namesSeen[name] = true
		if !classicZIPExtra(body[at+46+names : at+46+names+extra]) {
			return nil, false
		}
		local := int(binary.LittleEndian.Uint32(c[42:46]))
		if local < 0 || local > start || start-local < 30 || string(body[local:local+4]) != "PK\x03\x04" {
			return nil, false
		}
		l := body[local : local+30]
		ln, le := int(binary.LittleEndian.Uint16(l[26:28])), int(binary.LittleEndian.Uint16(l[28:30]))
		if ln != names || ln+le > start-local-30 || !bytes.Equal(c[8:12], l[6:10]) || !bytes.Equal(body[at+46:at+46+names], body[local+30:local+30+ln]) || !classicZIPExtra(body[local+30+ln:local+30+ln+le]) {
			return nil, false
		}
		size := uint64(binary.LittleEndian.Uint32(c[20:24]))
		uncompressed := uint64(binary.LittleEndian.Uint32(c[24:28]))
		compressed += size
		expanded += uncompressed
		data := local + 30 + ln + le
		if compressed > MaxBytes || size > uint64(start-data) || uncompressed > maxPart || expanded > maxExpanded || binary.LittleEndian.Uint32(l[18:22]) == 0xffffffff || binary.LittleEndian.Uint32(l[22:26]) == 0xffffffff {
			return nil, false
		}
		finish := data + int(size)
		if flags&8 == 0 {
			if !bytes.Equal(c[16:28], l[14:26]) {
				return nil, false
			}
		} else {
			for field := 0; field < 3; field++ {
				v := binary.LittleEndian.Uint32(l[14+field*4:])
				if v != 0 && v != binary.LittleEndian.Uint32(c[16+field*4:]) {
					return nil, false
				}
			}
			if start-finish >= 4 && string(body[finish:finish+4]) == "PK\x07\x08" {
				finish += 4
			}
			if start-finish < 12 || !bytes.Equal(body[finish:finish+12], c[16:28]) {
				return nil, false
			}
			finish += 12
		}
		spans = append(spans, span{local, finish})
		members = append(members, zipMember{body[at : at+46+names+extra+comment], int64(data)})
		at += 46 + names + extra + comment
	}
	if at != end {
		return nil, false
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return nil, false
		}
	}
	return members, true
}

// classicZIPExtra accepts complete extra fields except ZIP64 extensions.
func classicZIPExtra(extra []byte) bool {
	for len(extra) != 0 {
		if len(extra) < 4 || binary.LittleEndian.Uint16(extra[:2]) == 1 {
			return false
		}
		n := int(binary.LittleEndian.Uint16(extra[2:4]))
		if n > len(extra)-4 {
			return false
		}
		extra = extra[4+n:]
	}
	return true
}
