package worker

import (
	"bytes"
	"testing"

	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
)

func TestQueuedExistingAttachmentRemainsReadable(t *testing.T) {
	store, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("\x00\xff accepted existing bytes")
	key := "attachments/existing/file.unknown"
	if err := store.Put(t.Context(), key, bytes.NewReader(body), ""); err != nil {
		t.Fatal(err)
	}
	original := &emsg.EmailBlob{Attachments: []emsg.Attachment{{S3Key: key, Filename: "file.unknown", MimeType: "application/x-existing"}}}
	encoded, err := original.EncodeBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := emsg.DecodeBinary(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	w := &WorkerService{Storage: store}
	got, err := w.fetchAttachments(t.Context(), decoded.Attachments)
	if err != nil || len(got) != 1 || got[0].Filename != "file.unknown" || got[0].MimeType != "application/x-existing" || !bytes.Equal(got[0].Data, body) {
		t.Fatalf("queued attachment=%+v err=%v", got, err)
	}
}
