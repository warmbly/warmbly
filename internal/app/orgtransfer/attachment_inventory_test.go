package orgtransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

type inventoryArchive struct{ *bytes.Reader }

func (a inventoryArchive) Size() int64 { return a.Reader.Size() }

func inventoryFixture(t *testing.T, edit func(*Manifest, []models.CampaignAttachment, map[string][]byte)) []byte {
	t.Helper()
	org, campaign := uuid.New(), uuid.New()
	m := &Manifest{Kind: ArchiveKind, FormatVersion: 1, OrganizationID: org, SourceAppVersion: "v0.6.75",
		Tables: []ManifestTable{{Name: "campaigns", Rows: 1}, {Name: "campaign_attachments", Rows: 4}}}
	entries := map[string][]byte{}
	rows := make([]models.CampaignAttachment, 4)
	for i, name := range []string{"notes.txt", "legacy.unknown", "missing.txt", "mismatch.txt"} {
		body := []byte("archived original bytes\n")
		key := models.AttachmentObjectKey(campaign, name)
		rows[i] = models.CampaignAttachment{ID: uuid.New(), CampaignID: campaign, Filename: name, Size: int64(len(body)), MimeType: "application/x-existing", S3Key: key}
		ref := ManifestBlob{Key: key, Path: blobArchivePath(i, key), Size: int64(len(body)), Table: "campaign_attachments", Column: "s3_key"}
		m.Blobs = append(m.Blobs, ref)
		if i != 2 {
			entries[ref.Path] = body
		}
	}
	rows[3].Size++
	c, _ := json.Marshal(map[string]any{"id": campaign, "organization_id": org})
	entries[dataPath("campaigns")] = append(c, '\n')
	if edit != nil {
		edit(m, rows, entries)
	}
	var data bytes.Buffer
	for _, row := range rows {
		if err := json.NewEncoder(&data).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	entries[dataPath("campaign_attachments")] = data.Bytes()
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	entries[manifestPath] = manifest
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestAttachmentInventoryPagesPreserveArchiveAndReportContent(t *testing.T) {
	b := inventoryFixture(t, nil)
	before := bytes.Clone(b)
	archive := inventoryArchive{bytes.NewReader(b)}
	first, err := InventoryAttachments(t.Context(), archive, AttachmentInventoryOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	if first.ArchiveSHA256 != hex.EncodeToString(h[:]) || first.Done || first.NextAfter != 2 || first.TotalRows != 4 || len(first.Results) != 2 {
		t.Fatalf("first page: %+v", first)
	}
	if first.Results[0].Status != "matches_upload_profile" || first.Results[0].CanonicalMIME != "text/plain" || first.Results[0].StoredMIME != "application/x-existing" || first.Results[1].Status != "outside_upload_profile" {
		t.Fatalf("content observations: %+v", first.Results)
	}
	want := sha256.Sum256([]byte("archived original bytes\n"))
	for _, item := range first.Results {
		if item.ContentSHA256 != hex.EncodeToString(want[:]) {
			t.Fatalf("original content fingerprint: %+v", item)
		}
	}
	second, err := InventoryAttachments(t.Context(), archive, AttachmentInventoryOptions{After: first.NextAfter, Limit: 2, SHA256: first.ArchiveSHA256})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Done || second.NextAfter != 4 || len(second.Results) != 2 || second.Results[0].Status != "missing_blob" || second.Results[1].Status != "metadata_mismatch" {
		t.Fatalf("second page: %+v", second)
	}
	if !bytes.Equal(before, b) {
		t.Fatal("inventory changed archive bytes")
	}
}

func TestAttachmentInventoryRefusesUnboundResumeAndInvalidScope(t *testing.T) {
	b := inventoryFixture(t, nil)
	for _, opts := range []AttachmentInventoryOptions{
		{After: 1, Limit: 1}, {Limit: 0}, {Limit: 101}, {After: -1, Limit: 1},
		{Limit: 1, SHA256: "invalid"}, {Limit: 1, SHA256: string(bytes.Repeat([]byte("0"), 64))},
	} {
		if report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, opts); err == nil || report != nil {
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*Manifest, []models.CampaignAttachment, map[string][]byte)
	}{
		{"foreign campaign", func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) { m.OrganizationID = uuid.New() }},
		{"foreign attachment", func(_ *Manifest, a []models.CampaignAttachment, _ map[string][]byte) { a[0].CampaignID = uuid.New() }},
		{"duplicate row", func(_ *Manifest, a []models.CampaignAttachment, _ map[string][]byte) { a[1].ID = a[0].ID }},
		{"truncated", func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) { m.Tables[1].Truncated = true }},
		{"row count", func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) { m.Tables[1].Rows++ }},
		{"duplicate blob", func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) {
			m.Blobs = append(m.Blobs, m.Blobs[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := inventoryFixture(t, tc.edit)
			if report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1}); err == nil || report != nil {
				t.Fatal("ambiguous archive produced a page")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if report, err := InventoryAttachments(ctx, inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1}); err == nil || report != nil {
		t.Fatal("canceled inventory produced a page")
	}
}

func TestAttachmentInventoryMissingAndUnreferencedBlobsRemainUnknown(t *testing.T) {
	b := inventoryFixture(t, func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) {
		m.Blobs[0].Key = "attachments/" + uuid.NewString() + "/unreferenced.txt"
	})
	report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.UnreferencedBlobs != 1 || report.Results[0].Status != "missing_blob" {
		t.Fatalf("missing/unreferenced coverage: %+v", report)
	}
	b = inventoryFixture(t, func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) {
		m.Blobs[0].Column = "other_column"
	})
	report, err = InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Status != "metadata_mismatch" || report.Results[0].ContentSHA256 != "" {
		t.Fatalf("manifest relation: %+v", report.Results[0])
	}
}

func TestAttachmentInventoryUsesCurrentCSVProfileWithoutChangingLegacyBytes(t *testing.T) {
	body := []byte(strings.Repeat("field,", 4096) + "last\n")
	b := inventoryFixture(t, func(m *Manifest, a []models.CampaignAttachment, entries map[string][]byte) {
		a[0].Filename = "existing.csv"
		a[0].Size = int64(len(body))
		m.Blobs[0].Size = int64(len(body))
		entries[m.Blobs[0].Path] = body
	})
	before := bytes.Clone(b)
	report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Status != "outside_upload_profile" || report.Results[0].ContentSHA256 == "" || !bytes.Equal(before, b) {
		t.Fatalf("legacy CSV observation: %+v", report.Results[0])
	}
}

func TestAttachmentInventoryRejectsDuplicatePaths(t *testing.T) {
	b := inventoryFixture(t, nil)
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var duplicate bytes.Buffer
	w := zip.NewWriter(&duplicate)
	for _, f := range zr.File {
		if err := w.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Copy(zr.File[0]); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(duplicate.Bytes())}, AttachmentInventoryOptions{Limit: 1}); err == nil || err.Error() != "archive contains duplicate ZIP paths" || report != nil {
		t.Fatal("duplicate ZIP paths produced a report")
	}
}

type changingInventoryArchive struct {
	*bytes.Reader
	next *bytes.Reader
}

func (a *changingInventoryArchive) ReadAt(p []byte, off int64) (int, error) {
	n, err := a.Reader.ReadAt(p, off)
	if off+int64(n) == a.Size() && a.next != nil {
		a.Reader, a.next = a.next, nil
	}
	return n, err
}

func TestAttachmentInventoryRefusesArchiveChangedDuringRead(t *testing.T) {
	before := inventoryFixture(t, nil)
	// ZIP comments change archive identity without changing any member or size.
	first := append(bytes.Clone(before), 'a')
	second := append(bytes.Clone(before), 'b')
	for _, b := range [][]byte{first, second} {
		b[len(before)-2] = 1
		if _, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1}); err != nil {
			t.Fatalf("immutable fixture must be readable: %v", err)
		}
	}
	a := &changingInventoryArchive{Reader: bytes.NewReader(first), next: bytes.NewReader(second)}
	report, err := InventoryAttachments(t.Context(), a, AttachmentInventoryOptions{Limit: 1})
	if report != nil || err == nil || err.Error() != "archive changed during inventory; discard this page" {
		t.Fatalf("changed archive must not emit a page: report=%v error=%v", report, err)
	}
}

func rewriteInventoryMember(t *testing.T, source []byte, target string, transform func(*zip.FileHeader, []byte) []byte) ([]byte, int64) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range zr.File {
		if f.Name != target {
			if err := w.Copy(f); err != nil {
				t.Fatal(err)
			}
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		h := f.FileHeader
		h.Flags &^= 8
		h.Method = zip.Store
		h.Extra = nil
		h.CompressedSize64 = uint64(len(body))
		raw := transform(&h, body)
		dst, err := w.CreateRaw(&h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dst.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	zr, err = zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == target {
			offset, err := f.DataOffset()
			if err != nil {
				t.Fatal(err)
			}
			return b, offset
		}
	}
	t.Fatal("member not found")
	return nil, 0
}

func TestAttachmentInventoryVerifiesEveryReadMemberCRC(t *testing.T) {
	var blob string
	source := inventoryFixture(t, func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) { blob = m.Blobs[0].Path })
	for _, target := range []string{blob, manifestPath, dataPath("campaigns"), dataPath("campaign_attachments")} {
		for _, checksum := range []string{"valid", "zero", "nonzero"} {
			t.Run(target+"/"+checksum, func(t *testing.T) {
				b, _ := rewriteInventoryMember(t, source, target, func(h *zip.FileHeader, body []byte) []byte {
					actual := crc32.ChecksumIEEE(body)
					if actual == 0 || actual == 1 {
						t.Fatal("fixture requires a distinct nonzero checksum")
					}
					switch checksum {
					case "valid":
						h.CRC32 = actual
					case "zero":
						h.CRC32 = 0
					case "nonzero":
						h.CRC32 = actual ^ 1
					}
					return body
				})
				report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1})
				if checksum != "valid" && target != blob {
					if err == nil || report != nil {
						t.Fatal("metadata integrity must be established before emitting a page")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				item := report.Results[0]
				if checksum == "valid" {
					if item.Status != "matches_upload_profile" || item.ContentSHA256 == "" {
						t.Fatalf("valid member observation: %+v", item)
					}
				} else if item.Status != "unreadable_blob" || item.ContentSHA256 != "" || item.CanonicalMIME != "" {
					t.Fatalf("member integrity requires unreadable_blob without content observations, got %+v", item)
				}
			})
		}
	}
}

func TestAttachmentInventoryAcceptsEmptyMetadataWithZeroCRC(t *testing.T) {
	m := Manifest{Kind: ArchiveKind, FormatVersion: 1, OrganizationID: uuid.New(),
		Tables: []ManifestTable{{Name: "campaigns"}, {Name: "campaign_attachments"}}}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range map[string][]byte{manifestPath: body, dataPath("campaigns"): nil, dataPath("campaign_attachments"): nil} {
		h := &zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(content),
			CompressedSize64: uint64(len(content)), UncompressedSize64: uint64(len(content))}
		member, err := w.CreateRaw(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b.Bytes())}, AttachmentInventoryOptions{Limit: 1})
	if err != nil || report == nil || !report.Done || report.TotalRows != 0 {
		t.Fatalf("empty metadata with valid zero CRC: report=%v error=%v", report, err)
	}
}

type inventoryCancelArchive struct {
	*bytes.Reader
	target           int64
	requestSize      int
	cancel           context.CancelFunc
	started          bool
	readsAfterCancel int
}

func (a *inventoryCancelArchive) ReadAt(p []byte, off int64) (int, error) {
	if !a.started && off == a.target && (a.requestSize == 0 || len(p) == a.requestSize) {
		a.started = true
		a.cancel()
	} else if a.started {
		a.readsAfterCancel++
	}
	return a.Reader.ReadAt(p, off)
}

func TestAttachmentInventoryCancelsArchiveSourceReads(t *testing.T) {
	var blob string
	source := inventoryFixture(t, func(m *Manifest, _ []models.CampaignAttachment, _ map[string][]byte) { blob = m.Blobs[0].Path })
	b, offset := rewriteInventoryMember(t, source, blob, func(h *zip.FileHeader, body []byte) []byte {
		// Empty DEFLATE blocks exercise source reads without decompressed output.
		raw := bytes.Repeat([]byte{0, 0, 0, 255, 255}, 200000)
		n := uint16(len(body))
		raw = append(raw, 1, byte(n), byte(n>>8), byte(^n), byte(^n>>8))
		raw = append(raw, body...)
		h.Method = zip.Deflate
		h.CRC32 = crc32.ChecksumIEEE(body)
		h.CompressedSize64 = uint64(len(raw))
		return raw
	})
	if report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(b)}, AttachmentInventoryOptions{Limit: 1}); err != nil || report.Results[0].Status != "matches_upload_profile" {
		t.Fatalf("uncanceled fixture must be readable: report=%v error=%v", report, err)
	}
	for _, tc := range []struct {
		name   string
		offset int64
		size   int
	}{
		{"index", int64(len(b) - 1024), 1024},
		{"decompress", offset, 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			a := &inventoryCancelArchive{Reader: bytes.NewReader(b), target: tc.offset, requestSize: tc.size, cancel: cancel}
			report, err := InventoryAttachments(ctx, a, AttachmentInventoryOptions{Limit: 1})
			if !a.started || report != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: triggered=%v report=%v error=%v", a.started, report, err)
			}
			if a.readsAfterCancel != 0 {
				t.Fatalf("archive must stop source reads after cancellation, got %d further reads", a.readsAfterCancel)
			}
		})
	}
}

func TestAttachmentInventoryPreservesManifestInterruption(t *testing.T) {
	source := inventoryFixture(t, nil)
	b, offset := rewriteInventoryMember(t, source, manifestPath, func(_ *zip.FileHeader, body []byte) []byte { return body })
	for _, stage := range []string{"header", "body"} {
		for _, cause := range []string{"canceled", "deadline"} {
			t.Run(stage+"/"+cause, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				interrupt := cancel
				want := context.Canceled
				if cause == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
					interrupt = func() { <-ctx.Done() }
					want = context.DeadlineExceeded
				}
				defer cancel()
				target, request := offset, 0
				if stage == "header" {
					target -= int64(30 + len(manifestPath))
					request = 30
				}
				a := &inventoryCancelArchive{Reader: bytes.NewReader(b), target: target, requestSize: request, cancel: interrupt}
				report, err := InventoryAttachments(ctx, a, AttachmentInventoryOptions{Limit: 1})
				if !a.started || report != nil || !errors.Is(err, want) {
					t.Fatalf("manifest interruption: triggered=%v report=%v error=%v, want %v", a.started, report, err, want)
				}
				if a.readsAfterCancel != 0 {
					t.Fatalf("archive read after manifest interruption: %d", a.readsAfterCancel)
				}
			})
		}
	}
}
