package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/models"
)

func TestOrgAttachmentsPreflightRunsOfflineThroughDispatch(t *testing.T) {
	t.Setenv("PRIMARY_DB", "invalid-unused-database")
	t.Setenv("REDIS", "invalid-unused-cache")
	t.Setenv("KMS_PROVIDER", "invalid-unused-provider")
	path := filepath.Join(t.TempDir(), "workspace.warmbly.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	org, campaign := uuid.New(), uuid.New()
	m := orgtransfer.Manifest{
		Kind: orgtransfer.ArchiveKind, FormatVersion: 1, OrganizationID: org,
		Tables: []orgtransfer.ManifestTable{{Name: "campaigns", Rows: 1}, {Name: "campaign_attachments", Rows: 2}},
	}
	var rows bytes.Buffer
	for i := range 2 {
		name := fmt.Sprintf("notes-%d.txt", i)
		body := []byte("archived original bytes\n")
		key := models.AttachmentObjectKey(campaign, name)
		blobPath := fmt.Sprintf("blobs/%d", i)
		m.Blobs = append(m.Blobs, orgtransfer.ManifestBlob{Key: key, Path: blobPath, Size: int64(len(body)), Table: "campaign_attachments", Column: "s3_key"})
		member, err := w.Create(blobPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(&rows).Encode(models.CampaignAttachment{
			ID: uuid.New(), CampaignID: campaign, Filename: name, S3Key: key, Size: int64(len(body)), MimeType: "text/plain",
		}); err != nil {
			t.Fatal(err)
		}
	}
	campaignRow, err := json.Marshal(map[string]any{"id": campaign, "organization_id": org})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"data/campaigns.ndjson": campaignRow, "data/campaign_attachments.ndjson": rows.Bytes()} {
		member, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := w.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(manifest).Encode(m); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "report")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	stdout := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = stdout }()
	if err := dispatch(t.Context(), []string{"org", "attachments-preflight", "--file", path, "--limit", "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var report orgtransfer.AttachmentInventory
	if err := json.NewDecoder(out).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report.Done || report.TotalRows != 2 || len(report.Results) != 1 || report.NextAfter != 1 || len(report.ArchiveSHA256) != 64 {
		t.Fatalf("offline command report: %+v", report)
	}
	if report.Results[0].Status != "matches_upload_profile" {
		t.Fatalf("valid attachment result: %+v", report.Results[0])
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("command changed the archive")
	}
}
