package campaign

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type existingAttachmentRepo struct {
	repository.AttachmentRepository
	files []models.CampaignAttachment
}

func (r existingAttachmentRepo) ListByCampaign(context.Context, uuid.UUID) ([]models.CampaignAttachment, error) {
	return r.files, nil
}

func TestDuplicateRetainsExistingAttachmentMetadataAndBytes(t *testing.T) {
	store, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	src, dst, org := uuid.New(), uuid.New(), uuid.New()
	body := []byte("\x00\xff portable existing attachment")
	att := models.CampaignAttachment{Filename: "legacy.unknown", MimeType: "application/x-existing", Size: int64(len(body)), S3Key: models.AttachmentObjectKey(src, "legacy.unknown")}
	if err := store.Put(t.Context(), att.S3Key, bytes.NewReader(body), ""); err != nil {
		t.Fatal(err)
	}
	s := &campaignService{storage: store, attachmentRepo: existingAttachmentRepo{files: []models.CampaignAttachment{att}}}
	copies, _, undo, xerr := s.copyAttachments(t.Context(), org, src, dst)
	defer undo()
	if xerr != nil || len(copies) != 1 {
		t.Fatalf("copy=%v err=%v", copies, xerr)
	}
	copy := copies[0]
	if copy.Filename != att.Filename || copy.MimeType != att.MimeType || copy.Size != att.Size || copy.S3Key == att.S3Key {
		t.Fatal("copy changed legacy metadata")
	}
	r, err := store.Get(t.Context(), copy.S3Key)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("copy changed bytes")
	}
}
