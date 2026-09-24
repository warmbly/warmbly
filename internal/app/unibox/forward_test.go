package unibox

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// Only the read is implemented: a mark-seen reaching the embedded interface
// panics, which is the assertion that forwarding does not read the message.
type fakeForwardRepo struct {
	repository.UniboxRepository
	msg   *models.EmailMessageStoreData
	owner uuid.UUID
}

func (f *fakeForwardRepo) GetByIDForOrg(_ context.Context, _, id uuid.UUID) (*models.EmailMessageStoreData, uuid.UUID, error) {
	if f.msg == nil || f.msg.ID != id {
		return nil, uuid.Nil, repository.ErrEmailNotFound
	}
	return f.msg, f.owner, nil
}

func forwardService(t *testing.T, blob *emsg.EmailBlob) (*uniboxService, *models.EmailMessageStoreData) {
	t.Helper()
	store, err := storage.NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	msg := &models.EmailMessageStoreData{
		ID: uuid.New(), EmailID: uuid.New(), MessageID: "<real@example.com>",
		FromAddr: []string{"John <john@example.com>"}, ToAddr: []string{"sales@example.com"},
		Subject: "Pricing", Snippet: "Hi, here are...", Seen: false,
		SentDate: time.Date(2026, 9, 23, 8, 34, 0, 0, time.UTC),
	}
	owner := uuid.New()
	if blob != nil {
		raw, err := blob.EncodeBinary()
		if err != nil {
			t.Fatal(err)
		}
		key := config.StorageEndpointEmailBody(owner, msg.EmailID, msg.ID)
		if err := store.Put(context.Background(), key, bytes.NewReader(raw), ""); err != nil {
			t.Fatal(err)
		}
	}
	return &uniboxService{uniboxRepository: &fakeForwardRepo{msg: msg, owner: owner}, blob: store}, msg
}

func TestForwardSourceReadsStoredBody(t *testing.T) {
	s, msg := forwardService(t, &emsg.EmailBlob{PlainText: []byte("Hi, full text"), HTMLBody: []byte("<p>Hi, <b>full</b></p>")})

	got, xerr := s.ForwardSource(context.Background(), uuid.New(), msg.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if got.EmailID != msg.EmailID || got.Subject != "Pricing" || !got.Date.Equal(msg.SentDate) {
		t.Fatalf("envelope: %+v", got)
	}
	if got.BodyPlain != "Hi, full text" || got.BodyHTML != "<p>Hi, <b>full</b></p>" {
		t.Fatalf("body: plain %q html %q", got.BodyPlain, got.BodyHTML)
	}
}

func TestForwardSourceFallsBackToThePreview(t *testing.T) {
	s, msg := forwardService(t, nil)

	got, xerr := s.ForwardSource(context.Background(), uuid.New(), msg.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if got.BodyPlain != "Hi, here are..." || got.BodyHTML != "" {
		t.Fatalf("a missing body should forward the preview: %+v", got)
	}
}

// Legacy rows stored the text under both bodies; that is not HTML.
func TestForwardSourceDropsLegacyPlainAsHTML(t *testing.T) {
	s, msg := forwardService(t, &emsg.EmailBlob{HTMLBody: []byte("Line one\nLine & two")})

	got, _ := s.ForwardSource(context.Background(), uuid.New(), msg.ID)
	if got.BodyHTML != "" || got.BodyPlain != "Line one\nLine & two" {
		t.Fatalf("legacy body: plain %q html %q", got.BodyPlain, got.BodyHTML)
	}
}

func TestForwardSourceUnknownMessageIsNotFound(t *testing.T) {
	s, _ := forwardService(t, nil)

	_, xerr := s.ForwardSource(context.Background(), uuid.New(), uuid.New())
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("want NotFound, got %v", xerr)
	}
}
