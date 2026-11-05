package orgtransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

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

func TestAttachmentInventoryReportsMemberIntegrityAndRejectsDuplicatePaths(t *testing.T) {
	b := inventoryFixture(t, nil)
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	r, err := indexEntries(zr)[manifestPath].Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatal(err)
	}
	r.Close()
	// Alter only the selected member's recorded CRC, leaving metadata readable.
	var corrupt bytes.Buffer
	w := zip.NewWriter(&corrupt)
	for _, f := range zr.File {
		if f.Name == m.Blobs[0].Path {
			f.CRC32++
		}
		if err := w.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(corrupt.Bytes())}, AttachmentInventoryOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].Status != "unreadable_blob" || report.Results[0].ContentSHA256 != "" {
		t.Fatalf("integrity result: %+v", report.Results[0])
	}
	var duplicate bytes.Buffer
	w = zip.NewWriter(&duplicate)
	for range 2 {
		if err := w.Copy(zr.File[0]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if report, err := InventoryAttachments(t.Context(), inventoryArchive{bytes.NewReader(duplicate.Bytes())}, AttachmentInventoryOptions{Limit: 1}); err == nil || report != nil {
		t.Fatal("duplicate ZIP paths produced a report")
	}
}
