package attachmenttype

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"
)

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
	header := zip.FileHeader{Name: "document.txt", Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte("document")), CompressedSize64: 8, UncompressedSize64: 8}
	body := zipWithHeader(t, header, []byte("document"))
	end := len(body) - 22
	start := int(binary.LittleEndian.Uint32(body[end+16:]))
	members, ok := zipDirectory(body, start, end, 1)
	if !ok {
		t.Fatal("valid directory refused")
	}
	for _, tc := range []struct {
		name   string
		change func(*zip.FileHeader)
	}{
		{"identical", func(h *zip.FileHeader) {}},
		{"name", func(h *zip.FileHeader) { h.Name = "proposal.txt" }},
		{"CRC", func(h *zip.FileHeader) { h.CRC32++ }},
		{"compressed size", func(h *zip.FileHeader) { h.CompressedSize64++ }},
		{"expanded size", func(h *zip.FileHeader) { h.UncompressedSize64++ }},
		{"method", func(h *zip.FileHeader) { h.Method = zip.Deflate }},
		{"flags", func(h *zip.FileHeader) { h.Flags = 0x800 }},
		{"extra and data offset", func(h *zip.FileHeader) { h.Extra = []byte{0xfe, 0xca, 0, 0} }},
		{"comment", func(h *zip.FileHeader) { h.Comment = "document comment" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			otherHeader := header
			tc.change(&otherHeader)
			other := zipWithHeader(t, otherHeader, []byte("document"))
			reader, err := zip.NewReader(bytes.NewReader(other), int64(len(other)))
			if err != nil {
				t.Fatal(err)
			}
			if sameZIPView(reader, members, "") != (tc.name == "identical") {
				t.Fatal("reader must match all validated members before opening")
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
