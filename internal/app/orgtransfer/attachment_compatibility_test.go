package orgtransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
)

type attachmentScopeTx struct {
	pgx.Tx
	campaign uuid.UUID
}

func (tx attachmentScopeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &attachmentScopeRows{campaign: tx.campaign}, nil
}

type attachmentScopeRows struct {
	pgx.Rows
	campaign uuid.UUID
	read     bool
}

func (r *attachmentScopeRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}
func (r *attachmentScopeRows) Scan(dest ...any) error { *dest[0].(*uuid.UUID) = r.campaign; return nil }
func (r *attachmentScopeRows) Close()                 {}
func (r *attachmentScopeRows) Err() error             { return nil }

func TestPortableAttachmentsRetainExistingBytesAcrossArchiveRoundtrip(t *testing.T) {
	ctx := t.Context()
	campaign, org := uuid.New(), uuid.New()
	source, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	dest, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"legacy.unknown": []byte("\x00\xfflegacy bytes"), "document.pdf": []byte("previously accepted opaque bytes"), "notes.txt": []byte("Renée 東京\r\n")}
	collector := newBlobCollector()
	for filename, body := range files {
		key := models.AttachmentObjectKey(campaign, filename)
		if err := source.Put(ctx, key, bytes.NewReader(body), ""); err != nil {
			t.Fatal(err)
		}
		collector.addKey(key, "campaign_attachments", "s3_key")
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	m := &Manifest{SourceAppVersion: "v0.6.75"}
	if err := (&service{blobs: source}).writeBlobs(ctx, writer, collector, m); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if len(m.Blobs) != len(files) || len(m.attachmentCompatibilityWarnings()) != 1 {
		t.Fatal("manifest lost files or omitted compatibility notice")
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	warnings := (&service{blobs: dest}).restoreBlobs(ctx, attachmentScopeTx{campaign: campaign}, org, indexEntries(reader), m)
	if len(warnings) != 0 {
		t.Fatalf("restore: %v", warnings)
	}
	for _, ref := range m.Blobs {
		a, err := source.Get(ctx, ref.Key)
		if err != nil {
			t.Fatal(err)
		}
		b, err := dest.Get(ctx, ref.Key)
		if err != nil {
			a.Close()
			t.Fatal(err)
		}
		before, e1 := io.ReadAll(a)
		after, e2 := io.ReadAll(b)
		a.Close()
		b.Close()
		if e1 != nil || e2 != nil || !bytes.Equal(before, after) {
			t.Fatal("archive changed or lost attachment bytes")
		}
	}
	if notices := (&Manifest{}).attachmentCompatibilityWarnings(); len(notices) != 0 {
		t.Fatal("unrelated archive notice")
	}
}
