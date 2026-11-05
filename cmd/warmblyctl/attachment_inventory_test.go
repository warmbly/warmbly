package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/orgtransfer"
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
	for _, name := range []string{"data/campaigns.ndjson", "data/campaign_attachments.ndjson"} {
		if _, err := w.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := w.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(manifest).Encode(orgtransfer.Manifest{
		Kind: orgtransfer.ArchiveKind, FormatVersion: 1, OrganizationID: uuid.New(),
		Tables: []orgtransfer.ManifestTable{{Name: "campaigns"}, {Name: "campaign_attachments"}},
	}); err != nil {
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
	if !report.Done || report.TotalRows != 0 || report.Results == nil || len(report.ArchiveSHA256) != 64 {
		t.Fatalf("offline command report: %+v", report)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("command changed the archive")
	}
}
