package attachmenttype

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"
)

func deflatedZIPData(t *testing.T, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestZIPDirectoryStreamContracts(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		for _, tc := range []struct {
			name     string
			payload  []byte
			expanded uint64
			crc      uint32
			valid    bool
		}{
			{"empty", nil, 0, 0, true},
			{"data with empty declaration", []byte("a"), 0, 0, false},
			{"nonempty directory", []byte("a"), 1, crc32.ChecksumIEEE([]byte("a")), false},
			{"incorrect empty CRC", nil, 0, 1, false},
			{"incorrect expanded size", nil, 1, 0, false},
		} {
			t.Run(zipMethodName(method)+"/"+tc.name, func(t *testing.T) {
				data := tc.payload
				if method == zip.Deflate {
					data = deflatedZIPData(t, data)
					if len(data) == 0 {
						t.Fatal("Deflate control must contain physical compressed bytes")
					}
				}
				h := zip.FileHeader{Name: "folder/", Method: method, CRC32: tc.crc, CompressedSize64: uint64(len(data)), UncompressedSize64: tc.expanded}
				body := zipDirectoryWithHeader(t, h, data)
				before := bytes.Clone(body)
				typ, err := Validate("document.zip", body)
				if (err == nil) != tc.valid || (tc.valid && typ.MIME != "application/zip") {
					t.Fatalf("valid=%v type=%+v err=%v", tc.valid, typ, err)
				}
				if !bytes.Equal(body, before) {
					t.Fatal("archive bytes changed")
				}
			})
		}
	}
	for _, data := range [][]byte{nil, {0xff}, deflatedZIPData(t, nil)[:1]} {
		h := zip.FileHeader{Name: "folder/", Method: zip.Deflate, CompressedSize64: uint64(len(data))}
		if _, err := Validate("document.zip", zipDirectoryWithHeader(t, h, data)); err == nil {
			t.Fatalf("invalid Deflate stream accepted: %x", data)
		}
	}
}

func zipMethodName(method uint16) string {
	if method == zip.Store {
		return "Store"
	}
	return "Deflate"
}

func zipDirectoryWithHeader(t *testing.T, header zip.FileHeader, data []byte) []byte {
	t.Helper()
	// Write raw data as an ordinary file, then set equal-length directory names in both headers.
	header.Name = "folderx"
	body := zipWithHeader(t, header, data)
	end := len(body) - 22
	central := int(binary.LittleEndian.Uint32(body[end+16:]))
	copy(body[30:37], "folder/")
	copy(body[central+46:central+53], "folder/")
	return body
}

func TestZIPDirectoryDescriptors(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(zipMethodName(method), func(t *testing.T) {
			var data []byte
			if method == zip.Deflate {
				data = deflatedZIPData(t, nil)
			}
			h := zip.FileHeader{Method: method, Flags: 8, CompressedSize64: uint64(len(data))}
			signed := zipDirectoryWithHeader(t, h, data)
			end := len(signed) - 22
			central := int(binary.LittleEndian.Uint32(signed[end+16:]))
			descriptor := central - 16
			if string(signed[descriptor:descriptor+4]) != "PK\x07\x08" {
				t.Fatal("signed descriptor required")
			}
			unsigned := append(bytes.Clone(signed[:descriptor]), signed[descriptor+4:]...)
			binary.LittleEndian.PutUint32(unsigned[end-4+16:], uint32(central-4))
			for _, body := range [][]byte{signed, unsigned} {
				if _, err := Validate("document.zip", body); err != nil {
					t.Fatal("valid empty directory descriptor refused", err)
				}
				bad := bytes.Clone(body)
				crcOffset := descriptor
				if len(body) == len(signed) {
					crcOffset += 4
				}
				bad[crcOffset] ^= 1
				if _, err := Validate("document.zip", bad); err == nil {
					t.Fatal("inconsistent directory descriptor accepted")
				}
			}
		})
	}
}

func zipLayout(body []byte, start, end, count int) bool {
	_, ok := zipDirectory(body, start, end, count)
	return ok
}

func zipWithHeader(t *testing.T, header zip.FileHeader, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.CreateRaw(&header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestZIPChecksEveryDeclaredCRC(t *testing.T) {
	zeroCRC := []byte{0x9d, 0x0a, 0xd9, 0x6d}
	if crc32.ChecksumIEEE(zeroCRC) != 0 {
		t.Fatal("nonempty zero-CRC fixture must have checksum zero")
	}
	for _, payload := range [][]byte{nil, []byte("recipient document"), zeroCRC} {
		crc := crc32.ChecksumIEEE(payload)
		for _, declared := range []uint32{crc, 0, crc ^ 1} {
			header := zip.FileHeader{Name: "document.txt", Method: zip.Store, CRC32: declared, CompressedSize64: uint64(len(payload)), UncompressedSize64: uint64(len(payload))}
			body := zipWithHeader(t, header, payload)
			before := bytes.Clone(body)
			_, err := Validate("document.zip", body)
			if (err == nil) != (declared == crc) {
				t.Fatalf("payload=%x declared=%08x computed=%08x err=%v", payload, declared, crc, err)
			}
			if !bytes.Equal(body, before) {
				t.Fatal("archive bytes changed")
			}
		}
	}
}

func TestZIPReaderRequiresEquivalentMemberView(t *testing.T) {
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, name := range []string{"document.txt", "notes.txt"} {
		h := zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte("a")), CompressedSize64: 1, UncompressedSize64: 1}
		f, err := w.CreateRaw(&h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("a")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body := out.Bytes()
	end := len(body) - 22
	start := int(binary.LittleEndian.Uint32(body[end+16:]))
	members, ok := zipDirectory(body, start, end, 2)
	if !ok {
		t.Fatal("valid directory refused")
	}
	for _, tc := range []struct {
		name   string
		change func(*zip.Reader)
	}{
		{"identical", func(z *zip.Reader) {}},
		{"fewer members", func(z *zip.Reader) { z.File = z.File[:1] }},
		{"more members", func(z *zip.Reader) { z.File = append(z.File, z.File[0]) }},
		{"order", func(z *zip.Reader) { z.File[0], z.File[1] = z.File[1], z.File[0] }},
		{"archive comment", func(z *zip.Reader) { z.Comment = "archive comment" }},
		{"name", func(z *zip.Reader) { z.File[0].Name = "proposal.txt" }},
		{"creator version", func(z *zip.Reader) { z.File[0].CreatorVersion++ }},
		{"reader version", func(z *zip.Reader) { z.File[0].ReaderVersion++ }},
		{"DOS time", func(z *zip.Reader) { z.File[0].ModifiedTime++ }},
		{"DOS date", func(z *zip.Reader) { z.File[0].ModifiedDate++ }},
		{"CRC", func(z *zip.Reader) { z.File[0].CRC32++ }},
		{"compressed size 32", func(z *zip.Reader) { z.File[0].CompressedSize++ }},
		{"compressed size 64", func(z *zip.Reader) { z.File[0].CompressedSize64++ }},
		{"expanded size 32", func(z *zip.Reader) { z.File[0].UncompressedSize++ }},
		{"expanded size 64", func(z *zip.Reader) { z.File[0].UncompressedSize64++ }},
		{"method", func(z *zip.Reader) { z.File[0].Method = zip.Deflate }},
		{"flags", func(z *zip.Reader) { z.File[0].Flags ^= 0x800 }},
		{"extra", func(z *zip.Reader) { z.File[0].Extra = []byte{0xfe, 0xca, 0, 0} }},
		{"member comment", func(z *zip.Reader) { z.File[0].Comment = "document comment" }},
		{"external attributes", func(z *zip.Reader) { z.File[0].ExternalAttrs++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			z, err := newValidatedZIP(body, func(r io.ReaderAt, size int64) (*zip.Reader, error) {
				called = true
				reader, err := zip.NewReader(r, size)
				if err != nil {
					t.Fatal(err)
				}
				tc.change(reader)
				return reader, nil
			})
			valid := tc.name == "identical"
			if !called || (err == nil) != valid || (z != nil) != valid {
				t.Fatalf("production boundary must require equivalent member views: reader=%v err=%v", z != nil, err)
			}
		})
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	shifted := append([]zipMember(nil), members...)
	shifted[0].dataOffset++
	if sameZIPView(reader, shifted, "") {
		t.Fatal("reader must match validated compressed ranges")
	}
	// Local-only extra fields shift physical ranges without changing central metadata.
	at := 30 + len("document.txt")
	other := append(bytes.Clone(body[:at]), []byte{0xfe, 0xca, 0, 0}...)
	other = append(other, body[at:]...)
	binary.LittleEndian.PutUint16(other[28:], 4)
	binary.LittleEndian.PutUint32(other[end+4+16:], uint32(start+4))
	second := start + 4 + len(members[0].record)
	binary.LittleEndian.PutUint32(other[second+42:], binary.LittleEndian.Uint32(other[second+42:])+4)
	if _, err := Validate("document.zip", other); err != nil {
		t.Fatal("consistent shifted-range control refused", err)
	}
	if z, err := newValidatedZIP(body, func(io.ReaderAt, int64) (*zip.Reader, error) {
		return zip.NewReader(bytes.NewReader(other), int64(len(other)))
	}); err == nil || z != nil {
		t.Fatal("production boundary must require identical physical ranges")
	}
}

func TestZIPCompressedReadersShareBudget(t *testing.T) {
	remaining := int64(5)
	first := zipBudgetReader{bytes.NewBufferString("abc"), &remaining}
	if b, err := io.ReadAll(&first); err != nil || string(b) != "abc" || remaining != 2 {
		t.Fatalf("first read=%q remaining=%d err=%v", b, remaining, err)
	}
	second := zipBudgetReader{bytes.NewBufferString("def"), &remaining}
	if b, err := io.ReadAll(&second); err == nil || string(b) != "de" || remaining != 0 {
		t.Fatalf("second read=%q remaining=%d err=%v", b, remaining, err)
	}
}

func TestZIPClassicProfile(t *testing.T) {
	payload := []byte("document")
	base := zip.FileHeader{Name: "document.txt", Method: zip.Store, CRC32: crc32.ChecksumIEEE(payload), CompressedSize64: uint64(len(payload)), UncompressedSize64: uint64(len(payload))}
	for _, tc := range []struct {
		name   string
		change func(*zip.FileHeader)
		valid  bool
	}{
		{"ordinary", func(h *zip.FileHeader) {}, true},
		{"ordinary extra", func(h *zip.FileHeader) { h.Extra = []byte{0xfe, 0xca, 0, 0} }, true},
		{"ZIP64 extra", func(h *zip.FileHeader) { h.Extra = []byte{1, 0, 0, 0} }, false},
		{"compressed sentinel", func(h *zip.FileHeader) { h.CompressedSize64 = 0xffffffff }, false},
		{"expanded sentinel", func(h *zip.FileHeader) { h.UncompressedSize64 = 0xffffffff }, false},
		{"incomplete extra", func(h *zip.FileHeader) { h.Extra = []byte{0xfe, 0xca, 1, 0} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := base
			tc.change(&header)
			_, err := Validate("document.zip", zipWithHeader(t, header, payload))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
